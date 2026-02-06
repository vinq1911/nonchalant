// If you are AI: This file defines the configuration structure for nonchalant.
// It uses strict YAML decoding and explicit defaults.

package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds the complete server configuration.
// All fields must have explicit defaults or be required.
type Config struct {
	Server    ServerConfig     `yaml:"server"`
	Relays    []RelayConfig    `yaml:"relays,omitempty"`
	Transcode *TranscodeConfig `yaml:"transcode,omitempty"`
}

// ServerConfig defines HTTP server settings.
type ServerConfig struct {
	HealthPort int `yaml:"health_port"` // Port for health endpoint
	HTTPPort   int `yaml:"http_port"`   // Port for future HTTP services
	RTMPPort   int `yaml:"rtmp_port"`   // Port for future RTMP service
}

// RelayConfig defines a relay task configuration.
type RelayConfig struct {
	App       string `yaml:"app"`                 // Application name
	Name      string `yaml:"name"`                // Stream name
	Mode      string `yaml:"mode"`                // "pull" or "push"
	RemoteURL string `yaml:"remote_url"`          // Remote RTMP URL
	Reconnect bool   `yaml:"reconnect,omitempty"` // Enable reconnect on failure
}

// TranscodeConfig defines transcoding configuration.
// Only used when built with -tags ffmpeg.
type TranscodeConfig struct {
	Enabled  bool               `yaml:"enabled"`            // Enable transcoding
	Profiles []TranscodeProfile `yaml:"profiles,omitempty"` // Transcoding profiles
}

// TranscodeProfile defines a transcoding profile.
type TranscodeProfile struct {
	Name      string `yaml:"name"`       // Profile name
	App       string `yaml:"app"`        // Source application
	Stream    string `yaml:"stream"`     // Source stream name
	Format    string `yaml:"format"`     // Output format (hls, dash, etc.)
	OutputURL string `yaml:"output_url"` // Output URL
}

// Load reads configuration from a YAML file.
// Returns an error if the file cannot be read or decoded.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true) // Reject unknown fields

	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	// Apply defaults
	cfg.setDefaults()

	return &cfg, nil
}

// setDefaults applies explicit default values to unset fields.
func (c *Config) setDefaults() {
	if c.Server.HealthPort == 0 {
		c.Server.HealthPort = 8080
	}
	if c.Server.HTTPPort == 0 {
		c.Server.HTTPPort = 8081
	}
	if c.Server.RTMPPort == 0 {
		c.Server.RTMPPort = 1935
	}
}
