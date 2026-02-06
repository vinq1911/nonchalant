// If you are AI: This file implements pull relay functionality.
// Pull relay connects to remote RTMP server, plays stream, and republishes locally.

package relay

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"nonchalant/internal/core/bus"
	rtmpprotocol "nonchalant/internal/core/protocol/rtmp"
	"time"
)

// PullTask implements pull relay (connect to remote, play, republish locally).
type PullTask struct {
	*BaseTask
}

// NewPullTask creates a new pull relay task.
func NewPullTask(registry *bus.Registry, app, name, remoteURL string, reconnect bool) *PullTask {
	return &PullTask{
		BaseTask: NewBaseTask(registry, app, name, remoteURL, reconnect),
	}
}

// Start starts the pull relay task.
// Connects to remote RTMP server, plays stream, and republishes locally.
// NOTE: This is a simplified implementation. Full RTMP client protocol
// would require more complex command handling.
func (t *PullTask) Start(ctx context.Context) error {
	t.SetRunning(true)
	defer t.SetRunning(false)

	// Parse remote URL
	u, err := url.Parse(t.RemoteURL())
	if err != nil {
		return fmt.Errorf("invalid remote URL: %w", err)
	}

	host := u.Host
	if u.Port() == "" {
		host += ":1935" // Default RTMP port
	}

	// Connect loop with reconnect support
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.StopChan():
			return nil
		default:
		}

		// Connect to remote server
		conn, err := net.DialTimeout("tcp", host, 5*time.Second)
		if err != nil {
			if !t.reconnect {
				return fmt.Errorf("connect failed: %w", err)
			}
			// Wait before reconnect (bounded to prevent storms)
			select {
			case <-time.After(5 * time.Second):
				continue
			case <-ctx.Done():
				return ctx.Err()
			case <-t.StopChan():
				return nil
			}
		}

		// Perform client handshake
		if err := rtmpprotocol.PerformClientHandshake(conn); err != nil {
			conn.Close()
			if !t.reconnect {
				return fmt.Errorf("handshake failed: %w", err)
			}
			select {
			case <-time.After(5 * time.Second):
				continue
			case <-ctx.Done():
				return ctx.Err()
			case <-t.StopChan():
				return nil
			}
		}

		// Create session for reading messages
		session := rtmpprotocol.NewSession(conn)

		// Get or create local stream
		streamKey := bus.NewStreamKey(t.App(), t.Name())
		stream, _ := t.Registry().GetOrCreate(streamKey)

		// Attach as publisher
		publisherID := uint64(1)
		if !stream.AttachPublisher(publisherID) {
			// Publisher already exists, skip
			conn.Close()
			return fmt.Errorf("stream already has publisher")
		}
		defer stream.DetachPublisher()

		// NOTE: Full implementation would:
		// 1. Send connect command
		// 2. Send createStream command
		// 3. Send play command
		// 4. Read media messages and republish
		// For now, this is a placeholder that demonstrates lifecycle

		// Run until connection closes or context cancelled
		done := make(chan error, 1)
		go func() {
			// Read chunks and process messages
			for {
				csID, err := session.ReadChunk()
				if err != nil {
					done <- err
					return
				}
				// Process message (simplified - would need full command handling)
				_, _, _, _ = session.GetCompleteMessage(csID)
			}
		}()

		select {
		case err := <-done:
			conn.Close()
			if !t.reconnect {
				return err
			}
			// Reconnect after delay
			select {
			case <-time.After(5 * time.Second):
				continue
			case <-ctx.Done():
				return ctx.Err()
			case <-t.StopChan():
				return nil
			}
		case <-ctx.Done():
			conn.Close()
			return ctx.Err()
		case <-t.StopChan():
			conn.Close()
			return nil
		}
	}
}
