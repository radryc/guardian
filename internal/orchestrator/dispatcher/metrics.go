package dispatcher

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// guardianWritesTotal counts every logical file write issued by the dispatcher.
	// The "operation" label identifies the write context:
	//   queue_task, write_intent_state, write_partition_state,
	//   write_partition_runtime, archive_deployment, write_event,
	//   delete_intent_state, delete_partition_runtime, cleanup_task
	guardianWritesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "guardian",
		Subsystem: "dispatcher",
		Name:      "writes_total",
		Help:      "Total logical file writes issued by the Guardian dispatcher, by operation.",
	}, []string{"operation"})

	// guardianWriteBytesTotal counts raw bytes sent to the store by the dispatcher.
	guardianWriteBytesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "guardian",
		Subsystem: "dispatcher",
		Name:      "write_bytes_total",
		Help:      "Total bytes written to the store by the Guardian dispatcher, by operation.",
	}, []string{"operation"})

	// guardianSkippedWritesTotal counts writes suppressed by the content-hash guard.
	guardianSkippedWritesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "guardian",
		Subsystem: "dispatcher",
		Name:      "skipped_writes_total",
		Help:      "Total writes skipped by the Guardian dispatcher because content was identical to the last write.",
	}, []string{"operation"})

	// guardianDeletesTotal counts logical file deletes issued by the dispatcher.
	guardianDeletesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "guardian",
		Subsystem: "dispatcher",
		Name:      "deletes_total",
		Help:      "Total logical file deletes issued by the Guardian dispatcher, by operation.",
	}, []string{"operation"})

	// guardianPartitionStatusCurrent tracks the latest derived partition status distribution.
	guardianPartitionStatusCurrent = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "guardian",
		Subsystem: "dispatcher",
		Name:      "partition_status_current",
		Help:      "Current number of partitions in each derived status.",
	}, []string{"status"})

	// guardianIntentStatusCurrent tracks the latest derived intent status distribution.
	guardianIntentStatusCurrent = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "guardian",
		Subsystem: "dispatcher",
		Name:      "intent_status_current",
		Help:      "Current number of desired intents in each status across partition runtime snapshots.",
	}, []string{"status"})
)
