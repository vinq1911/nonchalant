// If you are AI: This file validates configuration values and returns descriptive errors.

package config

import (
	"fmt"
)

// Validate checks that all configuration values are within acceptable ranges.
// Returns an error describing the first validation failure found.
func (c *Config) Validate() error {
	if err := c.Server.Validate(); err != nil {
		return fmt.Errorf("server config: %w", err)
	}
	return nil
}

// Validate checks server configuration values.
func (s *ServerConfig) Validate() error {
	if s.HealthPort <= 0 || s.HealthPort > 65535 {
		return fmt.Errorf("health_port must be between 1 and 65535, got %d", s.HealthPort)
	}
	if s.HTTPPort <= 0 || s.HTTPPort > 65535 {
		return fmt.Errorf("http_port must be between 1 and 65535, got %d", s.HTTPPort)
	}
	if s.RTMPPort <= 0 || s.RTMPPort > 65535 {
		return fmt.Errorf("rtmp_port must be between 1 and 65535, got %d", s.RTMPPort)
	}
	if s.HealthPort == s.HTTPPort {
		return fmt.Errorf("health_port and http_port must be different, both are %d", s.HealthPort)
	}
	if s.HealthPort == s.RTMPPort {
		return fmt.Errorf("health_port and rtmp_port must be different, both are %d", s.HealthPort)
	}
	if s.HTTPPort == s.RTMPPort {
		return fmt.Errorf("http_port and rtmp_port must be different, both are %d", s.HTTPPort)
	}
	return nil
}
