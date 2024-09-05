package modelregistry

import (
	"github.com/opendatahub-io/odh-platform/pkg/platform"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (m *ModelRegistry) platformRegister() {
	if m.platform.Routing().IsAvailable() {
		m.platform.Routing().Expose(m.RoutingResources()...)
	}

	if m.platform.Authorization().IsAvailable() {
		m.platform.Authorization().ProtectedResources(m.ProtectedResources()...)
	}
}

func (m *ModelRegistry) RoutingResources() []platform.RoutingTarget {
	return []platform.RoutingTarget{
		{
			ResourceReference: watchedCR,
			ServiceSelector: map[string]string{
				"app.kubernetes.io/component": "model-registry",
				"app.kubernetes.io/instance":  "{{.metadata.name}}",
			},
		},
	}
}

func (m *ModelRegistry) ProtectedResources() []platform.ProtectedResource {
	return []platform.ProtectedResource{
		{
			ResourceReference: watchedCR,
			WorkloadSelector: map[string]string{
				"app":       "{{.metadata.name}}",
				"component": "model-registry",
			},
			HostPaths: []string{"status.hosts"},
			Ports:     []string{"8080", "9090"},
		},
	}
}

// platform target resource.
var watchedCR = platform.ResourceReference{
	GroupVersionKind: schema.GroupVersionKind{
		Group:   "modelregistry.opendatahub.io",
		Version: "v1alpha1",
		Kind:    "ModelRegistry",
	},
	Resources: "modelregistries",
}
