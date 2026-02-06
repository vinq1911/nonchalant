// If you are AI: This file defines the relay task interface and base implementation.
// Tasks manage the lifecycle of pull or push relays.

package relay

import (
	"context"
	"nonchalant/internal/core/bus"
)

// Task represents a relay task (pull or push).
// Tasks run in their own goroutines and manage connection lifecycle.
type Task interface {
	// Start starts the relay task.
	// Should run until context is cancelled or error occurs.
	Start(ctx context.Context) error

	// Stop stops the relay task cleanly.
	Stop() error

	// IsRunning returns true if the task is currently running.
	IsRunning() bool
}

// BaseTask provides common functionality for relay tasks.
type BaseTask struct {
	registry  *bus.Registry
	app       string
	name      string
	remoteURL string
	reconnect bool
	running   bool
	stopChan  chan struct{}
}

// NewBaseTask creates a new base task with common configuration.
func NewBaseTask(registry *bus.Registry, app, name, remoteURL string, reconnect bool) *BaseTask {
	return &BaseTask{
		registry:  registry,
		app:       app,
		name:      name,
		remoteURL: remoteURL,
		reconnect: reconnect,
		stopChan:  make(chan struct{}),
	}
}

// App returns the application name.
func (t *BaseTask) App() string {
	return t.app
}

// Name returns the stream name.
func (t *BaseTask) Name() string {
	return t.name
}

// RemoteURL returns the remote RTMP URL.
func (t *BaseTask) RemoteURL() string {
	return t.remoteURL
}

// Registry returns the bus registry.
func (t *BaseTask) Registry() *bus.Registry {
	return t.registry
}

// IsRunning returns true if the task is running.
func (t *BaseTask) IsRunning() bool {
	return t.running
}

// SetRunning sets the running state.
func (t *BaseTask) SetRunning(running bool) {
	t.running = running
}

// StopChan returns the stop channel.
func (t *BaseTask) StopChan() <-chan struct{} {
	return t.stopChan
}

// Stop signals the task to stop.
func (t *BaseTask) Stop() error {
	close(t.stopChan)
	return nil
}
