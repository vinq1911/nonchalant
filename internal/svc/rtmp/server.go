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
	defer conn.Close()

	// Create service session
	session := NewServiceSession(conn, s.registry)
	defer session.Close()

	// Perform handshake
	if err := session.PerformHandshake(); err != nil {
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
		body, msgType, timestamp, complete := session.GetCompleteMessage(csID)
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
			if err := s.handleCommand(session, body); err != nil {
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
func (s *Server) handleCommand(session *ServiceSession, body []byte) error {
	// Log first bytes for debugging
	if len(body) > 0 {
		log.Printf("Decoding command: first byte=0x%02x, body length=%d", body[0], len(body))
		if len(body) > 16 {
			log.Printf("  First 16 bytes: %x", body[:16])
		}
	}

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

	log.Printf("Decoded command array: length=%d", len(command))
	if len(command) == 0 {
		log.Printf("Empty command array")
		return nil
	}

	log.Printf("Command[0] type: %T, value: %v", command[0], command[0])
	cmdName, ok := command[0].(string)
	if !ok {
		log.Printf("Command name is not string, got %T", command[0])
		return nil
	}
	log.Printf("Command: %s", cmdName)

	switch cmdName {
	case "connect":
		return session.HandleConnect(command)
	case "createStream":
		return session.HandleCreateStream(command)
	case "publish":
		return session.HandlePublish(command)
	case "deleteStream", "closeStream":
		// Handle unpublish
		session.Close()
		return nil
	default:
		// NOTE: Unknown commands are ignored
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
