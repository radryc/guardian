package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	statedomain "github.com/radryc/guardian/internal/domain/state"
	"github.com/radryc/guardian/internal/notify"
	"github.com/radryc/guardian/internal/orchestrator/dispatcher"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/store/memory"
	"github.com/radryc/guardian/pkg/guardianapi"
)

type recordingNotifier struct {
	mu     sync.Mutex
	alerts []notify.Alert
}

func (r *recordingNotifier) Name() string { return "recording" }

func (r *recordingNotifier) Send(_ context.Context, alert notify.Alert) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.alerts = append(r.alerts, alert)
	return nil
}

func (r *recordingNotifier) snapshot() []notify.Alert {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]notify.Alert(nil), r.alerts...)
}

func seedIntentStatus(t *testing.T, ctx context.Context, store guardianapi.Store, status statedomain.IntentStatus) {
	t.Helper()
	state := statedomain.IntentState{
		APIVersion: "guardian/v1alpha1",
		Kind:       "IntentState",
		Partition:  "payments",
		Intent:     "api",
		Status:     status,
		Outputs:    map[string]string{},
	}
	payload, _ := json.Marshal(state)
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{LogicalPath: paths.IntentState("payments", "api"), Content: payload}},
	}); err != nil {
		t.Fatalf("seed intent state: %v", err)
	}
}

func newTestEvaluator(store guardianapi.Store, notifier notify.Notifier) *Evaluator {
	return &Evaluator{
		Store:          store,
		Dispatcher:     dispatcher.NewDispatcher(store, "guardiand"),
		Notifiers:      map[string]notify.Notifier{"recording": notifier},
		Channels:       []string{"recording"},
		FailThreshold:  2,
		Cooldown:       0,
		RepeatInterval: time.Hour,
	}
}

func TestEvaluatorFiresOnceOnFailingAndRecovers(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	notifier := &recordingNotifier{}
	evaluator := newTestEvaluator(store, notifier)
	seedIntentStatus(t, ctx, store, statedomain.StatusApplyFailed)

	// First pass arms the threshold, second fires.
	if err := evaluator.EvaluateOnce(ctx); err != nil {
		t.Fatalf("EvaluateOnce #1: %v", err)
	}
	if got := len(notifier.snapshot()); got != 0 {
		t.Fatalf("alerts after first pass = %d, want 0", got)
	}
	if err := evaluator.EvaluateOnce(ctx); err != nil {
		t.Fatalf("EvaluateOnce #2: %v", err)
	}
	alerts := notifier.snapshot()
	if len(alerts) != 1 {
		t.Fatalf("alerts after second pass = %d, want 1", len(alerts))
	}
	if alerts[0].Event != eventPartitionFailing || alerts[0].Status != "firing" {
		t.Fatalf("alert = %+v", alerts[0])
	}

	// Further failing passes must not duplicate (repeat interval not elapsed).
	if err := evaluator.EvaluateOnce(ctx); err != nil {
		t.Fatalf("EvaluateOnce #3: %v", err)
	}
	if got := len(notifier.snapshot()); got != 1 {
		t.Fatalf("alerts after third pass = %d, want 1", got)
	}

	// Recovery needs the same threshold.
	seedIntentStatus(t, ctx, store, statedomain.StatusHealthy)
	if err := evaluator.EvaluateOnce(ctx); err != nil {
		t.Fatalf("EvaluateOnce #4: %v", err)
	}
	if err := evaluator.EvaluateOnce(ctx); err != nil {
		t.Fatalf("EvaluateOnce #5: %v", err)
	}
	alerts = notifier.snapshot()
	if len(alerts) != 2 {
		t.Fatalf("alerts after recovery = %d, want 2", len(alerts))
	}
	if alerts[1].Event != eventPartitionRecovered || alerts[1].Status != "resolved" {
		t.Fatalf("recovery alert = %+v", alerts[1])
	}
}

type failingNotifier struct {
	mu       sync.Mutex
	failures int
	attempts int
	alerts   []notify.Alert
}

func (f *failingNotifier) Name() string { return "failing" }

func (f *failingNotifier) Send(_ context.Context, alert notify.Alert) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if f.failures > 0 {
		f.failures--
		return errTestDelivery
	}
	f.alerts = append(f.alerts, alert)
	return nil
}

func (f *failingNotifier) counts() (attempts int, delivered int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts, len(f.alerts)
}

