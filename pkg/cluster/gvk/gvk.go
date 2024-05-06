package gvk

import "k8s.io/apimachinery/pkg/runtime/schema"

var (
	ClusterRole = schema.GroupVersionKind{
		Group:   "rbac.authorization.k8s.io",
		Version: "v1",
		Kind:    "ClusterRole",
	}
	ClusterRoleBinding = schema.GroupVersionKind{
		Group:   "rbac.authorization.k8s.io",
		Version: "v1",
		Kind:    "ClusterRoleBinding",
	}
	Deployment = schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}
	KfDef = schema.GroupVersionKind{
		Group:   "kfdef.apps.kubeflow.org",
		Version: "v1", Kind: "KfDef",
	}
	KnativeServing = schema.GroupVersionKind{
		Group:   "operator.knative.dev",
		Version: "v1beta1",
		Kind:    "KnativeServing",
	}
	Namespace = schema.GroupVersionKind{
		Group:   "",
		Version: "v1", Kind: "Namespace",
	}
	OdhApplication = schema.GroupVersionKind{
		Group:   "dashboard.opendatahub.io",
		Version: "v1",
		Kind:    "OdhApplication",
	}
	OdhDashboardConfig = schema.GroupVersionKind{
		Group:   "opendatahub.io",
		Version: "v1alpha",
		Kind:    "OdhDashboardConfig",
	}
	OdhDocument = schema.GroupVersionKind{
		Group:   "dashboard.opendatahub.io",
		Version: "v1", Kind: "OdhDocument",
	}
	OdhQuickStart = schema.GroupVersionKind{
		Group:   "console.openshift.io",
		Version: "v1", Kind: "OdhQuickStart",
	}
	OpenshiftIngress = schema.GroupVersionKind{
		Group:   "config.openshift.io",
		Version: "v1",
		Kind:    "Ingress",
	}
	Route = schema.GroupVersionKind{
		Group:   "route.openshift.io",
		Version: "v1", Kind: "Route",
	}
	Secret = schema.GroupVersionKind{
		Group:   "",
		Version: "v1", Kind: "Secret",
	}
	Service = schema.GroupVersionKind{
		Group:   "",
		Version: "v1", Kind: "Service",
	}
	ServiceAccount = schema.GroupVersionKind{
		Group:   "",
		Version: "v1", Kind: "ServiceAccount",
	}
	ServiceMeshControlPlane = schema.GroupVersionKind{
		Group:   "maistra.io",
		Version: "v2", Kind: "ServiceMeshControlPlane",
	}
	ServiceMonitor = schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1", Kind: "ServiceMonitor",
	}
	StatefulSet = schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1", Kind: "StatefulSet",
	}
)
