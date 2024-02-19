package cluster

import "k8s.io/apimachinery/pkg/runtime/schema"

var (
	KnativeServingGVK = schema.GroupVersionKind{
		Group:   "operator.knative.dev",
		Version: "v1beta1",
		Kind:    "KnativeServing",
	}

	OpenshiftIngressGVK = schema.GroupVersionKind{
		Group:   "config.openshift.io",
		Version: "v1",
		Kind:    "Ingress",
	}

	ServiceMeshControlPlaneGVK = schema.GroupVersionKind{
		Group:   "maistra.io",
		Version: "v2",
		Kind:    "ServiceMeshControlPlane",
	}
)
