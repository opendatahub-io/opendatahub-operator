package deploy

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// DeployedResourcesTotal is a prometheus counter metrics which holds the total
	// number of resource deployed by the action per controller. It has one label.
	// controller label refers  to the controller name.
	DeployedResourcesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "action_deploy_resources_total",
			Help: "Number of deployed resources",
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
	metrics.Registry.MustRegister(DeployedResourcesTotal)
}
