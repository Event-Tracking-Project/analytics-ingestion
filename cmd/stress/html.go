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
		workers.WriteString(`<tr><td colspan="3" class="muted">No worker log entries found for this run</td></tr>`)
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
:root { color-scheme: dark; --bg: #221a0f; --surface: #2b2117; --surface-raised: #36291c; --border: #55402c; --text: #d3af86; --muted: #a18a70; --accent: #f06431; --accent-soft: #c88b5a; --green: #889b4a; --yellow: #f9b256; font-family: "Cascadia Code", "SFMono-Regular", Consolas, monospace; }
* { box-sizing: border-box; }
body { margin: 0; min-height: 100vh; color: var(--text); background: var(--bg); }
body::before { content: ""; position: fixed; inset: 0; pointer-events: none; opacity: .16; background: linear-gradient(90deg, transparent 49%, #5c452e 50%, transparent 51%); background-size: 80px 80px; }
.container { position: relative; max-width: 1060px; margin: 0 auto; padding: 42px 24px 64px; }
h1, h2, p { margin-top: 0; } h1 { margin-bottom: 8px; color: #e7c99f; font-size: 1.8rem; } h2 { color: #e7c99f; font-size: 1rem; }
a { color: var(--accent); } .muted { color: var(--muted); font-size: .78rem; line-height: 1.6; }
.cards { display: grid; grid-template-columns: repeat(4, minmax(130px, 1fr)); gap: 12px; margin: 28px 0 18px; }
.cards div, .panel { border: 1px solid var(--border); border-radius: 6px; background: rgba(43, 33, 23, .9); }
.cards div { padding: 18px; } .cards span { display: block; color: var(--muted); font-size: .7rem; }
.cards strong { display: block; margin-top: 10px; color: var(--yellow); font-size: 1.3rem; font-weight: normal; }
.panel { padding: 22px; margin-bottom: 18px; overflow-x: auto; } .panel h2 { padding-bottom: 12px; border-bottom: 1px solid var(--border); }
table { width: 100%; border-collapse: collapse; font-size: .76rem; } th, td { padding: 11px 8px; border-bottom: 1px solid var(--border); text-align: left; } th { color: var(--accent-soft); font-weight: normal; }
.facts { display: grid; grid-template-columns: 190px 1fr; gap: 11px; margin: 0; font-size: .78rem; } dt { color: var(--muted); } dd { margin: 0; overflow-wrap: anywhere; }
pre { white-space: pre-wrap; overflow-wrap: anywhere; color: var(--text); font-size: .76rem; }
@media (max-width: 800px) { .container { padding: 28px 18px; } .cards { grid-template-columns: repeat(2, 1fr); } }`
