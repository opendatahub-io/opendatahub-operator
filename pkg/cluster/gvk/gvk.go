package gvk

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	ClusterServiceVersion = schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "ClusterServiceVersion",
	}

	DataScienceCluster = schema.GroupVersionKind{
		Group:   "datasciencecluster.opendatahub.io",
		Version: "v1",
		Kind:    "DataScienceCluster",
	}
	DSCInitialization = schema.GroupVersionKind{
		Group:   "dscinitialization.opendatahub.io",
		Version: "v1",
		Kind:    "DSCInitialization",
	}

	Deployment = schema.GroupVersionKind{
		Group:   appsv1.SchemeGroupVersion.Group,
		Version: appsv1.SchemeGroupVersion.Version,
		Kind:    "Deployment",
	}

	ClusterRole = schema.GroupVersionKind{
		Group:   "rbac.authorization.k8s.io",
		Version: "v1",
		Kind:    "ClusterRole",
	}

	RoleBinding = schema.GroupVersionKind{
		Group:   rbacv1.SchemeGroupVersion.Group,
		Version: rbacv1.SchemeGroupVersion.Version,
		Kind:    "RoleBinding",
	}

	Secret = schema.GroupVersionKind{
		Group:   corev1.SchemeGroupVersion.Group,
		Version: corev1.SchemeGroupVersion.Version,
		Kind:    "Secret",
	}

	ConfigMap = schema.GroupVersionKind{
		Group:   corev1.SchemeGroupVersion.Group,
		Version: corev1.SchemeGroupVersion.Version,
		Kind:    "ConfigMap",
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
		Version: "v1",
		Kind:    "OdhDocument",
	}

	AcceleratorProfile = schema.GroupVersionKind{
		Group:   "dashboard.opendatahub.io",
		Version: "v1",
		Kind:    "AcceleratorProfile",
	}

	OdhQuickStart = schema.GroupVersionKind{
		Group:   "console.openshift.io",
		Version: "v1",
		Kind:    "OdhQuickStart",
	}

	OdhDashboardConfig = schema.GroupVersionKind{
		Group:   "opendatahub.io",
		Version: "v1alpha",
		Kind:    "OdhDashboardConfig",
	}

	Dashboard = schema.GroupVersionKind{
		Group:   "components.opendatahub.io",
		Version: "v1",
		Kind:    "Dashboard",
	}

	Workbenches = schema.GroupVersionKind{
		Group:   "components.opendatahub.io",
		Version: "v1",
		Kind:    "Workbenches",
	}

	ModelMeshServing = schema.GroupVersionKind{
		Group:   "components.opendatahub.io",
		Version: "v1",
		Kind:    "ModelMeshServing",
	}

	DataSciencePipelines = schema.GroupVersionKind{
		Group:   "components.opendatahub.io",
		Version: "v1",
		Kind:    "DataSciencePipelines",
	}

	Kserve = schema.GroupVersionKind{
		Group:   "components.opendatahub.io",
		Version: "v1",
		Kind:    "Kserve",
	}

	CodeFlare = schema.GroupVersionKind{
		Group:   "components.opendatahub.io",
		Version: "v1",
		Kind:    "CodeFlare",
	}

	Ray = schema.GroupVersionKind{
		Group:   "components.opendatahub.io",
		Version: "v1",
		Kind:    "Ray",
	}

	TrustyAI = schema.GroupVersionKind{
		Group:   "components.opendatahub.io",
		Version: "v1",
		Kind:    "TrustyAI",
	}

	ModelRegistry = schema.GroupVersionKind{
		Group:   "components.opendatahub.io",
		Version: "v1",
		Kind:    "ModelRegistry",
	}

	TrainingOperator = schema.GroupVersionKind{
		Group:   "components.opendatahub.io",
		Version: "v1",
		Kind:    "TrainingOperator",
	}

	CustomResourceDefinition = schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
	}

	ServiceMeshMember = schema.GroupVersionKind{
		Group:   "maistra.io",
		Version: "v1",
		Kind:    "ServiceMeshMember",
	}
)
