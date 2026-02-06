// If you are AI: This file implements push relay functionality.
// Push relay subscribes to local stream and publishes to remote RTMP server.

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

// PushTask implements push relay (subscribe local, publish remote).
type PushTask struct {
	*BaseTask
}

// NewPushTask creates a new push relay task.
func NewPushTask(registry *bus.Registry, app, name, remoteURL string, reconnect bool) *PushTask {
	return &PushTask{
		BaseTask: NewBaseTask(registry, app, name, remoteURL, reconnect),
	}
}

// Start starts the push relay task.
// Subscribes to local stream and publishes to remote RTMP server.
// NOTE: This is a simplified implementation. Full RTMP client protocol
// would require more complex command handling.
func (t *PushTask) Start(ctx context.Context) error {
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

	// Get local stream
	streamKey := bus.NewStreamKey(t.App(), t.Name())
	stream := t.Registry().Get(streamKey)
	if stream == nil || !stream.HasPublisher() {
		if !t.reconnect {
			return fmt.Errorf("local stream not found or has no publisher")
		}
		// Wait and retry
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.StopChan():
				return nil
			case <-time.After(5 * time.Second):
				stream = t.Registry().Get(streamKey)
				if stream != nil && stream.HasPublisher() {
					break
				}
			}
		}
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

		// Create session for writing messages
		_ = rtmpprotocol.NewSession(conn)

		// Attach subscriber to local stream
		// Use drop oldest to prevent blocking local publisher
		subscriber, subID := stream.AttachSubscriber(1000, bus.BackpressureDropOldest)
		defer stream.DetachSubscriber(subID)

		// NOTE: Full implementation would:
		// 1. Send connect command
		// 2. Send createStream command
		// 3. Send publish command
		// 4. Read messages from subscriber and write to remote
		// For now, this is a placeholder that demonstrates lifecycle

		// Process messages from local stream
		done := make(chan error, 1)
		go func() {
			for {
				msg, ok := subscriber.Buffer().Read()
				if !ok {
					// Buffer empty, continue
					time.Sleep(10 * time.Millisecond)
					continue
				}

				// Write message to remote (simplified)
				// Full implementation would convert to RTMP message format
				_ = msg
			}
		}()

		select {
		case err := <-done:
			conn.Close()
			if !t.reconnect {
				return err
			}
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
