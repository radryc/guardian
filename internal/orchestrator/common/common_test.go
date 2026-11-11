package common

import (
	"context"
	"testing"

	"github.com/radryc/guardian/internal/store/memory"

	statedomain "github.com/radryc/guardian/internal/domain/state"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
)

func TestQueuedStatusTransitionsOutOfFailureStates(t *testing.T) {
	tests := []struct {
		name    string
		current statedomain.IntentStatus
		op      taskdomain.Operation
		want    statedomain.IntentStatus
	}{
		{
			name:    "apply failed requeues as checking",
			current: statedomain.StatusApplyFailed,
			op:      taskdomain.OpCheck,
			want:    statedomain.StatusChecking,
		},
		{
			name:    "check failed requeues as checking",
			current: statedomain.StatusCheckFailed,
			op:      taskdomain.OpCheck,
			want:    statedomain.StatusChecking,
		},
		{
			name:    "diff failed requeues as diffing",
			current: statedomain.StatusDiffFailed,
			op:      taskdomain.OpDiff,
			want:    statedomain.StatusDiffing,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := QueuedStatus(tt.current, tt.op); got != tt.want {
				t.Fatalf("QueuedStatus(%q, %q) = %q, want %q", tt.current, tt.op, got, tt.want)
			}
		})
	}
}

func TestHasActiveTaskWithMissingTargetPusherIsInactive(t *testing.T) {
	state := &statedomain.IntentState{
		Status:     statedomain.StatusApplying,
		LastTaskID: "task_1",
	}
	active, err := HasActiveTask(context.Background(), memory.New(), state)
	if err != nil {
		t.Fatalf("HasActiveTask() error = %v", err)
	}
	if active {
		t.Fatal("expected a state without targetPusher to be treated as inactive")
	}
}
