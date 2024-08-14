package capabilities

import (
	"embed"
	"io/fs"
	"path"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

//go:embed resources
var capabilityResourcesFS embed.FS

const baseDir = "resources"

var Templates = struct {
	// ServiceMeshIngressDir is the path to the Service Mesh Ingress templates.
	ServiceMeshIngressDir string
	// Location specifies the file system that contains the templates to be used.
	Location fs.FS
	// BaseDir is the path to the base of the embedded FS
	BaseDir string
}{
	ServiceMeshIngressDir: path.Join(baseDir, "servicemesh-ingress"),
	Location:              capabilityResourcesFS,
	BaseDir:               baseDir,
}

// TODO(RHOAIENG-XXXX): promote to API when we flesh out configuration requirements and come up how to align/migrate KServe.
type IngressGatewaySpec struct {
	// Namespace is a namespace where the Service Mesh Ingress should be deployed is deployed. Defaults to "opendatahub-services".
	// +kubebuilder:default=opendatahub-services
	Namespace string `json:"namespace,omitempty"`
	// Name is the name of the Ingress Gateway Service
	// +kubebuilder:default=opendatahub-ingressgateway
	Name string `json:"name,omitempty"`
	// LabelSelectorKey is a key:value defining the label to use for the ingress gateway objects
	// +kubebuilder:default="opendatahub"
	LabelSelectorKey string `json:"labelSelectorKey,omitempty"`
	// LabelSelectorValue is a key:value defining the label to use for the ingress gateway objects
	// +kubebuilder:default="ingressgateway"
	LabelSelectorValue string `json:"labelSelectorValue,omitempty"`
}

type RoutingSpec struct {
	// CertSecretName is the name of the secret that contains the certificate for the Ingress Gateway.
	CertSecretName string
	// IngressGateway is the configuration for the common Ingress Gateway.
	IngressGateway IngressGatewaySpec
	// ControlPlane is the configuration for the Service Mesh Control Plane populated from DSCI.
	ControlPlane infrav1.ControlPlaneSpec
}

func (r RoutingSpec) AddTo(f *feature.Feature) error {
	return f.Set("Routing", r)
}

var _ feature.Entry = &RoutingSpec{}

// NewRoutingSpec creates a new RoutingSpec from the DSCInitializationSpec.
func NewRoutingSpec(spec *dsciv1.DSCInitializationSpec) RoutingSpec {
	appNamespace := spec.ApplicationsNamespace
	return RoutingSpec{
		CertSecretName: appNamespace + "-router-ingress-certs",
		ControlPlane:   spec.ServiceMesh.ControlPlane,
		IngressGateway: IngressGatewaySpec{
			Namespace:          appNamespace + "-services",
			Name:               appNamespace + "-ingress-router",
			LabelSelectorKey:   "istio",
			LabelSelectorValue: appNamespace + "-ingress-gateway",
		},
	}
}
