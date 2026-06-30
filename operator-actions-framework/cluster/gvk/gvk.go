package gvk

import (
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	LeaderWorkerSetOperatorCRDname = "leaderworkersetoperators.operator.openshift.io"
	SubscriptionCRDname            = "subscriptions.operators.coreos.com"
	VariantAutoscalingCRDname      = "variantautoscalings.llmd.ai"
)

var (
	Namespace = schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
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

	HorizontalPodAutoscaler = schema.GroupVersionKind{
		Group:   autoscalingv2.SchemeGroupVersion.Group,
		Version: autoscalingv2.SchemeGroupVersion.Version,
		Kind:    "HorizontalPodAutoscaler",
	}

	StatefulSet = schema.GroupVersionKind{
		Group:   appsv1.SchemeGroupVersion.Group,
		Version: appsv1.SchemeGroupVersion.Version,
		Kind:    "StatefulSet",
	}

	DaemonSet = schema.GroupVersionKind{
		Group:   appsv1.SchemeGroupVersion.Group,
		Version: appsv1.SchemeGroupVersion.Version,
		Kind:    "DaemonSet",
	}

	Job = schema.GroupVersionKind{
		Group:   batchv1.SchemeGroupVersion.Group,
		Version: batchv1.SchemeGroupVersion.Version,
		Kind:    "Job",
	}

	CronJob = schema.GroupVersionKind{
		Group:   batchv1.SchemeGroupVersion.Group,
		Version: batchv1.SchemeGroupVersion.Version,
		Kind:    "CronJob",
	}

	ResourceQuota = schema.GroupVersionKind{
		Group:   corev1.SchemeGroupVersion.Group,
		Version: corev1.SchemeGroupVersion.Version,
		Kind:    "ResourceQuota",
	}

	Group = schema.GroupVersionKind{
		Group:   rbacv1.SchemeGroupVersion.Group,
		Version: rbacv1.SchemeGroupVersion.Version,
		Kind:    "Group",
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

	ServiceAccount = schema.GroupVersionKind{
		Group:   corev1.SchemeGroupVersion.Group,
		Version: corev1.SchemeGroupVersion.Version,
		Kind:    "ServiceAccount",
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

	Service = schema.GroupVersionKind{
		Group:   corev1.SchemeGroupVersion.Group,
		Version: corev1.SchemeGroupVersion.Version,
		Kind:    "Service",
	}

	OpenshiftIngress = schema.GroupVersionKind{
		Group:   "config.openshift.io",
		Version: "v1",
		Kind:    "Ingress",
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

	DashboardAcceleratorProfile = schema.GroupVersionKind{
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

	Tenant = schema.GroupVersionKind{
		Group:   "maas.opendatahub.io",
		Version: "v1alpha1",
		Kind:    "Tenant",
	}

	CustomResourceDefinition = schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
	}

	Lease = schema.GroupVersionKind{
		Group:   coordinationv1.SchemeGroupVersion.Group,
		Version: coordinationv1.SchemeGroupVersion.Version,
		Kind:    "Lease",
	}

	// networking.istio.io.

	DestinationRule = schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1",
		Kind:    "DestinationRule",
	}

	EnvoyFilter = schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1alpha3",
		Kind:    "EnvoyFilter",
	}

	IstioGateway = schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1",
		Kind:    "Gateway",
	}

	ProxyConfig = schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "ProxyConfig",
	}

	ServiceEntry = schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1",
		Kind:    "ServiceEntry",
	}

	Sidecar = schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1",
		Kind:    "Sidecar",
	}

	WorkloadEntry = schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1",
		Kind:    "WorkloadEntry",
	}

	WorkloadGroup = schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1",
		Kind:    "WorkloadGroup",
	}

	// security.istio.io.

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

	PeerAuthentication = schema.GroupVersionKind{
		Group:   "security.istio.io",
		Version: "v1",
		Kind:    "PeerAuthentication",
	}

	RequestAuthentication = schema.GroupVersionKind{
		Group:   "security.istio.io",
		Version: "v1",
		Kind:    "RequestAuthentication",
	}

	// telemetry.istio.io.

	Telemetry = schema.GroupVersionKind{
		Group:   "telemetry.istio.io",
		Version: "v1",
		Kind:    "Telemetry",
	}

	// extensions.istio.io.

	WasmPlugin = schema.GroupVersionKind{
		Group:   "extensions.istio.io",
		Version: "v1alpha1",
		Kind:    "WasmPlugin",
	}

	// sailoperator.io.

	Istio = schema.GroupVersionKind{
		Group:   "sailoperator.io",
		Version: "v1",
		Kind:    "Istio",
	}

	// leaderworkerset.x-k8s.io.

	LeaderWorkerSetV1 = schema.GroupVersionKind{
		Group:   "leaderworkerset.x-k8s.io",
		Version: "v1",
		Kind:    "LeaderWorkerSet",
	}

	// kueue.x-k8s.io.

	LocalQueue = schema.GroupVersionKind{
		Group:   "kueue.x-k8s.io",
		Version: "v1beta1",
		Kind:    "LocalQueue",
	}

	ClusterQueue = schema.GroupVersionKind{
		Group:   "kueue.x-k8s.io",
		Version: "v1beta1",
		Kind:    "ClusterQueue",
	}

	ResourceFlavor = schema.GroupVersionKind{
		Group:   "kueue.x-k8s.io",
		Version: "v1beta1",
		Kind:    "ResourceFlavor",
	}

	KueueConfigV1 = schema.GroupVersionKind{
		Group:   "kueue.openshift.io",
		Version: "v1",
		Kind:    "Kueue",
	}

	// operator.openshift.io.

	CertManagerV1Alpha1 = schema.GroupVersionKind{
		Group:   "operator.openshift.io",
		Version: "v1alpha1",
		Kind:    "CertManager",
	}

	LeaderWorkerSetOperatorV1 = schema.GroupVersionKind{
		Group:   "operator.openshift.io",
		Version: "v1",
		Kind:    "LeaderWorkerSetOperator",
	}

	JobSetOperatorV1 = schema.GroupVersionKind{
		Group:   "operator.openshift.io",
		Version: "v1",
		Kind:    "JobSetOperator",
	}

	InferenceServices = schema.GroupVersionKind{
		Group:   "serving.kserve.io",
		Version: "v1beta1",
		Kind:    "InferenceService",
	}

	ServingRuntime = schema.GroupVersionKind{
		Group:   "serving.kserve.io",
		Version: "v1alpha1",
		Kind:    "ServingRuntime",
	}

	Notebook = schema.GroupVersionKind{
		Group:   "kubeflow.org",
		Version: "v1",
		Kind:    "Notebook",
	}

	LLMInferenceServiceConfigV1Alpha1 = schema.GroupVersionKind{
		Group:   "serving.kserve.io",
		Version: "v1alpha1",
		Kind:    "LLMInferenceServiceConfig",
	}

	LLMInferenceServiceConfigV1Alpha2 = schema.GroupVersionKind{
		Group:   "serving.kserve.io",
		Version: "v1alpha2",
		Kind:    "LLMInferenceServiceConfig",
	}

	LLMInferenceServiceV1Alpha1 = schema.GroupVersionKind{
		Group:   "serving.kserve.io",
		Version: "v1alpha1",
		Kind:    "LLMInferenceService",
	}

	LLMInferenceServiceV1Alpha2 = schema.GroupVersionKind{
		Group:   "serving.kserve.io",
		Version: "v1alpha2",
		Kind:    "LLMInferenceService",
	}

	InferencePoolV1alpha2 = schema.GroupVersionKind{
		Group:   "inference.networking.x-k8s.io",
		Version: "v1alpha2",
		Kind:    "InferencePool",
	}

	InferencePoolV1 = schema.GroupVersionKind{
		Group:   "inference.networking.k8s.io",
		Version: "v1",
		Kind:    "InferencePool",
	}

	InferenceModelV1alpha2 = schema.GroupVersionKind{
		Group:   "inference.networking.x-k8s.io",
		Version: "v1alpha2",
		Kind:    "InferenceModel",
	}

	VariantAutoscaling = schema.GroupVersionKind{
		Group:   "llmd.ai",
		Version: "v1alpha1",
		Kind:    "VariantAutoscaling",
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

	PyTorchJob = schema.GroupVersionKind{
		Group:   "kubeflow.org",
		Version: "v1",
		Kind:    "PyTorchJob",
	}

	TrainJob = schema.GroupVersionKind{
		Group:   "trainer.kubeflow.org",
		Version: "v1alpha1",
		Kind:    "TrainJob",
	}

	ClusterTrainingRuntime = schema.GroupVersionKind{
		Group:   "trainer.kubeflow.org",
		Version: "v1alpha1",
		Kind:    "ClusterTrainingRuntime",
	}

	RayJobV1Alpha1 = schema.GroupVersionKind{
		Group:   "ray.io",
		Version: "v1alpha1",
		Kind:    "RayJob",
	}

	RayJobV1 = schema.GroupVersionKind{
		Group:   "ray.io",
		Version: "v1",
		Kind:    "RayJob",
	}

	RayClusterV1Alpha1 = schema.GroupVersionKind{
		Group:   "ray.io",
		Version: "v1alpha1",
		Kind:    "RayCluster",
	}

	RayClusterV1 = schema.GroupVersionKind{
		Group:   "ray.io",
		Version: "v1",
		Kind:    "RayCluster",
	}

	TempoMonolithic = schema.GroupVersionKind{
		Group:   "tempo.grafana.com",
		Version: "v1alpha1",
		Kind:    "TempoMonolithic",
	}

	TempoStack = schema.GroupVersionKind{
		Group:   "tempo.grafana.com",
		Version: "v1alpha1",
		Kind:    "TempoStack",
	}

	OpenTelemetryCollector = schema.GroupVersionKind{
		Group:   "opentelemetry.io",
		Version: "v1beta1",
		Kind:    "OpenTelemetryCollector",
	}

	Instrumentation = schema.GroupVersionKind{
		Group:   "opentelemetry.io",
		Version: "v1alpha1",
		Kind:    "Instrumentation",
	}

	ImageStream = schema.GroupVersionKind{
		Group:   "image.openshift.io",
		Version: "v1",
		Kind:    "ImageStream",
	}

	OpenshiftTemplate = schema.GroupVersionKind{
		Group:   "template.openshift.io",
		Version: "v1",
		Kind:    "Template",
	}

	ServiceMonitor = schema.GroupVersionKind{
		Group:   "monitoring.rhobs",
		Version: "v1",
		Kind:    "ServiceMonitor",
	}

	CoreosServiceMonitor = schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "ServiceMonitor",
	}

	CoreosPodMonitor = schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "PodMonitor",
	}

	PrometheusRule = schema.GroupVersionKind{
		Group:   "monitoring.rhobs",
		Version: "v1",
		Kind:    "PrometheusRule",
	}

	PersesV1Alpha1 = schema.GroupVersionKind{
		Group:   "perses.dev",
		Version: "v1alpha1",
		Kind:    "Perses",
	}
	PersesV1Alpha2 = schema.GroupVersionKind{
		Group:   "perses.dev",
		Version: "v1alpha2",
		Kind:    "Perses",
	}
	ThanosQuerier = schema.GroupVersionKind{
		Group:   "monitoring.rhobs",
		Version: "v1alpha1",
		Kind:    "ThanosQuerier",
	}

	PersesDatasourceV1Alpha1 = schema.GroupVersionKind{
		Group:   "perses.dev",
		Version: "v1alpha1",
		Kind:    "PersesDatasource",
	}
	PersesDatasourceV1Alpha2 = schema.GroupVersionKind{
		Group:   "perses.dev",
		Version: "v1alpha2",
		Kind:    "PersesDatasource",
	}
	PersesDatasource = PersesDatasourceV1Alpha2

	PersesDashboardV1Alpha1 = schema.GroupVersionKind{
		Group:   "perses.dev",
		Version: "v1alpha1",
		Kind:    "PersesDashboard",
	}
	PersesDashboardV1Alpha2 = schema.GroupVersionKind{
		Group:   "perses.dev",
		Version: "v1alpha2",
		Kind:    "PersesDashboard",
	}
	PersesDashboard = PersesDashboardV1Alpha2

	ValidatingAdmissionPolicy = schema.GroupVersionKind{
		Group:   "admissionregistration.k8s.io",
		Version: "v1",
		Kind:    "ValidatingAdmissionPolicy",
	}

	ValidatingAdmissionPolicyBinding = schema.GroupVersionKind{
		Group:   "admissionregistration.k8s.io",
		Version: "v1",
		Kind:    "ValidatingAdmissionPolicyBinding",
	}

	MutatingWebhookConfiguration = schema.GroupVersionKind{
		Group:   admissionregistrationv1.SchemeGroupVersion.Group,
		Version: admissionregistrationv1.SchemeGroupVersion.Version,
		Kind:    "MutatingWebhookConfiguration",
	}

	ValidatingWebhookConfiguration = schema.GroupVersionKind{
		Group:   admissionregistrationv1.SchemeGroupVersion.Group,
		Version: admissionregistrationv1.SchemeGroupVersion.Version,
		Kind:    "ValidatingWebhookConfiguration",
	}

	AuthPolicyv1 = schema.GroupVersionKind{
		Group:   "kuadrant.io",
		Version: "v1",
		Kind:    "AuthPolicy",
	}

	RateLimitPolicyv1 = schema.GroupVersionKind{
		Group:   "kuadrant.io",
		Version: "v1",
		Kind:    "RateLimitPolicy",
	}

	TelemetryPolicyv1alpha1 = schema.GroupVersionKind{
		Group:   "extensions.kuadrant.io",
		Version: "v1alpha1",
		Kind:    "TelemetryPolicy",
	}

	AuthConfigv1beta3 = schema.GroupVersionKind{
		Group:   "authorino.kuadrant.io",
		Version: "v1beta3",
		Kind:    "AuthConfig",
	}

	Authorinov1beta1 = schema.GroupVersionKind{
		Group:   "operator.authorino.kuadrant.io",
		Version: "v1beta1",
		Kind:    "Authorino",
	}

	Kuadrantv1beta1 = schema.GroupVersionKind{
		Group:   "kuadrant.io",
		Version: "v1beta1",
		Kind:    "Kuadrant",
	}

	JobSetv1alpha2 = schema.GroupVersionKind{
		Group:   "jobset.x-k8s.io",
		Version: "v1alpha2",
		Kind:    "JobSet",
	}

	MLflow = schema.GroupVersionKind{
		Group:   "mlflow.opendatahub.io",
		Version: "v1",
		Kind:    "MLflow",
	}

	PersistentVolumeClaim = schema.GroupVersionKind{
		Group:   corev1.SchemeGroupVersion.Group,
		Version: corev1.SchemeGroupVersion.Version,
		Kind:    "PersistentVolumeClaim",
	}

	// cert-manager.io.

	CertManagerCertificate = schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Certificate",
	}

	CertManagerCertificateRequest = schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "CertificateRequest",
	}

	CertManagerIssuer = schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Issuer",
	}

	CertManagerClusterIssuer = schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "ClusterIssuer",
	}

	AzureKubernetesEngine = schema.GroupVersionKind{
		Group:   "infrastructure.opendatahub.io",
		Version: "v1alpha1",
		Kind:    "AzureKubernetesEngine",
	}

	CoreWeaveKubernetesEngine = schema.GroupVersionKind{
		Group:   "infrastructure.opendatahub.io",
		Version: "v1alpha1",
		Kind:    "CoreWeaveKubernetesEngine",
	}

	SparkApplication = schema.GroupVersionKind{
		Group:   "sparkoperator.k8s.io",
		Version: "v1beta2",
		Kind:    "SparkApplication",
	}

	ScheduledSparkApplication = schema.GroupVersionKind{
		Group:   "sparkoperator.k8s.io",
		Version: "v1beta2",
		Kind:    "ScheduledSparkApplication",
	}
)
