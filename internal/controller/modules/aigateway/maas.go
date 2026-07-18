package aigateway

const (
	// MaaSGatewayNamespace is the namespace where the MaaS Gateway and
	// payload-processing workload run (must match maas-controller config).
	MaaSGatewayNamespace = "openshift-ingress"

	// MaaSGatewayName is the name of the MaaS Gateway resource.
	MaaSGatewayName = "maas-default-gateway"

	// MaaSSubscriptionNamespace is the namespace where MaaS CRs live
	// (Tenant, MaaSSubscription, MaaSAuthPolicy). Must match the
	// maas-controller --maas-subscription-namespace flag.
	MaaSSubscriptionNamespace = "models-as-a-service"

	// MaaSControllerDeploymentName is the maas-controller workload Deployment name
	// (must match upstream manager kustomize).
	MaaSControllerDeploymentName = "maas-controller"
)
