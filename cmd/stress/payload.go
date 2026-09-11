/*
cmd/stress/payload.go
Payload structs for evnts and batches when stress testing
*/
package main

import "fmt"

type eventPayload struct {
	Name       string         `json:"event"`
	Timestamp  int64          `json:"timestamp"`
	ProjectID  string         `json:"projectid"`
	OrgID      string         `json:"orgid"`
	FunnelID   string         `json:"funnelid"`
	Properties map[string]any `json:"properties"`
}

type batchPayload struct {
	BatchID string         `json:"batch_id"`
	Events  []eventPayload `json:"events"`
}

func makeBatch(id int64, size int, invalidRate float64) (batchPayload, error) {
	events := make([]eventPayload, size)
	for i := range events {
		name, project, org := "stress_event", "stress_project", "stress_org"
		if invalidRate > 0 && float64((id+int64(i))%1000)/1000 < invalidRate {
			name, project = "", ""
		}
		events[i] = eventPayload{
			Name: name, Timestamp: nowMillis(), ProjectID: project,
			OrgID: org, FunnelID: "stress_funnel",
			Properties: map[string]any{"sequence": i},
		}
	}
	return batchPayload{
		BatchID: fmt.Sprintf("stress-%d-%d", nowMillis(), id),
		Events:  events,
	}, nil
}
