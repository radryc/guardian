package dispatcher

import "github.com/radryc/guardian/internal/metrics"

var (
	// guardianWritesTotal counts every logical file write issued by the dispatcher.
	// The "operation" label identifies the write context:
	//   queue_task, write_intent_state, write_partition_state,
	//   write_partition_runtime, archive_deployment, write_event,
	//   delete_intent_state, delete_partition_runtime, cleanup_task
	guardianWritesTotal = metrics.NewCounter(
		"guardian_dispatcher_writes_total",
		"Total logical file writes issued by the Guardian dispatcher, by operation.",
		"operation",
	)

	// guardianWriteBytesTotal counts raw bytes sent to the store by the dispatcher.
	guardianWriteBytesTotal = metrics.NewCounter(
		"guardian_dispatcher_write_bytes_total",
		"Total bytes written to the store by the Guardian dispatcher, by operation.",
		"operation",
	)

	// guardianSkippedWritesTotal counts writes suppressed by the content-hash guard.
	guardianSkippedWritesTotal = metrics.NewCounter(
		"guardian_dispatcher_skipped_writes_total",
		"Total writes skipped by the Guardian dispatcher because content was identical to the last write.",
		"operation",
	)

	// guardianDeletesTotal counts logical file deletes issued by the dispatcher.
	guardianDeletesTotal = metrics.NewCounter(
		"guardian_dispatcher_deletes_total",
		"Total logical file deletes issued by the Guardian dispatcher, by operation.",
		"operation",
	)

	// guardianPartitionStatusCurrent tracks the latest derived partition status distribution.
	guardianPartitionStatusCurrent = metrics.NewGauge(
		"guardian_dispatcher_partition_status_current",
		"Current number of partitions in each derived status.",
		"status",
	)

	// guardianIntentStatusCurrent tracks the latest derived intent status distribution.
	guardianIntentStatusCurrent = metrics.NewGauge(
		"guardian_dispatcher_intent_status_current",
		"Current number of desired intents in each status across partition runtime snapshots.",
		"status",
	)

	guardianCASConflictsTotal = metrics.NewCounter(
		"guardian_dispatcher_cas_conflicts_total",
		"Compare-and-swap conflicts observed by the Guardian dispatcher.",
		"operation",
	)

	guardianCASRetriesTotal = metrics.NewCounter(
		"guardian_dispatcher_cas_retries_total",
		"Compare-and-swap retries performed by the Guardian dispatcher.",
		"operation",
	)

	guardianCASExhaustedTotal = metrics.NewCounter(
		"guardian_dispatcher_cas_exhausted_total",
		"Compare-and-swap retry budgets exhausted by the Guardian dispatcher.",
		"operation",
	)

	guardianCASVersionMissingTotal = metrics.NewCounter(
		"guardian_dispatcher_cas_version_missing_total",
		"Intent state writes where content existed but no authoritative version was available.",
	)
)
