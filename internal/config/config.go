/*
internal/config/config.go
This file contains function for config extraction
Takes variables from yaml for use
*/
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Main config struct
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Logging   LoggingConfig   `yaml:"logging"`
	Ingestion IngestionConfig `yaml:"ingestion"`
}

// Config for server
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// Logging settings struct
type LoggingConfig struct {
	Enabled      bool          `yaml:"enabled"`
	Level        string        `yaml:"level"`
	Destination  string        `yaml:"destination"`
	File         LogFileConfig `yaml:"file"`
	FailedEvents bool          `yaml:"failed_events"`
}

// File path for logging if available
type LogFileConfig struct {
	Path string `yaml:"path"`
}

// Determines max batch ingestion
type IngestionConfig struct {
	MaxBatchSize int `yaml:"max_batch_size"`
}

// Loads config
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}
