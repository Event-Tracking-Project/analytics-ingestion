/*
internal/config/logging.go
This file contains functions for logging configs
Takes config data from a yaml file to configure the api logging
*/
package config

import (
	"io"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

// Configure logging function that takes in config struct
func ConfigureLogging(cfg LoggingConfig) error {

	// Format log based on destination
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
		ForceColors:   cfg.Destination == "stdout",
	})

	// Checks if logging is enabled
	if !cfg.Enabled {
		log.SetOutput(io.Discard)
		return nil
	}

	// Checks log level recording/output
	switch cfg.Level {
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "info":
		log.SetLevel(log.InfoLevel)
	case "warn":
		log.SetLevel(log.WarnLevel)
	case "error":
		log.SetLevel(log.ErrorLevel)
	default:
		log.SetLevel(log.InfoLevel)
	}

	// Checks log output destination
	switch cfg.Destination {
	case "stdout":
		log.SetOutput(os.Stdout)

	// File output. Checks if file is available and will create logs
	case "file":
		if err := os.MkdirAll(filepath.Dir(cfg.File.Path), 0755); err != nil {
			return err
		}

		file, err := os.OpenFile(
			cfg.File.Path,
			os.O_CREATE|os.O_WRONLY|os.O_APPEND,
			0666,
		)
		if err != nil {
			return err
		}

		log.SetOutput(file)

	default:
		log.SetOutput(os.Stdout)
	}

	return nil
}
