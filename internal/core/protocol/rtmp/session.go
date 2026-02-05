// If you are AI: This file manages RTMP session state and protocol handling.
// Session tracks connection state, chunk parser, and message handling.

package rtmp

import (
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
	mu         sync.RWMutex
}

// NewSession creates a new RTMP session.
func NewSession(conn io.ReadWriter) *Session {
	return &Session{
		conn:      conn,
		parser:    NewChunkParser(),
		state:     StateHandshaking,
		chunkSize: DefaultChunkSize,
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
func (s *Session) GetCompleteMessage(csID uint32) ([]byte, byte, uint32, bool) {
	return s.parser.GetCompleteMessage(csID)
}

// WriteMessage writes a message as chunks.
func (s *Session) WriteMessage(csID uint32, msgType byte, timestamp uint32, streamID uint32, body []byte) error {
	s.mu.RLock()
	chunkSize := s.chunkSize
	s.mu.RUnlock()
	return WriteChunk(s.conn, csID, msgType, timestamp, streamID, body, chunkSize)
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
