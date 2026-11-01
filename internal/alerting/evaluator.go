// Package alerting evaluates partition health and notifies configured channels
// when a partition starts failing or recovers.
package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"time"

	historydomain "github.com/radryc/guardian/internal/domain/history"
	"github.com/radryc/guardian/internal/notify"
	"github.com/radryc/guardian/internal/orchestrator/common"
	"github.com/radryc/guardian/internal/orchestrator/dispatcher"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/pkg/guardianapi"
)

const (
	defaultPollInterval   = 30 * time.Second
	defaultFailThreshold  = 2
	defaultCooldown       = 15 * time.Minute
	defaultRepeatInterval = 6 * time.Hour

	eventPartitionFailing   = "partition.failing"
	eventPartitionRecovered = "partition.recovered"
)

// Rule selects which alert events a channel receives.
type Rule struct {
	Name        string
	Events      []string
	MinSeverity notify.Severity
	Channels    []string
}

// Evaluator watches partition status and emits failure/recovery notifications.
type Evaluator struct {
	Store          guardianapi.Store
	Dispatcher     *dispatcher.Dispatcher
	Notifiers      map[string]notify.Notifier
	Channels       []string
	Rules          []Rule
	PollInterval   time.Duration
	FailThreshold  int
	Cooldown       time.Duration
	RepeatInterval time.Duration
}

type notificationState struct {
	APIVersion         string          `json:"apiVersion"`
	Kind               string          `json:"kind"`
	Partition          string          `json:"partition"`
	LastStatus         string          `json:"lastStatus"`
	ConsecutiveFailing int             `json:"consecutiveFailing"`
	ConsecutiveHealthy int             `json:"consecutiveHealthy"`
	ActiveEvent        string          `json:"activeEvent,omitempty"`
	ActiveSeverity     notify.Severity `json:"activeSeverity,omitempty"`
	ActiveSummary      string          `json:"activeSummary,omitempty"`
	FirstSeenAt        time.Time       `json:"firstSeenAt,omitempty"`
	LastNotifiedAt     time.Time       `json:"lastNotifiedAt,omitempty"`
	UpdatedAt          time.Time       `json:"updatedAt"`
	// Channels tracks the last successfully delivered idempotency key per
	// channel. It prevents partial deliveries from being resent when another
	// channel fails and the event is retried.
	Channels map[string]channelDelivery `json:"channels,omitempty"`
}

type channelDelivery struct {
	LastKey       string    `json:"lastKey,omitempty"`
	LastSuccessAt time.Time `json:"lastSuccessAt,omitempty"`
	Failures      int       `json:"failures,omitempty"`
	LastError     string    `json:"lastError,omitempty"`
}

// Run evaluates partitions until the context is cancelled.
func (e *Evaluator) Run(ctx context.Context) error {
	if e.Store == nil || e.Dispatcher == nil {
		return fmt.Errorf("alerting evaluator requires a store and dispatcher")
	}
	interval := e.PollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	if err := e.EvaluateOnce(ctx); err != nil && ctx.Err() == nil {
		log.Printf("[alerting] initial evaluation failed: %v", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := e.EvaluateOnce(ctx); err != nil && ctx.Err() == nil {
				log.Printf("[alerting] evaluation failed (will retry in %s): %v", interval, err)
			}
		}
	}
}

// EvaluateOnce classifies every partition and emits notifications on
// transitions.
func (e *Evaluator) EvaluateOnce(ctx context.Context) error {
	entries, err := e.Store.ListDir(ctx, paths.PartitionsRoot())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		if err := e.evaluatePartition(ctx, entry.Name); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("[alerting] partition %s: %v", entry.Name, err)
		}
	}
	return nil
}

