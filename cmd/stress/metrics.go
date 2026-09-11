/*
cmd/stress/metrics.go
Metrics calculations based on stress test
*/
package main

import (
	"sort"
	"time"
)

func nowMillis() int64 {
	return time.Now().UnixMilli()
}

func sortLatencies(values []float64) {
	sort.Float64s(values)
}

func summarize(values []float64) latencyStats {
	if len(values) == 0 {
		return latencyStats{}
	}
	return latencyStats{
		Min: values[0], P50: percentile(values, 0.50),
		P95: percentile(values, 0.95), P99: percentile(values, 0.99),
		Max: values[len(values)-1],
	}
}

func percentile(values []float64, ratio float64) float64 {
	index := int(float64(len(values)-1) * ratio)
	return values[index]
}

func successRate(result report) float64 {
	if result.Batches == 0 {
		return 0
	}
	return float64(result.SuccessfulRequests) / float64(result.Batches) * 100
}
