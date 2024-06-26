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

// FeatureData is a convention to simplify how the data for the Service Mesh features is created and accessed.
// Being a "singleton" it is based on anonymous struct concept.
var FeatureData = struct {
	ControlPlane  feature.ContextDefinition[dsciv1.DSCInitializationSpec, infrav1.ControlPlaneSpec]
	Authorization AuthorizationContext
}{
	ControlPlane: feature.ContextDefinition[dsciv1.DSCInitializationSpec, infrav1.ControlPlaneSpec]{
		Create: func(source *dsciv1.DSCInitializationSpec) feature.ContextEntry[infrav1.ControlPlaneSpec] {
			return feature.ContextEntry[infrav1.ControlPlaneSpec]{
				Key: controlPlaneKey,
				Value: func(_ context.Context, _ client.Client) (infrav1.ControlPlaneSpec, error) {
					return source.ServiceMesh.ControlPlane, nil
				},
			}
		},
		From: feature.ExtractEntry[infrav1.ControlPlaneSpec](controlPlaneKey),
	},
	Authorization: AuthorizationContext{
		Spec:                  authSpec,
		Namespace:             authNs,
		Provider:              authProvider,
		ExtensionProviderName: authExtensionName,
		All: func(source *dsciv1.DSCInitializationSpec) []feature.Action {
			return []feature.Action{
				authSpec.Create(source).AsAction(),
				authNs.Create(source).AsAction(),
				authProvider.Create(source).AsAction(),
				authExtensionName.Create(source).AsAction(),
			}
		},
	},
}

type AuthorizationContext struct {
	Spec                  feature.ContextDefinition[dsciv1.DSCInitializationSpec, infrav1.AuthSpec]
	Namespace             feature.ContextDefinition[dsciv1.DSCInitializationSpec, string]
	Provider              feature.ContextDefinition[dsciv1.DSCInitializationSpec, string]
	ExtensionProviderName feature.ContextDefinition[dsciv1.DSCInitializationSpec, string]
	All                   func(source *dsciv1.DSCInitializationSpec) []feature.Action
}

var authSpec = feature.ContextDefinition[dsciv1.DSCInitializationSpec, infrav1.AuthSpec]{
	Create: func(source *dsciv1.DSCInitializationSpec) feature.ContextEntry[infrav1.AuthSpec] {
		return feature.ContextEntry[infrav1.AuthSpec]{
			Key: authKey,
			Value: func(_ context.Context, _ client.Client) (infrav1.AuthSpec, error) {
				return source.ServiceMesh.Auth, nil
			},
		}
	},
	From: feature.ExtractEntry[infrav1.AuthSpec](authKey),
}

var authNs = feature.ContextDefinition[dsciv1.DSCInitializationSpec, string]{
	Create: func(source *dsciv1.DSCInitializationSpec) feature.ContextEntry[string] {
		return feature.ContextEntry[string]{
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
	From: feature.ExtractEntry[string](authProviderNsKey),
}

var authProvider = feature.ContextDefinition[dsciv1.DSCInitializationSpec, string]{
	Create: func(source *dsciv1.DSCInitializationSpec) feature.ContextEntry[string] {
		return feature.ContextEntry[string]{
			Key: authProviderNameKey,
			Value: func(_ context.Context, _ client.Client) (string, error) {
				return "authorino", nil
			},
		}
	},
	From: feature.ExtractEntry[string](authProviderNameKey),
}

var authExtensionName = feature.ContextDefinition[dsciv1.DSCInitializationSpec, string]{
	Create: func(source *dsciv1.DSCInitializationSpec) feature.ContextEntry[string] {
		return feature.ContextEntry[string]{
			Key: authExtensionNameKey,
			Value: func(_ context.Context, _ client.Client) (string, error) {
				return source.ApplicationsNamespace + "-auth-provider", nil
			},
		}
	},
	From: feature.ExtractEntry[string](authExtensionNameKey),
}
