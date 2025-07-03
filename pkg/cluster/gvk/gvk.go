package gvk

import (
	configv1 "github.com/openshift/api/config/v1"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	featuresv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	hwpv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
)

var (
	Namespace = schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	}

	OperatorGroup = schema.GroupVersionKind{
		Group:   operatorsv1.SchemeGroupVersion.Group,
		Version: operatorsv1.SchemeGroupVersion.Version,
		Kind:    "OperatorGroup",
	}

	Subscription = schema.GroupVersionKind{
		Group:   operatorsv1alpha1.SchemeGroupVersion.Group,
		Version: operatorsv1alpha1.SchemeGroupVersion.Version,
		Kind:    operatorsv1alpha1.SubscriptionKind,
	}

	InstallPlan = schema.GroupVersionKind{
		Group:   operatorsv1alpha1.SchemeGroupVersion.Group,
		Version: operatorsv1alpha1.SchemeGroupVersion.Version,
		Kind:    operatorsv1alpha1.InstallPlanKind,
	}

	ClusterServiceVersion = schema.GroupVersionKind{
		Group:   operatorsv1alpha1.SchemeGroupVersion.Group,
		Version: operatorsv1alpha1.SchemeGroupVersion.Version,
		Kind:    operatorsv1alpha1.ClusterServiceVersionKind,
	}

	ClusterVersion = schema.GroupVersionKind{
		Group:   configv1.SchemeGroupVersion.Group,
		Version: configv1.SchemeGroupVersion.Version,
		Kind:    "ClusterVersion",
	}

	DataScienceCluster = schema.GroupVersionKind{
		Group:   dscv1.GroupVersion.Group,
		Version: dscv1.GroupVersion.Version,
		Kind:    "DataScienceCluster",
	}

	DSCInitialization = schema.GroupVersionKind{
		Group:   dsciv1.GroupVersion.Group,
		Version: dsciv1.GroupVersion.Version,
		Kind:    "DSCInitialization",
	}

	HardwareProfile = schema.GroupVersionKind{
		Group:   hwpv1alpha1.GroupVersion.Group,
		Version: hwpv1alpha1.GroupVersion.Version,
		Kind:    "HardwareProfile",
	}

	FeatureTracker = schema.GroupVersionKind{
		Group:   featuresv1.GroupVersion.Group,
		Version: featuresv1.GroupVersion.Version,
		Kind:    "FeatureTracker",
	}

	Pod = schema.GroupVersionKind{
		Group:   corev1.SchemeGroupVersion.Group,
		Version: corev1.SchemeGroupVersion.Version,
		Kind:    "Pod",
	}

	Deployment = schema.GroupVersionKind{
		Group:   appsv1.SchemeGroupVersion.Group,
		Version: appsv1.SchemeGroupVersion.Version,
		Kind:    "Deployment",
	}

	OpenShiftClusterRole = schema.GroupVersionKind{
		Group:   "authorization.openshift.io",
		Version: "v1",
		Kind:    "ClusterRole",
	}

	ClusterRole = schema.GroupVersionKind{
		Group:   rbacv1.SchemeGroupVersion.Group,
		Version: rbacv1.SchemeGroupVersion.Version,
		Kind:    "ClusterRole",
	}

	ClusterRoleBinding = schema.GroupVersionKind{
		Group:   rbacv1.SchemeGroupVersion.Group,
		Version: rbacv1.SchemeGroupVersion.Version,
		Kind:    "ClusterRoleBinding",
	}

	Role = schema.GroupVersionKind{
		Group:   rbacv1.SchemeGroupVersion.Group,
		Version: rbacv1.SchemeGroupVersion.Version,
		Kind:    "Role",
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

	DashboardHardwareProfile = schema.GroupVersionKind{
		Group:   "dashboard.opendatahub.io",
		Version: "v1alpha1",
		Kind:    "HardwareProfile",
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
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.DashboardKind,
	}

	Workbenches = schema.GroupVersionKind{
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.WorkbenchesKind,
	}

	ModelController = schema.GroupVersionKind{
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.ModelControllerKind,
	}

	ModelMeshServing = schema.GroupVersionKind{
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.ModelMeshServingKind,
	}

	DataSciencePipelines = schema.GroupVersionKind{
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.DataSciencePipelinesKind,
	}

	Kserve = schema.GroupVersionKind{
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.KserveKind,
	}

	LlamaStackOperator = schema.GroupVersionKind{
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.LlamaStackOperatorKind,
	}

	Kueue = schema.GroupVersionKind{
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.KueueKind,
	}

	CodeFlare = schema.GroupVersionKind{
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.CodeFlareKind,
	}

	Ray = schema.GroupVersionKind{
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.RayKind,
	}

	TrustyAI = schema.GroupVersionKind{
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.TrustyAIKind,
	}

	ModelRegistry = schema.GroupVersionKind{
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.ModelRegistryKind,
	}

	TrainingOperator = schema.GroupVersionKind{
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.TrainingOperatorKind,
	}

	Monitoring = schema.GroupVersionKind{
		Group:   serviceApi.GroupVersion.Group,
		Version: serviceApi.GroupVersion.Version,
		Kind:    serviceApi.MonitoringKind,
	}

	FeastOperator = schema.GroupVersionKind{
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.FeastOperatorKind,
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

	Lease = schema.GroupVersionKind{
		Group:   coordinationv1.SchemeGroupVersion.Group,
		Version: coordinationv1.SchemeGroupVersion.Version,
		Kind:    "Lease",
	}

	EnvoyFilter = schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1alpha3",
		Kind:    "EnvoyFilter",
	}

	AuthorizationPolicy = schema.GroupVersionKind{
		Group:   "security.istio.io",
		Version: "v1",
		Kind:    "AuthorizationPolicy",
	}

	AuthorizationPolicyv1beta1 = schema.GroupVersionKind{
		Group:   "security.istio.io",
		Version: "v1beta1",
		Kind:    "AuthorizationPolicy",
	}

	Gateway = schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "Gateway",
	}

	Auth = schema.GroupVersionKind{
		Group:   serviceApi.GroupVersion.Group,
		Version: serviceApi.GroupVersion.Version,
		Kind:    serviceApi.AuthKind,
	}

	MultiKueueConfigV1Alpha1 = schema.GroupVersionKind{
		Group:   "kueue.x-k8s.io",
		Version: "v1alpha1",
		Kind:    "MultiKueueConfig",
	}

	MultikueueClusterV1Alpha1 = schema.GroupVersionKind{
		Group:   "kueue.x-k8s.io",
		Version: "v1alpha1",
		Kind:    "MultiKueueCluster",
	}

	InferenceServices = schema.GroupVersionKind{
		Group:   "serving.kserve.io",
		Version: "v1beta1",
		Kind:    "InferenceService",
	}

	OperatorCondition = schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v2",
		Kind:    "OperatorCondition",
	}

	NetworkPolicy = schema.GroupVersionKind{
		Group:   networkingv1.SchemeGroupVersion.Group,
		Version: networkingv1.SchemeGroupVersion.Version,
		Kind:    "NetworkPolicy",
	}

	MonitoringStack = schema.GroupVersionKind{
		Group:   "monitoring.rhobs",
		Version: "v1alpha1",
		Kind:    "MonitoringStack",
	}
)
