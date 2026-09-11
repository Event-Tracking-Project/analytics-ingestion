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
	Redis     RedisConfig     `yaml:"redis"`
	Database  DatabaseConfig  `yaml:"database"`
	Logging   LoggingConfig   `yaml:"logging"`
	Ingestion IngestionConfig `yaml:"ingestion"`
	Workers   WorkerConfig    `yaml:"workers"`
}

// Config for server
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// Redis config
type RedisConfig struct {
	Address       string `yaml:"address"`
	Password      string `yaml:"password"`
	Database      int    `yaml:"database"`
	Stream        string `yaml:"stream"`
	ConsumerGroup string `yaml:"consumer_group"`
}

// Database config
type DatabaseConfig struct {
	Host           string `yaml:"host"`
	Port           string `yaml:"port"`
	Name           string `yaml:"name"`
	User           string `yaml:"user"`
	Password       string `yaml:"password"`
	SslMode        string `yaml:"ssl_mode"`
	MaxConnections int    `yaml:"max_connections"`
	MinConnections int    `yaml:"min_connections"`
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

type WorkerConfig struct {
	WorkerCount   int  `yaml:"count"`
	StartOnDemand bool `yaml:"start_on_demand"`
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
