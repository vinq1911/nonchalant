// If you are AI: This file manages RTMP service session handling.
// Handles command processing and publish lifecycle.

package rtmp

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"nonchalant/internal/core/bus"
	"nonchalant/internal/core/protocol/amf0"
	rtmpprotocol "nonchalant/internal/core/protocol/rtmp"
)

// ServiceSession wraps RTMP protocol session with service logic.
type ServiceSession struct {
	*rtmpprotocol.Session
	registry     *bus.Registry
	publisher    *Publisher
	nextStreamID uint32
}

// NewServiceSession creates a new service session.
func NewServiceSession(conn io.ReadWriter, registry *bus.Registry) *ServiceSession {
	return &ServiceSession{
		Session:      rtmpprotocol.NewSession(conn),
		registry:     registry,
		nextStreamID: 1,
	}
}

// HandleConnect handles the connect command.
// Format: ["connect", transaction_id, command_object, ...]
// NOTE: Some clients may send command_object as a separate element or it may be missing
func (s *ServiceSession) HandleConnect(command amf0.Array) error {
	if len(command) < 2 {
		return fmt.Errorf("invalid connect command: need at least 2 elements")
	}

	// command[0] = "connect" (string)
	// command[1] = transaction_id (number)
	// command[2] = command_object (object, null, or may be missing)

	app := "live" // Default app name
	objectEncoding := float64(0)

	// Try to extract app and objectEncoding from command object if present
	if len(command) >= 3 && command[2] != nil {
		var cmdObj amf0.Object
		switch v := command[2].(type) {
		case amf0.Object:
			cmdObj = v
		case map[string]interface{}:
			// Convert to Object type
			cmdObj = make(amf0.Object)
			for k, val := range v {
				cmdObj[k] = val
			}
		default:
			// Not an object, use default app
		}

		if cmdObj != nil {
			if appVal, ok := cmdObj["app"].(string); ok {
				app = appVal
			}
			if encVal, ok := cmdObj["objectEncoding"].(float64); ok {
				objectEncoding = encVal
			}
		}
	}

	s.SetApp(app)

	// Send window acknowledgement size and peer bandwidth
	// These MUST be sent AFTER connect command but BEFORE connect response
	ackSize := createWindowAckSizeBody(5000000)
	if err := s.WriteMessage(2, rtmpprotocol.MessageTypeWinAckSize, 0, 0, ackSize); err != nil {
		log.Printf("HandleConnect: failed to send window ack size: %v", err)
		return fmt.Errorf("failed to send window ack size: %w", err)
	}

	peerBW := createSetPeerBandwidthBody(5000000, 2)
	if err := s.WriteMessage(2, rtmpprotocol.MessageTypeSetPeerBandwidth, 0, 0, peerBW); err != nil {
		log.Printf("HandleConnect: failed to send set peer bandwidth: %v", err)
		return fmt.Errorf("failed to send set peer bandwidth: %w", err)
	}

	// NOTE: Chunk size is sent immediately after handshake (see server.go)
	// so we don't send it again here

	// Send _result response with objectEncoding
	if err := s.SendConnectResult(command[1], objectEncoding); err != nil {
		log.Printf("HandleConnect: failed to send connect result: %v", err)
		return err
	}
	log.Printf("HandleConnect: successfully sent connect response for app=%s", app)
	return nil
}

// SendConnectResult sends the connect _result response.
// transID is the transaction ID from the connect command.
// objectEncoding is the object encoding from the connect command (0 for AMF0, 3 for AMF3).
// Format: _result, transID, cmdObj, info
func (s *ServiceSession) SendConnectResult(transID interface{}, objectEncoding float64) error {
	// Convert transaction ID to float64 (AMF0 numbers are float64)
	var transIDFloat float64
	switch v := transID.(type) {
	case float64:
		transIDFloat = v
	case int:
		transIDFloat = float64(v)
	case int64:
		transIDFloat = float64(v)
	default:
		// Try to convert, default to 1 if conversion fails
		if num, ok := transID.(float64); ok {
			transIDFloat = num
		} else {
			transIDFloat = 1.0
		}
	}

	// Create response objects with all expected fields
	cmdObj := amf0.Object{
		"fmsVer":       "FMS/3,0,1,123",
		"capabilities": float64(31),
	}
	info := amf0.Object{
		"level":          "status",
		"code":           "NetConnection.Connect.Success",
		"description":    "Connection succeeded.",
		"objectEncoding": objectEncoding,
	}

	// Encode response (items in sequence, no array wrapper)
	response := amf0.Array{
		"_result",
		transIDFloat, // Use the transaction ID from the connect command (as float64)
		cmdObj,
		info,
	}

	body, err := amf0.EncodeCommand(response)
	if err != nil {
		return err
	}

	// Debug: log first bytes of encoded response
	if len(body) > 0 {
		log.Printf("Connect response: first byte=0x%02x (should be 0x02 for string), len=%d, first 20 bytes: %x", body[0], len(body), body[:min(20, len(body))])
	}

	return s.WriteMessage(3, rtmpprotocol.MessageTypeCommandAMF0, 0, 0, body)
}