func (e *Evaluator) evaluatePartition(ctx context.Context, partition string) error {
	partState, err := common.LoadPartitionState(ctx, e.Store, partition)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	state, version, err := e.loadState(ctx, partition)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	severity, alerting := classify(partState.Status)

	if alerting {
		state.ConsecutiveFailing++
		state.ConsecutiveHealthy = 0
		if state.ActiveEvent == "" && state.ConsecutiveFailing >= e.failThreshold() {
			if e.cooldownActive(state, now) {
				// Suppress, but keep tracking.
			} else {
				if state.FirstSeenAt.IsZero() {
					state.FirstSeenAt = now
				}
				alert := notify.Alert{
					Event:     eventPartitionFailing,
					Partition: partition,
					Status:    "firing",
					Severity:  severity,
					Summary:   partState.Summary,
					Details: map[string]string{
						"status":           partState.Status,
						"failingIntents":   fmt.Sprintf("%d", partState.Metrics.FailingIntents),
						"attentionIntents": fmt.Sprintf("%d", partState.Metrics.AttentionIntents),
					},
					FiredAt: now,
				}
				alert.IdempotencyKey = e.deliveryKey(partition, alert, state)
				sent, attempted := e.dispatch(ctx, partition, alert, state)
				if attempted == 0 || sent {
					state.ActiveEvent = alert.Event
					state.ActiveSeverity = severity
					state.ActiveSummary = alert.Summary
					state.LastNotifiedAt = now
				} else {
					// Keep the event unarmed so the next evaluation retries
					// only the channels that have not delivered yet.
					log.Printf("[alerting] %s: delivery incomplete; will retry on next evaluation", partition)
				}
			}
		} else if state.ActiveEvent != "" && e.repeatDue(state, now) {
			alert := notify.Alert{
				Event:     state.ActiveEvent,
				Partition: partition,
				Status:    "firing",
				Severity:  state.ActiveSeverity,
				Summary:   partState.Summary,
				Details: map[string]string{
					"status":    partState.Status,
					"firstSeen": state.FirstSeenAt.Format(time.RFC3339),
				},
				FiredAt: now,
			}
			alert.IdempotencyKey = e.deliveryKey(partition, alert, state)
			sent, attempted := e.dispatch(ctx, partition, alert, state)
			if attempted == 0 || sent {
				state.LastNotifiedAt = now
			}
		}
	} else {
		state.ConsecutiveHealthy++
		state.ConsecutiveFailing = 0
		if state.ActiveEvent != "" && state.ConsecutiveHealthy >= e.failThreshold() {
			resolvedAt := now
			alert := notify.Alert{
				Event:      eventPartitionRecovered,
				Partition:  partition,
				Status:     "resolved",
				Severity:   state.ActiveSeverity,
				Summary:    "partition recovered: " + partState.Summary,
				FiredAt:    state.FirstSeenAt,
				ResolvedAt: &resolvedAt,
				Details: map[string]string{
					"status":        partState.Status,
					"resolvedEvent": state.ActiveEvent,
				},
			}
			alert.IdempotencyKey = e.deliveryKey(partition, alert, state)
			sent, attempted := e.dispatch(ctx, partition, alert, state)
			if attempted == 0 || sent {
				state.ActiveEvent = ""
				state.ActiveSeverity = ""
				state.ActiveSummary = ""
				state.FirstSeenAt = time.Time{}
				state.LastNotifiedAt = now
			} else {
				log.Printf("[alerting] %s: recovery delivery incomplete; will retry on next evaluation", partition)
			}
		}
	}
	state.LastStatus = partState.Status
	state.UpdatedAt = now
	return e.saveState(ctx, partition, state, version)
}

func (e *Evaluator) dispatch(ctx context.Context, partition string, alert notify.Alert, state *notificationState) (sent bool, attempted int) {
	if state.Channels == nil {
		state.Channels = map[string]channelDelivery{}
	}
	allDelivered := true
	for _, channel := range e.channelsFor(alert) {
		notifier, ok := e.Notifiers[channel]
		if !ok {
			continue
		}
		attempted++
		delivery := state.Channels[channel]
		if alert.IdempotencyKey != "" && delivery.LastKey == alert.IdempotencyKey {
			// A previous partial delivery already reached this channel.
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := sendWithRetry(sendCtx, notifier, alert)
		cancel()
		if err != nil {
			delivery.Failures++
			delivery.LastError = err.Error()
			state.Channels[channel] = delivery
			allDelivered = false
			log.Printf("[alerting] %s channel %s failed: %v", partition, channel, err)
			_ = e.Dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
				Partition: partition,
				Type:      "notification.failed",
				Message:   fmt.Sprintf("channel %s failed: %v", channel, err),
				Details:   map[string]string{"channel": channel, "event": alert.Event, "error": err.Error()},
			})
			continue
		}
		delivery.LastKey = alert.IdempotencyKey
		delivery.LastSuccessAt = time.Now().UTC()
		delivery.Failures = 0
		delivery.LastError = ""
		state.Channels[channel] = delivery
		_ = e.Dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
			Partition: partition,
			Type:      "notification.sent",
			Message:   fmt.Sprintf("%s alert delivered via %s", alert.Event, channel),
			Details:   map[string]string{"channel": channel, "event": alert.Event, "status": alert.Status},
		})
	}
	_ = e.Dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
		Partition: partition,
		Type:      alert.Event,
		Message:   alert.Summary,
		Details: map[string]string{
			"severity": string(alert.Severity),
			"status":   alert.Status,
		},
	})
	return allDelivered, attempted
}

