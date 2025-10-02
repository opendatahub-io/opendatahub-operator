package auth

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	ServiceName = serviceApi.AuthServiceName
)

// TODO: deprecate this function in favor of IsIntegratedOAuth()
// IsDefaultAuthMethod returns true if the default authentication method is IntegratedOAuth or empty.
// This will give indication that Operator should create userGroups or not in the cluster.
func IsDefaultAuthMethod(ctx context.Context, cli client.Client) (bool, error) {
	authenticationobj := &configv1.Authentication{}
	if err := cli.Get(ctx, client.ObjectKey{Name: cluster.ClusterAuthenticationObj, Namespace: ""}, authenticationobj); err != nil {
		if meta.IsNoMatchError(err) { // when CRD is missing, convert error type
			return false, k8serr.NewNotFound(schema.GroupResource{Group: gvk.Auth.Group}, cluster.ClusterAuthenticationObj)
		}
		return false, err
	}

	// for now, HPC support "" "None" "IntegratedOAuth"(default) "OIDC"
	// other offering support "" "None" "IntegratedOAuth"(default)
	// we only create userGroups for "IntegratedOAuth" or "" and leave other or new supported type value in the future
	return authenticationobj.Spec.Type == configv1.AuthenticationTypeIntegratedOAuth || authenticationobj.Spec.Type == "", nil
}
