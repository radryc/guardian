package common

import "github.com/radryc/guardian/internal/metrics"

var (
	partitionRuntimeLoadsTotal = metrics.NewCounter(
		"guardian_runtime_partition_loads_total",
		"Total partition runtime loads by source.",
		"source",
	)

	partitionRuntimeIntentStatesTotal = metrics.NewCounter(
		"guardian_runtime_partition_intent_states_loaded_total",
		"Total intent states materialized from partition runtime loads by source.",
		"source",
	)
)
