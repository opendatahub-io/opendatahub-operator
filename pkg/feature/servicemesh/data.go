package servicemesh

import (
	"context"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

// These keys are used in FeatureData struct, as fields of a struct are not accessible in closures which we define for
// creating and fetching the data.
const (
	controlPlaneKey      string = "ControlPlane"
	authKey              string = "Auth"
	authProviderNsKey    string = "AuthNamespace"
	authProviderNameKey  string = "AuthProviderName"
	authExtensionNameKey string = "AuthExtensionName"
)

// FeatureData is a convention to simplify how the data for the Service Mesh features is Defined and accessed.
// Being a "singleton" it is based on anonymous struct concept.
var FeatureData = struct {
	ControlPlane  feature.DataDefinition[dsciv1.DSCInitializationSpec, infrav1.ControlPlaneSpec]
	Authorization AuthorizationData
}{
	ControlPlane: feature.DataDefinition[dsciv1.DSCInitializationSpec, infrav1.ControlPlaneSpec]{
		Define: func(source *dsciv1.DSCInitializationSpec) feature.DataEntry[infrav1.ControlPlaneSpec] {
			return feature.DataEntry[infrav1.ControlPlaneSpec]{
				Key: controlPlaneKey,
				Value: func(_ context.Context, _ client.Client) (infrav1.ControlPlaneSpec, error) {
					return source.ServiceMesh.ControlPlane, nil
				},
			}
		},
		Extract: feature.ExtractEntry[infrav1.ControlPlaneSpec](controlPlaneKey),
	},
	Authorization: AuthorizationData{
		Spec:                  authSpec,
		Namespace:             authNs,
		Provider:              authProvider,
		ExtensionProviderName: authExtensionName,
		All: func(source *dsciv1.DSCInitializationSpec) []feature.Action {
			return []feature.Action{
				authSpec.Define(source).AsAction(),
				authNs.Define(source).AsAction(),
				authProvider.Define(source).AsAction(),
				authExtensionName.Define(source).AsAction(),
			}
		},
	},
}

type AuthorizationData struct {
	Spec                  feature.DataDefinition[dsciv1.DSCInitializationSpec, infrav1.AuthSpec]
	Namespace             feature.DataDefinition[dsciv1.DSCInitializationSpec, string]
	Provider              feature.DataDefinition[dsciv1.DSCInitializationSpec, string]
	ExtensionProviderName feature.DataDefinition[dsciv1.DSCInitializationSpec, string]
	All                   func(source *dsciv1.DSCInitializationSpec) []feature.Action
}

var authSpec = feature.DataDefinition[dsciv1.DSCInitializationSpec, infrav1.AuthSpec]{
	Define: func(source *dsciv1.DSCInitializationSpec) feature.DataEntry[infrav1.AuthSpec] {
		return feature.DataEntry[infrav1.AuthSpec]{
			Key: authKey,
			Value: func(_ context.Context, _ client.Client) (infrav1.AuthSpec, error) {
				return source.ServiceMesh.Auth, nil
			},
		}
	},
	Extract: feature.ExtractEntry[infrav1.AuthSpec](authKey),
}

var authNs = feature.DataDefinition[dsciv1.DSCInitializationSpec, string]{
	Define: func(source *dsciv1.DSCInitializationSpec) feature.DataEntry[string] {
		return feature.DataEntry[string]{
			Key: authProviderNsKey,
			Value: func(_ context.Context, _ client.Client) (string, error) {
				ns := strings.TrimSpace(source.ServiceMesh.Auth.Namespace)
				if len(ns) == 0 {
					ns = source.ApplicationsNamespace + "-auth-provider"
				}

				return ns, nil
			},
		}
	},
	Extract: feature.ExtractEntry[string](authProviderNsKey),
}

var authProvider = feature.DataDefinition[dsciv1.DSCInitializationSpec, string]{
	Define: func(source *dsciv1.DSCInitializationSpec) feature.DataEntry[string] {
		return feature.DataEntry[string]{
			Key: authProviderNameKey,
			Value: func(_ context.Context, _ client.Client) (string, error) {
				return "authorino", nil
			},
		}
	},
	Extract: feature.ExtractEntry[string](authProviderNameKey),
}

var authExtensionName = feature.DataDefinition[dsciv1.DSCInitializationSpec, string]{
	Define: func(source *dsciv1.DSCInitializationSpec) feature.DataEntry[string] {
		return feature.DataEntry[string]{
			Key: authExtensionNameKey,
			Value: func(_ context.Context, _ client.Client) (string, error) {
				return source.ApplicationsNamespace + "-auth-provider", nil
			},
		}
	},
	Extract: feature.ExtractEntry[string](authExtensionNameKey),
}