// HandleCreateStream handles the createStream command.
func (s *ServiceSession) HandleCreateStream(command amf0.Array) error {
	streamID := s.nextStreamID
	s.nextStreamID++

	// Send _result with stream ID
	result := amf0.Array{
		"_result",
		command[1], // Transaction ID
		nil,
		float64(streamID),
	}

	body, err := amf0.EncodeCommand(result)
	if err != nil {
		return err
	}

	return s.WriteMessage(3, rtmpprotocol.MessageTypeCommandAMF0, 0, 0, body)
}

// HandlePublish handles the publish command.
// streamID is the stream ID from the message header where the publish command was received.
func (s *ServiceSession) HandlePublish(command amf0.Array, streamID uint32) error {
	if len(command) < 3 {
		return fmt.Errorf("invalid publish command")
	}

	streamName, ok := command[2].(string)
	if !ok {
		return fmt.Errorf("stream name not found")
	}

	app := s.GetApp()
	if app == "" {
		return fmt.Errorf("app not set")
	}

	// Create stream key
	streamKey := bus.NewStreamKey(app, streamName)

	// Get or create stream
	stream, created := s.registry.GetOrCreate(streamKey)
	if !created {
		log.Printf("Stream %s already exists", streamKey)
	}

	// Attach publisher (FIXME: If publisher already exists, reject deterministically)
	publisherID := uint64(1) // Simple ID for now
	if !stream.AttachPublisher(publisherID) {
		return fmt.Errorf("stream already has a publisher")
	}

	// Create publisher
	s.publisher = NewPublisher(s.Session, stream, publisherID)
	s.SetStreamName(streamName)
	s.SetState(rtmpprotocol.StatePublishing)

	// Send onStatus response on the same stream ID as the publish command
	return s.SendPublishStatus("status", "NetStream.Publish.Start", "Start publishing", streamID)
}

// SendPublishStatus sends an onStatus message.
// streamID is the stream ID from the publish command message header.
func (s *ServiceSession) SendPublishStatus(level, code, description string, streamID uint32) error {
	status := amf0.Object{
		"level":       level,
		"code":        code,
		"description": description,
	}

	response := amf0.Array{
		"onStatus",
		float64(0),
		nil,
		status,
	}

	body, err := amf0.EncodeCommand(response)
	if err != nil {
		return err
	}

	// Send on status message on the same stream ID as the publish command
	return s.WriteMessage(5, rtmpprotocol.MessageTypeCommandAMF0, 0, streamID, body)
}

// HandleMediaMessage handles audio/video/data messages.
func (s *ServiceSession) HandleMediaMessage(msgType byte, timestamp uint32, body []byte) {
	if s.publisher == nil {
		return // Not publishing
	}

	switch msgType {
	case rtmpprotocol.MessageTypeAudio:
		s.publisher.PublishAudio(timestamp, body)
	case rtmpprotocol.MessageTypeVideo:
		s.publisher.PublishVideo(timestamp, body)
	case rtmpprotocol.MessageTypeDataAMF0:
		s.publisher.PublishMetadata(timestamp, body)
	default:
		// NOTE: Other message types are ignored
	}
}

// Close closes the session and detaches publisher.
func (s *ServiceSession) Close() {
	if s.publisher != nil {
		s.publisher.Detach()
		// Try to remove empty stream
		if s.publisher.stream != nil {
			streamKey := s.publisher.StreamKey()
			s.registry.RemoveIfEmpty(streamKey)
		}
	}
	s.Session.Close()
}

// createWindowAckSizeBody creates a window acknowledgement size message body.
func createWindowAckSizeBody(size uint32) []byte {
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body, size)
	return body
}

// createSetPeerBandwidthBody creates a set peer bandwidth message body.
func createSetPeerBandwidthBody(size uint32, limitType byte) []byte {
	body := make([]byte, 5)
	binary.BigEndian.PutUint32(body[0:4], size)
	body[4] = limitType
	return body
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
