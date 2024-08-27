package modelregistry

import (
	"github.com/opendatahub-io/odh-platform/pkg/platform"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (m *ModelRegistry) platformRegister() {
	if m.platform.Routing().IsAvailable() {
		m.platform.Routing().Expose(platform.RoutingTarget{ObjectReference: watchedCR})
		// -> CR not representing a solid abstraction yet, desired long-term
		// -> applying overlay/CR shipped as manifests
		// -> that would result in deploying platform-ctrl + IGW
		// Expose(odhp.RoutingTarget{ObjectReference: watchedCR})
	}

	if m.platform.Authorization().IsAvailable() {
		m.platform.Authorization().ProtectedResources(m.ProtectedResources()...)
	}
}

func (m *ModelRegistry) ProtectedResources() []platform.ProtectedResource {
	return []platform.ProtectedResource{
		{
			ObjectReference: watchedCR,
			WorkloadSelector: map[string]string{
				"component": "model-registry",
			},
			HostPaths: []string{"status.hosts"},
			Ports:     []string{"8080", "9090"},
		},
	}
}

// platform target resource.
var watchedCR = platform.ObjectReference{
	GroupVersionKind: schema.GroupVersionKind{
		Group:   "modelregistry.opendatahub.io",
		Version: "v1alpha1",
		Kind:    "ModelRegistry",
	},
	Resources: "modelregistries",
}
