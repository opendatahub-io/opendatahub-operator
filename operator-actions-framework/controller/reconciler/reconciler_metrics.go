package reconciler

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// DynamicWatchResourcesTotal is a prometheus counter metrics which holds the total
	// number of dynamically watched resource per controller.
	// It has one labels.
	// controller label refers to the controller name.
	DynamicWatchResourcesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "action_dynamic_watch_total",
			Help: "Number of dynamically watched resources",
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
	metrics.Registry.MustRegister(DynamicWatchResourcesTotal)
}
