package gc

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// DeletedTotal is a prometheus counter metrics which holds the total number
	// of resource deleted by the action per controller. It has one label.
	// controller label refers  to the controller name.
	DeletedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "action_gc_deleted_total",
			Help: "Number of GCed resources",
		},
		[]string{
			"controller",
		},
	)

	// CyclesTotal is a prometheus counter metrics which holds the total number
	// gc cycles per controller. It has one label.
	// controller label refers  to the controller name.
	CyclesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "action_gc_cycles_total",
			Help: "Number of GC cycles",
		},
		[]string{
			"controller",
		},
	)
)

// init register metrics to the global registry from controller-runtime/pkg/metrics.
// see https://book.kubebuilder.io/reference/metrics#publishing-additional-metrics
//
//nolint:gochecknoinits
func init() {
	metrics.Registry.MustRegister(DeletedTotal)
	metrics.Registry.MustRegister(CyclesTotal)
}
