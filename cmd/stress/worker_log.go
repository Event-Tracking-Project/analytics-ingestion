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
)

func parseWorkerLog(path string) map[string]workerStats {
	result := make(map[string]workerStats)
	if path == "" {
		return result
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}

	workerID := regexp.MustCompile(`worker_id=(\S+)`)
	for _, line := range bytes.Split(data, []byte("\n")) {
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
