// If you are AI: This file implements the relay manager.
// Manages lifecycle of all relay tasks (start, stop, restart).

package relay

import (
	"context"
	"fmt"
	"nonchalant/internal/config"
	"nonchalant/internal/core/bus"
	"sync"
)

// Manager manages relay tasks lifecycle.
type Manager struct {
	registry *bus.Registry
	tasks    []Task
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
}

// NewManager creates a new relay manager.
func NewManager(registry *bus.Registry) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		registry: registry,
		tasks:    make([]Task, 0),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// StartTasks starts all relay tasks from configuration.
func (m *Manager) StartTasks(cfg *config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, relayCfg := range cfg.Relays {
		// Validate configuration
		if relayCfg.App == "" || relayCfg.Name == "" {
			return fmt.Errorf("relay config missing app or name")
		}
		if relayCfg.Mode != "pull" && relayCfg.Mode != "push" {
			return fmt.Errorf("invalid relay mode: %s (must be 'pull' or 'push')", relayCfg.Mode)
		}
		if relayCfg.RemoteURL == "" {
			return fmt.Errorf("relay config missing remote_url")
		}

		var task Task
		if relayCfg.Mode == "pull" {
			task = NewPullTask(m.registry, relayCfg.App, relayCfg.Name, relayCfg.RemoteURL, relayCfg.Reconnect)
		} else {
			task = NewPushTask(m.registry, relayCfg.App, relayCfg.Name, relayCfg.RemoteURL, relayCfg.Reconnect)
		}

		m.tasks = append(m.tasks, task)

		// Start task in goroutine
		m.wg.Add(1)
		go func(t Task) {
			defer m.wg.Done()
			if err := t.Start(m.ctx); err != nil {
				// Log error but don't fail manager
				// NOTE: In production, this should be logged
			}
		}(task)
	}

	return nil
}

// Stop stops all relay tasks and waits for them to finish.
// FIXME: If a task cannot stop cleanly, it may block shutdown.
// Workaround: Use context timeout in caller.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Cancel context to signal all tasks to stop
	m.cancel()

	// Stop all tasks
	for _, task := range m.tasks {
		task.Stop()
	}

	// Wait for all tasks to finish
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-m.ctx.Done():
		// Context already cancelled
		return nil
	}
}

// TaskCount returns the number of active relay tasks.
func (m *Manager) TaskCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tasks)
}
