/*
cmd/stress/html.go
HTML stress test report builder
*/
package main

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func writeHTMLReport(path string, result report, reportJSON []byte) error {
	var workers strings.Builder
	workerNames := make([]string, 0, len(result.Workers))
	for name := range result.Workers {
		workerNames = append(workerNames, name)
	}
	sort.Strings(workerNames)
	for _, name := range workerNames {
		stats := result.Workers[name]
		fmt.Fprintf(&workers, `<tr><td>%s</td><td>%d</td><td>%d</td></tr>`,
			html.EscapeString(name), stats.Validated, stats.Processed)
	}
	if len(workerNames) == 0 {
		workers.WriteString(`<tr><td colspan="3" class="muted">No worker log entries found</td></tr>`)
	}

	content := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Stress test %s</title><style>%s</style></head>
<body><main class="container">
<p><a href="index.html">← All stress tests</a></p>
<h1>Stress test report</h1><p class="muted">%s</p>
<section class="cards">
<div><span>Requests/sec</span><strong>%.2f</strong></div>
<div><span>Events/sec</span><strong>%.2f</strong></div>
<div><span>Success rate</span><strong>%.2f%%</strong></div>
<div><span>Duration</span><strong>%.2fs</strong></div>
</section>
<section class="panel"><h2>Run parameters</h2><dl class="facts">
<dt>Started</dt><dd>%s</dd><dt>Endpoint</dt><dd>%s</dd>
<dt>Batches</dt><dd>%d</dd><dt>Batch size</dt><dd>%d</dd>
<dt>Client concurrency</dt><dd>%d</dd><dt>Submitted bytes</dt><dd>%d</dd>
</dl></section>
<section class="panel"><h2>Latency (ms)</h2>
<table><tr><th>Min</th><th>P50</th><th>P95</th><th>P99</th><th>Max</th></tr>
<tr><td>%.2f</td><td>%.2f</td><td>%.2f</td><td>%.2f</td><td>%.2f</td></tr></table></section>
<section class="panel"><h2>Workers</h2>
<table><tr><th>Worker ID</th><th>Validated</th><th>Processed</th></tr>%s</table></section>
<section class="panel"><h2>Raw JSON</h2><pre>%s</pre></section>
</main></body></html>`,
		html.EscapeString(result.StartedAt.Format(time.RFC3339)), reportStyles,
		html.EscapeString(result.StartedAt.Format(time.RFC3339)),
		result.RequestsPerSecond, result.EventsPerSecond, successRate(result),
		result.DurationSeconds, html.EscapeString(result.StartedAt.Format(time.RFC3339)),
		html.EscapeString(result.URL), result.Batches, result.BatchSize,
		result.Concurrency, result.SubmittedBytes, result.LatencyMS.Min,
		result.LatencyMS.P50, result.LatencyMS.P95, result.LatencyMS.P99,
		result.LatencyMS.Max, workers.String(), html.EscapeString(string(reportJSON)))
	return os.WriteFile(path, []byte(content), 0644)
}

func writeReportManifest(outputDir string) error {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return err
	}

	type manifestEntry struct {
		File   string `json:"file"`
		Report report `json:"report"`
	}
	manifest := make([]manifestEntry, 0)
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "reports.json" || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(outputDir, entry.Name()))
		if err != nil {
			return err
		}
		var result report
		if err := json.Unmarshal(data, &result); err != nil {
			return fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		manifest = append(manifest, manifestEntry{File: entry.Name(), Report: result})
	}
	sort.Slice(manifest, func(i, j int) bool {
		return manifest[i].Report.StartedAt.After(manifest[j].Report.StartedAt)
	})
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outputDir, "reports.json"), data, 0644)
}

const reportStyles = `
:root { color-scheme: dark; font-family: Inter, system-ui, sans-serif; background: #0f172a; color: #e2e8f0; }
body { margin: 0; background: linear-gradient(135deg, #0f172a, #172554); min-height: 100vh; }
.container { max-width: 1180px; margin: 0 auto; padding: 40px 24px 64px; }
h1 { font-size: 2.2rem; margin-bottom: 8px; } h2 { margin-top: 0; }
a { color: #67e8f9; } .muted { color: #94a3b8; }
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 16px; margin: 28px 0; }
.cards div, .panel { background: rgba(15, 23, 42, .82); border: 1px solid #334155; border-radius: 14px; box-shadow: 0 12px 28px rgba(0,0,0,.18); }
.cards div { padding: 20px; } .cards span { display: block; color: #94a3b8; font-size: .85rem; }
.cards strong { display: block; margin-top: 8px; font-size: 1.7rem; color: #a5f3fc; }
.panel { padding: 22px; margin: 18px 0; overflow-x: auto; }
table { border-collapse: collapse; width: 100%; } th, td { border-bottom: 1px solid #334155; padding: 12px 10px; text-align: left; white-space: nowrap; }
th { color: #bae6fd; cursor: pointer; user-select: none; } th:hover { background: #1e3a5f; } tr:hover { background: rgba(30, 58, 95, .35); }
.facts { display: grid; grid-template-columns: 180px 1fr; gap: 10px; } dt { color: #94a3b8; } dd { margin: 0; word-break: break-word; }
pre { white-space: pre-wrap; overflow-wrap: anywhere; color: #cbd5e1; }`