var errTestDelivery = &testDeliveryError{}

type testDeliveryError struct{}

func (*testDeliveryError) Error() string { return "delivery failed" }

func TestEvaluatorRetriesOnlyFailedChannels(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	success := &recordingNotifier{}
	failing := &failingNotifier{failures: 3}
	evaluator := &Evaluator{
		Store:      store,
		Dispatcher: dispatcher.NewDispatcher(store, "guardiand"),
		Notifiers: map[string]notify.Notifier{
			"success": success,
			"failing": failing,
		},
		Channels:       []string{"success", "failing"},
		FailThreshold:  2,
		Cooldown:       0,
		RepeatInterval: time.Hour,
	}
	seedIntentStatus(t, ctx, store, statedomain.StatusApplyFailed)

	for i := 0; i < 3; i++ {
		if err := evaluator.EvaluateOnce(ctx); err != nil {
			t.Fatalf("EvaluateOnce #%d: %v", i+1, err)
		}
	}
	if got := len(success.snapshot()); got != 1 {
		t.Fatalf("successful channel deliveries = %d, want 1", got)
	}
	attempts, delivered := failing.counts()
	if delivered != 1 {
		t.Fatalf("failing channel deliveries = %d, want 1 (attempts=%d)", delivered, attempts)
	}
	if attempts != 4 {
		t.Fatalf("failing channel attempts = %d, want 4 (three failures, one success)", attempts)
	}
}

func TestEvaluatorDispatchIsIdempotentPerChannel(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	notifier := &recordingNotifier{}
	evaluator := newTestEvaluator(store, notifier)
	state := &notificationState{Partition: "payments"}
	alert := notify.Alert{
		Event:          eventPartitionFailing,
		Partition:      "payments",
		Status:         "firing",
		Severity:       notify.SeverityCritical,
		FiredAt:        time.Now().UTC(),
		IdempotencyKey: "payments|failing|once",
	}
	if sent, _ := evaluator.dispatch(ctx, "payments", alert, state); !sent {
		t.Fatal("first dispatch did not deliver")
	}
	if sent, _ := evaluator.dispatch(ctx, "payments", alert, state); !sent {
		t.Fatal("second dispatch should report already delivered")
	}
	if got := len(notifier.snapshot()); got != 1 {
		t.Fatalf("deliveries = %d, want 1", got)
	}
}

func TestEvaluatorStateWriteUsesCAS(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	evaluator := newTestEvaluator(store, &recordingNotifier{})
	state := &notificationState{Partition: "payments", LastStatus: "Failing"}
	if err := evaluator.saveState(ctx, "payments", state, "absent"); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := evaluator.saveState(ctx, "payments", state, "absent"); err == nil {
		t.Fatal("second save with stale version succeeded, want conflict")
	}
}

func TestEvaluatorRulesFilterChannels(t *testing.T) {
	evaluator := &Evaluator{
		Channels: []string{"a", "b"},
		Rules: []Rule{{
			Name:     "critical-only",
			Events:   []string{eventPartitionFailing},
			Channels: []string{"b"},
		}},
	}
	channels := evaluator.channelsFor(notify.Alert{Event: eventPartitionFailing, Severity: notify.SeverityCritical})
	if len(channels) != 1 || channels[0] != "b" {
		t.Fatalf("channels = %v, want [b]", channels)
	}
	if got := evaluator.channelsFor(notify.Alert{Event: eventPartitionRecovered, Severity: notify.SeverityCritical}); len(got) != 0 {
		t.Fatalf("recovery channels = %v, want none", got)
	}
}

func TestWebhookDeliveryEndToEnd(t *testing.T) {
	var delivered int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx := context.Background()
	store := memory.New()
	evaluator := &Evaluator{
		Store:         store,
		Dispatcher:    dispatcher.NewDispatcher(store, "guardiand"),
		Notifiers:     map[string]notify.Notifier{"webhook": notify.NewWebhookNotifier("webhook", server.URL, "", time.Second)},
		Channels:      []string{"webhook"},
		FailThreshold: 1,
		Cooldown:      0,
	}
	seedIntentStatus(t, ctx, store, statedomain.StatusDiffFailed)
	if err := evaluator.EvaluateOnce(ctx); err != nil {
		t.Fatalf("EvaluateOnce: %v", err)
	}
	if delivered != 1 {
		t.Fatalf("delivered = %d, want 1", delivered)
	}
}
