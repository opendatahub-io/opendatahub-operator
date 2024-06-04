package gvk

import "k8s.io/apimachinery/pkg/runtime/schema"

var (
	ClusterServiceVersion = schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "ClusterServiceVersion",
	}

	KnativeServing = schema.GroupVersionKind{
		Group:   "operator.knative.dev",
		Version: "v1beta1",
		Kind:    "KnativeServing",
	}

	OpenshiftIngress = schema.GroupVersionKind{
		Group:   "config.openshift.io",
		Version: "v1",
		Kind:    "Ingress",
	}

	ServiceMeshControlPlane = schema.GroupVersionKind{
		Group:   "maistra.io",
		Version: "v2",
		Kind:    "ServiceMeshControlPlane",
	}

	OdhApplication = schema.GroupVersionKind{
		Group:   "dashboard.opendatahub.io",
		Version: "v1",
		Kind:    "OdhApplication",
	}
	OdhDocument = schema.GroupVersionKind{
		Group:   "dashboard.opendatahub.io",
		Version: "v1", Kind: "OdhDocument",
	}
	OdhQuickStart = schema.GroupVersionKind{
		Group:   "console.openshift.io",
		Version: "v1", Kind: "OdhQuickStart",
	}

	OdhDashboardConfig = schema.GroupVersionKind{
		Group:   "opendatahub.io",
		Version: "v1alpha",
		Kind:    "OdhDashboardConfig",
	}
)
