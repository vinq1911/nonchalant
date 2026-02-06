// If you are AI: This file implements the RTMP server that accepts connections.
// The server handles handshake, command processing, and media publishing.

package rtmp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"nonchalant/internal/core/bus"
	"nonchalant/internal/core/protocol/amf0"
	rtmpprotocol "nonchalant/internal/core/protocol/rtmp"
)

// Server represents an RTMP server.
type Server struct {
	registry *bus.Registry
	listener net.Listener
}

// NewServer creates a new RTMP server.
func NewServer(registry *bus.Registry) *Server {
	return &Server{
		registry: registry,
	}
}

// Listen starts listening on the specified address.
func (s *Server) Listen(addr string) error {
	var err error
	s.listener, err = net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return nil
}

// Accept accepts a new connection and handles it in a goroutine.
func (s *Server) Accept() error {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return err
		}
		go s.handleConnection(conn)
	}
}

// handleConnection handles a single RTMP connection.
func (s *Server) handleConnection(conn net.Conn) {
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("Error closing connection: %v", err)
		}
	}()

	// Create service session
	session := NewServiceSession(conn, s.registry)
	defer session.Close()

	// Perform handshake
	if err := session.PerformHandshake(); err != nil {
		// Log handshake errors but don't spam logs for invalid version
		if err.Error() == "invalid RTMP version" {
			// This might be a browser trying to connect via HTTP, or leftover data
			// Just close silently
			return
		}
		log.Printf("Handshake failed: %v", err)
		return
	}

	// NOTE: Window ACK, peer bandwidth, and chunk size are sent AFTER connect command
	// but BEFORE connect response (see HandleConnect)

	// Main message loop
	for {
		// Read chunk
		csID, err := session.ReadChunk()
		if err != nil {
			if err != io.EOF {
				log.Printf("Read chunk error: %v", err)
			}
			return
		}

		// Get complete message if available
		body, msgType, timestamp, streamID, complete := session.GetCompleteMessage(csID)
		if !complete {
			continue
		}

		// NOTE: Debug logging removed for production

		// Handle message based on type
		switch msgType {
		case rtmpprotocol.MessageTypeSetChunkSize:
			size, err := rtmpprotocol.ParseSetChunkSize(body)
			if err != nil {
				log.Printf("Failed to parse set chunk size: %v", err)
				continue
			}
			session.SetChunkSize(size)

		case rtmpprotocol.MessageTypeUserCtrl:
			// Handle user control messages (ping, stream begin, etc.)
			// Most don't require a response, just continue

		case rtmpprotocol.MessageTypeCommandAMF0:
			if err := s.handleCommand(session, body, streamID); err != nil {
				log.Printf("Command handling error: %v", err)
				return
			}

		case rtmpprotocol.MessageTypeAudio, rtmpprotocol.MessageTypeVideo, rtmpprotocol.MessageTypeDataAMF0:
			session.HandleMediaMessage(msgType, timestamp, body)

		default:
			// NOTE: Other message types are ignored
		}
	}
}

// handleCommand handles AMF0 command messages.
// streamID is the stream ID from the message header (used for publish/play commands).
func (s *Server) handleCommand(session *ServiceSession, body []byte, streamID uint32) error {
	// Decode command (only command name and transaction ID, skips rest)
	command, err := amf0.DecodeCommand(bytes.NewReader(body))
	if err != nil {
		// Log diagnostic info before returning error
		hexDump := ""
		if len(body) > 0 {
			hexDump = fmt.Sprintf(" (first byte: 0x%02x", body[0])
			if len(body) > 1 {
				hexDump += fmt.Sprintf(", second: 0x%02x", body[1])
			}
			if len(body) > 15 {
				hexDump += fmt.Sprintf(", bytes 0-15: %x", body[:16])
			}
			hexDump += ")"
		}
		log.Printf("Failed to decode command: %v (body length: %d)%s", err, len(body), hexDump)
		return err
	}

	if len(command) == 0 {
		return nil
	}

	cmdName, ok := command[0].(string)
	if !ok {
		return nil
	}

	switch cmdName {
	case "connect":
		if err := session.HandleConnect(command); err != nil {
			return err
		}
		log.Printf("Command handled: connect")
		return nil
	case "createStream":
		if err := session.HandleCreateStream(command); err != nil {
			return err
		}
		log.Printf("Command handled: createStream")
		return nil
	case "publish":
		if err := session.HandlePublish(command, streamID); err != nil {
			return err
		}
		log.Printf("Command handled: publish (streamID=%d)", streamID)
		return nil
	case "deleteStream", "closeStream":
		// Handle unpublish
		log.Printf("Command handled: %s (unpublish)", cmdName)
		session.Close()
		return nil
	default:
		// NOTE: Unknown commands are ignored
		log.Printf("Command ignored: %s", cmdName)
		return nil
	}
}

// Close closes the server.
func (s *Server) Close() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// CreateWindowAckSize creates a window acknowledgement size message.
func CreateWindowAckSize(size uint32) []byte {
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body, size)
	return body
}

// CreateSetPeerBandwidth creates a set peer bandwidth message.
func CreateSetPeerBandwidth(size uint32, limitType byte) []byte {
	body := make([]byte, 5)
	binary.BigEndian.PutUint32(body[0:4], size)
	body[4] = limitType
	return body
}
