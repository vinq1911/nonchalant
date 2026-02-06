// If you are AI: This file manages RTMP session state and protocol handling.
// Session tracks connection state, chunk parser, and message handling.

package rtmp

import (
	"encoding/binary"
	"io"
	"sync"
)

// SessionState represents the current state of an RTMP session.
type SessionState int

const (
	StateHandshaking SessionState = iota
	StateConnected
	StatePublishing
	StateClosed
)

// Session manages an RTMP connection session.
// Handles chunk parsing, message routing, and state management.
type Session struct {
	conn       io.ReadWriter
	parser     *ChunkParser
	state      SessionState
	chunkSize  uint32
	app        string
	streamName string
	streamID   uint32
	// ACK tracking for RTMP protocol
	ackSize   uint32 // Window acknowledgement size we sent to client
	inAckSize uint32 // Total bytes received from client
	inLastAck uint32 // Last ACK value we sent
	mu        sync.RWMutex
}

// NewSession creates a new RTMP session.
func NewSession(conn io.ReadWriter) *Session {
	return &Session{
		conn:      conn,
		parser:    NewChunkParser(),
		state:     StateHandshaking,
		chunkSize: DefaultChunkSize,
		ackSize:   0, // Will be set when we send Window Acknowledgement Size
	}
}

// PerformHandshake performs the RTMP handshake.
func (s *Session) PerformHandshake() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateHandshaking {
		return ErrHandshakeFailed
	}
	if err := PerformServerHandshake(s.conn); err != nil {
		return err
	}
	s.state = StateConnected
	return nil
}

// ReadChunk reads a chunk from the connection.
func (s *Session) ReadChunk() (uint32, error) {
	return s.parser.ReadChunk(s.conn)
}

// GetCompleteMessage gets a complete message if available.
// Returns: body, messageType, timestamp, streamID, complete
func (s *Session) GetCompleteMessage(csID uint32) ([]byte, byte, uint32, uint32, bool) {
	return s.parser.GetCompleteMessage(csID)
}

// WriteMessage writes a message as chunks.
func (s *Session) WriteMessage(csID uint32, msgType byte, timestamp uint32, streamID uint32, body []byte) error {
	s.mu.RLock()
	chunkSize := s.chunkSize
	s.mu.RUnlock()
	return WriteChunk(s.conn, csID, msgType, timestamp, streamID, body, chunkSize)
}

// SetAckSize sets the window acknowledgement size (sent to client).
// This tells the client how often we will acknowledge bytes received.
func (s *Session) SetAckSize(size uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackSize = size
}

// RecordBytesReceived records bytes received from client and sends ACK if needed.
// Returns true if an ACK was sent.
func (s *Session) RecordBytesReceived(bytesRead uint32) (bool, error) {
	s.mu.Lock()
	s.inAckSize += bytesRead
	// Handle overflow (RTMP spec: reset at 0xf0000000)
	if s.inAckSize >= 0xf0000000 {
		s.inAckSize = 0
		s.inLastAck = 0
	}
	ackSize := s.ackSize
	inAckSize := s.inAckSize
	inLastAck := s.inLastAck
	s.mu.Unlock()

	// Send ACK if we've received enough bytes
	if ackSize > 0 && inAckSize-inLastAck >= ackSize {
		if err := s.SendACK(inAckSize); err != nil {
			return false, err
		}
		s.mu.Lock()
		s.inLastAck = inAckSize
		s.mu.Unlock()
		return true, nil
	}
	return false, nil
}

// SendACK sends an acknowledgement message to the client.
func (s *Session) SendACK(ackSize uint32) error {
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body, ackSize)
	return s.WriteMessage(2, MessageTypeAck, 0, 0, body)
}

// SetChunkSize sets the chunk size for this session.
func (s *Session) SetChunkSize(size uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunkSize = size
	s.parser.SetChunkSize(size)
}

// GetChunkSize returns the current chunk size.
func (s *Session) GetChunkSize() uint32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.chunkSize
}

// SetApp sets the application name.
func (s *Session) SetApp(app string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.app = app
}

// GetApp returns the application name.
func (s *Session) GetApp() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.app
}

// SetStreamName sets the stream name.
func (s *Session) SetStreamName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streamName = name
}

// GetStreamName returns the stream name.
func (s *Session) GetStreamName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.streamName
}

// SetState sets the session state.
func (s *Session) SetState(state SessionState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
}

// GetState returns the current session state.
func (s *Session) GetState() SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Close closes the session.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = StateClosed
	if closer, ok := s.conn.(io.Closer); ok {
		closer.Close()
	}
}
