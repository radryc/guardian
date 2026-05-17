package history

import "time"

type EventRecord struct {
	APIVersion         string            `json:"apiVersion"`
	Kind               string            `json:"kind"`
	EventID            string            `json:"eventID"`
	Partition          string            `json:"partition"`
	Intent             string            `json:"intent,omitempty"`
	Type               string            `json:"type"`
	Message            string            `json:"message"`
	TaskID             string            `json:"taskID,omitempty"`
	DeploymentRevision string            `json:"deploymentRevision,omitempty"`
	Pusher             string            `json:"pusher,omitempty"`
	CorrelationID      string            `json:"correlationID,omitempty"`
	CreatedAt          time.Time         `json:"createdAt"`
	Details            map[string]string `json:"details,omitempty"`
}
