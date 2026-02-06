// If you are AI: This file manages RTMP service session handling.
// Handles command processing and publish lifecycle.

package rtmp

import (
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

	// Try to extract app from command object if present
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
			log.Printf("Connect command object is not an object (type: %T), using default app", v)
		}

		if cmdObj != nil {
			if appVal, ok := cmdObj["app"].(string); ok {
				app = appVal
			}
		}
	}

	s.SetApp(app)

	// Send _result response
	return s.SendConnectResult()
}

// SendConnectResult sends the connect _result response.
func (s *ServiceSession) SendConnectResult() error {
	// Create response object
	result := amf0.Object{
		"fmsVer":       "FMS/3,0,1,123",
		"capabilities": float64(31),
	}
	info := amf0.Object{
		"level":          "status",
		"code":           "NetConnection.Connect.Success",
		"description":    "Connection succeeded.",
		"objectEncoding": float64(0),
	}

	// Encode response
	response := amf0.Array{
		"_result",
		float64(1), // Transaction ID
		result,
		info,
	}

	body, err := amf0.EncodeCommand(response)
	if err != nil {
		return err
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
func (s *ServiceSession) HandlePublish(command amf0.Array) error {
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

	// Send onStatus response
	return s.SendPublishStatus("status", "NetStream.Publish.Start", "Start publishing")
}

// SendPublishStatus sends an onStatus message.
func (s *ServiceSession) SendPublishStatus(level, code, description string) error {
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

	streamID := s.nextStreamID - 1
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
