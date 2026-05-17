package common

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	partitionRuntimeLoadsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "guardian",
		Subsystem: "runtime",
		Name:      "loads_total",
		Help:      "Total partition runtime loads by source.",
	}, []string{"source"})

	partitionRuntimeIntentStatesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "guardian",
		Subsystem: "runtime",
		Name:      "intent_states_loaded_total",
		Help:      "Total intent states materialized from partition runtime loads by source.",
	}, []string{"source"})
)
