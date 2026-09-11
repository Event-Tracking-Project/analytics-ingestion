/*
cmd/stress/worker_log.go
Contains code for worker logs when stress testing
*/
package main

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"time"
)

func parseWorkerLog(path string, since time.Time) map[string]workerStats {
	result := make(map[string]workerStats)
	if path == "" {
		return result
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}

	workerID := regexp.MustCompile(`worker_id=(\S+)`)
	logTime := regexp.MustCompile(`time="([^"]+)"`)
	for _, line := range bytes.Split(data, []byte("\n")) {
		if !since.IsZero() {
			timeMatch := logTime.FindSubmatch(line)
			if len(timeMatch) != 2 {
				continue
			}
			timestamp, err := time.Parse(time.RFC3339, string(timeMatch[1]))
			if err != nil || timestamp.Before(since) {
				continue
			}
		}

		match := workerID.FindSubmatch(line)
		if len(match) != 2 {
			continue
		}
		name := string(match[1])
		stats := result[name]
		if strings.Contains(string(line), "Worker processed batch") {
			stats.Processed++
		}
		if strings.Contains(string(line), "Worker validated batch") {
			stats.Validated++
		}
		result[name] = stats
	}
	return result
}
