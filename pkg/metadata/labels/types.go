package labels

const (
	ODHAppPrefix           = "app.opendatahub.io"
	ODHPlatformPrefix      = "platform.opendatahub.io"
	InjectTrustCA          = "config.openshift.io/inject-trusted-cabundle"
	SecurityEnforce        = "pod-security.kubernetes.io/enforce"
	ClusterMonitoring      = "openshift.io/cluster-monitoring"
	PlatformPartOf         = ODHPlatformPrefix + "/part-of"
	PlatformDependency     = ODHPlatformPrefix + "/dependency"
	Platform               = "platform"
	True                   = "true"
	CustomizedAppNamespace = "opendatahub.io/application-namespace"
)

// K8SCommon keeps common kubernetes labels [1]
// used across the project.
// [1] (https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/#labels)
var K8SCommon = struct {
	PartOf    string
	Component string
}{
	PartOf:    "app.kubernetes.io/part-of",
	Component: "app.kubernetes.io/component",
}

// GatewayAPI holds Gateway API labels [1]
// used across the project.
// [1] (https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.Gateway)
var GatewayAPI = struct {
	GatewayName string
}{
	GatewayName: "gateway.networking.k8s.io/gateway-name",
}

// ODH holds Open Data Hub specific labels grouped by types.
var ODH = struct {
	OwnedNamespace string
	Component      func(string) string
}{
	OwnedNamespace: "opendatahub.io/generated-namespace",
	Component: func(name string) string {
		return ODHAppPrefix + "/" + name
	},
}
