package servicemesh

import (
	"context"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/provider"
)

// These keys are used in FeatureData struct, as fields of a struct are not accessible in closures which we define for
// creating and fetching the data.
const (
	authorizationKey = "Auth"
	controlPlaneKey  = "ControlPlane"
)

// FeatureData is a convention to simplify how the data for the Service Mesh features is Defined and accessed.
// Being a "singleton" it is based on anonymous struct concept.
var FeatureData = struct {
	ControlPlane  feature.DataDefinition[*dsciv1.DSCInitializationSpec, ControlPlane]
	Authorization feature.DataDefinition[*dsciv1.DSCInitializationSpec, Authorization]
}{
	Authorization: feature.DataDefinition[*dsciv1.DSCInitializationSpec, Authorization]{
		Create:  CreateAuthorizationData,
		Extract: feature.ExtractEntry[Authorization](authorizationKey),
	},
	ControlPlane: feature.DataDefinition[*dsciv1.DSCInitializationSpec, ControlPlane]{
		Create: func(_ context.Context, _ client.Client, source *dsciv1.DSCInitializationSpec) (ControlPlane, error) {
			return ControlPlane{source.ServiceMesh.ControlPlane}, nil
		},
		Extract: feature.ExtractEntry[ControlPlane](controlPlaneKey),
	},
}

type Authorization struct {
	infrav1.AuthSpec
	InstanceName,
	ProviderName,
	AuthConfigSelector string
}

func (a Authorization) AddTo(f *feature.Feature) error {
	return f.Set(authorizationKey, a)
}

var _ feature.Entry = &Authorization{}

func CreateAuthorizationData(_ context.Context, _ client.Client, dsciSpec *dsciv1.DSCInitializationSpec) (Authorization, error) {
	authNamespace := provider.ValueOf(strings.TrimSpace(dsciSpec.ServiceMesh.Auth.Namespace)).
		OrElse(dsciSpec.ApplicationsNamespace + "-auth-provider")

	config := Authorization{
		AuthSpec: infrav1.AuthSpec{
			Namespace: authNamespace,
			Audiences: dsciSpec.ServiceMesh.Auth.Audiences,
		},
		InstanceName:       "authorino",
		ProviderName:       dsciSpec.ApplicationsNamespace + "-auth-provider",
		AuthConfigSelector: "security.opendatahub.io/authorization-group=default",
	}

	return config, nil
}

type ControlPlane struct {
	infrav1.ControlPlaneSpec
}

func (c ControlPlane) AddTo(f *feature.Feature) error {
	return f.Set(controlPlaneKey, c)
}

var _ feature.Entry = &ControlPlane{}
