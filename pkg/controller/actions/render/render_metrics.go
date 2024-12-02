package render

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// RenderedResourcesTotal is a prometheus counter metrics which holds the total
	// number of resource rendered by the action per controller and rendering type.
	// It has two labels.
	// controller label refers to the controller name.
	// engine label refers to the rendering engine.
	RenderedResourcesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "action_renderer_manifests_total",
			Help: "Number of rendered resources",
		},
		[]string{
			"controller",
			"engine",
		},
	)
)

// init register metrics to the global registry from controller-runtime/pkg/metrics.
// see https://book.kubebuilder.io/reference/metrics#publishing-additional-metrics
//
//nolint:gochecknoinits
func init() {
	metrics.Registry.MustRegister(RenderedResourcesTotal)
}