func sendWithRetry(ctx context.Context, notifier notify.Notifier, alert notify.Alert) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 100 * time.Millisecond):
			}
		}
		if err := notifier.Send(ctx, alert); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("notification failed")
	}
	return lastErr
}

func (e *Evaluator) deliveryKey(partition string, alert notify.Alert, state *notificationState) string {
	if alert.IdempotencyKey != "" {
		return alert.IdempotencyKey
	}
	base := state.FirstSeenAt
	if alert.Event == eventPartitionFailing && !state.LastNotifiedAt.IsZero() {
		// A repeat notification is a new occurrence after the previous delivery.
		base = state.LastNotifiedAt
	}
	if alert.Event == eventPartitionRecovered {
		base = state.FirstSeenAt
	}
	if base.IsZero() {
		base = alert.FiredAt
	}
	return fmt.Sprintf("%s|%s|%s|%s", partition, alert.Event, alert.Status, base.UTC().Format(time.RFC3339Nano))
}

func (e *Evaluator) channelsFor(alert notify.Alert) []string {
	if len(e.Rules) == 0 {
		return e.Channels
	}
	selected := map[string]struct{}{}
	for _, rule := range e.Rules {
		if !ruleMatches(rule, alert) {
			continue
		}
		for _, channel := range rule.Channels {
			selected[channel] = struct{}{}
		}
	}
	out := make([]string, 0, len(selected))
	for channel := range selected {
		out = append(out, channel)
	}
	return out
}

func ruleMatches(rule Rule, alert notify.Alert) bool {
	if len(rule.Events) > 0 {
		matched := false
		for _, event := range rule.Events {
			if event == alert.Event {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if rule.MinSeverity != "" && notify.Rank(alert.Severity) < notify.Rank(rule.MinSeverity) {
		return false
	}
	return true
}

func classify(status string) (notify.Severity, bool) {
	switch status {
	case "Failing", "Invalid":
		return notify.SeverityCritical, true
	case "Attention":
		return notify.SeverityWarning, true
	default:
		return notify.SeverityInfo, false
	}
}

func (e *Evaluator) failThreshold() int {
	if e.FailThreshold > 0 {
		return e.FailThreshold
	}
	return defaultFailThreshold
}

func (e *Evaluator) cooldownActive(state *notificationState, now time.Time) bool {
	cooldown := e.Cooldown
	if cooldown <= 0 {
		cooldown = defaultCooldown
	}
	return !state.LastNotifiedAt.IsZero() && now.Sub(state.LastNotifiedAt) < cooldown
}

func (e *Evaluator) repeatDue(state *notificationState, now time.Time) bool {
	interval := e.RepeatInterval
	if interval <= 0 {
		interval = defaultRepeatInterval
	}
	return !state.LastNotifiedAt.IsZero() && now.Sub(state.LastNotifiedAt) >= interval
}

func (e *Evaluator) statePath(partition string) string {
	return path.Join(paths.StateRoot(partition), "notifications.json")
}

func (e *Evaluator) loadState(ctx context.Context, partition string) (*notificationState, string, error) {
	var state notificationState
	err := common.ReadJSON(ctx, e.Store, e.statePath(partition), &state)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &notificationState{
				APIVersion: "guardian/v1alpha1",
				Kind:       "NotificationState",
				Partition:  partition,
			}, "absent", nil
		}
		return nil, "", err
	}
	info, statErr := e.Store.Stat(ctx, e.statePath(partition))
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, "", statErr
	}
	state.Partition = partition
	return &state, info.VersionID, nil
}

func (e *Evaluator) saveState(ctx context.Context, partition string, state *notificationState, expectedVersion string) error {
	state.Partition = partition
	state.APIVersion = "guardian/v1alpha1"
	state.Kind = "NotificationState"
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if expectedVersion == "" {
		expectedVersion = "absent"
	}
	_, err = e.Store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath:       e.statePath(partition),
			Content:           payload,
			ExpectedVersionID: expectedVersion,
		}},
		Context: guardianapi.MutationContext{
			PrincipalID: "guardiand",
			Reason:      "partition notification state",
		},
	})
	return err
}
