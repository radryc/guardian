package ui

import (
	"testing"
	"time"

	historydomain "github.com/rydzu/ainfra/guardian/internal/domain/history"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
)

func TestClassifyEvent(t *testing.T) {
	cases := []struct {
		name    string
		event   historydomain.EventRecord
		wantStatus string
		wantDisplay string
	}{
		{
			name: "deploy.completed is healthy Pushed",
			event: historydomain.EventRecord{Type: "deploy.completed"},
			wantStatus: "healthy",
			wantDisplay: "Pushed",
		},
		{
			name: "task.failed is failing Failed",
			event: historydomain.EventRecord{Type: "task.failed"},
			wantStatus: "failing",
			wantDisplay: "Failed",
		},
		{
			name: "partition.pushed new partition is pending Started",
			event: historydomain.EventRecord{Type: "partition.pushed", Details: map[string]string{"is_new": "true"}},
			wantStatus: "pending",
			wantDisplay: "Started",
		},
		{
			name: "partition.pushed update is neutral Updated",
			event: historydomain.EventRecord{Type: "partition.pushed", Details: map[string]string{"is_new": "false"}},
			wantStatus: "neutral",
			wantDisplay: "Updated",
		},
		{
			name: "partition.status.updated healthy is healthy Ready",
			event: historydomain.EventRecord{Type: "partition.status.updated", Details: map[string]string{"status": "Healthy", "displayStatus": "Stable"}},
			wantStatus: "healthy",
			wantDisplay: "Ready",
		},
		{
			name: "partition.status.updated progressing is pending Progressing",
			event: historydomain.EventRecord{Type: "partition.status.updated", Details: map[string]string{"status": "Progressing", "displayStatus": "Progressing"}},
			wantStatus: "pending",
			wantDisplay: "Progressing",
		},
		{
			name: "partition.status.updated failing uses display status",
			event: historydomain.EventRecord{Type: "partition.status.updated", Details: map[string]string{"status": "Failing", "displayStatus": "Needs action"}},
			wantStatus: "failing",
			wantDisplay: "Needs action",
		},
		{
			name: "rollback.triggered is attention Rollback",
			event: historydomain.EventRecord{Type: "rollback.triggered"},
			wantStatus: "attention",
			wantDisplay: "Rollback",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStatus, gotDisplay := classifyEvent(tc.event)
			if gotStatus != tc.wantStatus {
				t.Fatalf("status = %q, want %q", gotStatus, tc.wantStatus)
			}
			if gotDisplay != tc.wantDisplay {
				t.Fatalf("display = %q, want %q", gotDisplay, tc.wantDisplay)
			}
		})
	}
}

func TestDeploymentStatusFromAssets(t *testing.T) {
	cases := []struct {
		name        string
		assets      []AssetDeployment
		wantStatus  string
		wantDisplay string
	}{
		{
			name:        "empty assets are ready",
			assets:      nil,
			wantStatus:  "healthy",
			wantDisplay: "Ready",
		},
		{
			name:        "all healthy assets are ready",
			assets:      []AssetDeployment{{Status: "healthy"}, {Status: "healthy"}},
			wantStatus:  "healthy",
			wantDisplay: "Ready",
		},
		{
			name:        "failing asset takes precedence",
			assets:      []AssetDeployment{{Status: "healthy"}, {Status: "failing"}, {Status: "attention"}},
			wantStatus:  "failing",
			wantDisplay: "Error",
		},
		{
			name:        "attention asset degrades",
			assets:      []AssetDeployment{{Status: "healthy"}, {Status: "attention"}},
			wantStatus:  "attention",
			wantDisplay: "Degraded",
		},
		{
			name:        "pending asset is progressing",
			assets:      []AssetDeployment{{Status: "healthy"}, {Status: "pending"}},
			wantStatus:  "pending",
			wantDisplay: "Progressing",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStatus, gotDisplay := deploymentStatusFromAssets(tc.assets)
			if gotStatus != tc.wantStatus {
				t.Fatalf("status = %q, want %q", gotStatus, tc.wantStatus)
			}
			if gotDisplay != tc.wantDisplay {
				t.Fatalf("display = %q, want %q", gotDisplay, tc.wantDisplay)
			}
		})
	}
}

func TestBuildDeploymentViewDerivesStatus(t *testing.T) {
	record := historydomain.DeploymentRecord{
		DeploymentRevision: "dep-1",
		Partition:          "demo",
		Intent:             "web",
		AssetVersionIDs:    map[string]string{"app": "v1"},
		AssetVersions:      map[string]string{"app": "v1"},
		CreatedAt:          time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
	}
	logs := []taskdomain.LogEntry{
		{Asset: "app", Level: "error", Message: "apply failed"},
	}
	view := buildDeploymentView(record, logs)
	if view.Status != "failing" {
		t.Fatalf("deployment status = %q, want failing", view.Status)
	}
	if view.DisplayStatus != "Error" {
		t.Fatalf("deployment display status = %q, want Error", view.DisplayStatus)
	}
}
