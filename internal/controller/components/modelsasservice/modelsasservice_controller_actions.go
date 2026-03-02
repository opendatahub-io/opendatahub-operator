/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package modelsasservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// validateGateway validates the Gateway specification in the ModelsAsService resource.
// It checks that:
// 1. Both namespace and name are provided (or neither, in which case defaults are used).
// 2. The specified Gateway resource exists in the cluster.
func validateGateway(ctx context.Context, rr *types.ReconciliationRequest) error {
	maas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}

	// When the Gateway is omitted, use defaults
	if maas.Spec.GatewayRef.Namespace == "" && maas.Spec.GatewayRef.Name == "" {
		maas.Spec.GatewayRef.Namespace = DefaultGatewayNamespace
		maas.Spec.GatewayRef.Name = DefaultGatewayName
	}

	// If one field of the Gateway reference is specified, both are mandatory
	if maas.Spec.GatewayRef.Namespace == "" || maas.Spec.GatewayRef.Name == "" {
		return errors.New("invalid gateway specification: when specifying a custom gateway, both namespace and name must be provided")
	}

	// Validate that the Gateway exists in the cluster
	if err := validateGatewayExists(ctx, rr, maas.Spec.GatewayRef.Namespace, maas.Spec.GatewayRef.Name); err != nil {
		return err
	}

	return nil
}

// validateGatewayExists checks if a Gateway resource exists in the specified namespace.
func validateGatewayExists(ctx context.Context, rr *types.ReconciliationRequest, namespace, name string) error {
	gateway := &gwapiv1.Gateway{}
	err := rr.Client.Get(ctx, k8stypes.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, gateway)

	if err != nil {
		if k8serr.IsNotFound(err) {
			return fmt.Errorf("gateway %s/%s not found: the specified Gateway must exist before enabling ModelsAsService", namespace, name)
		}
		return fmt.Errorf("failed to check if gateway %s/%s exists: %w", namespace, name, err)
	}

	return nil
}

// initialize sets up the manifests for the ModelsAsService component.
func initialize(_ context.Context, rr *types.ReconciliationRequest) error {
	rr.Manifests = []types.ManifestInfo{
		baseManifestInfo(BaseManifestsSourcePath),
	}

	return nil
}

// customizeManifests applies component-specific customizations to the manifests.
func customizeManifests(ctx context.Context, rr *types.ReconciliationRequest) error {
	maas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}

	appNamespace, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return err
	}

	gatewayParams := map[string]string{
		"gateway-namespace": maas.Spec.GatewayRef.Namespace,
		"gateway-name":      maas.Spec.GatewayRef.Name,
		"app-namespace":     appNamespace,
	}

	if err := odhdeploy.ApplyParams(rr.Manifests[0].String(), "params.env", nil, gatewayParams); err != nil {
		return fmt.Errorf("failed to update Gateway params on path %s: %w", rr.Manifests[0].String(), err)
	}

	return nil
}

// configureGatewayNamespaceResources is a post-render action that configures resources
// that must be deployed to the gateway's namespace.
//
// For AuthPolicy:
// 1. Sets the namespace to match the gateway's namespace (AuthPolicy must be in the same namespace as the gateway).
// 2. Updates spec.targetRef.name to point to the configured gateway name.
//
// For DestinationRule:
// 1. Sets the namespace to match the gateway's namespace (DestinationRule must be in the same namespace as the gateway).
func configureGatewayNamespaceResources(ctx context.Context, rr *types.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	maas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}

	gatewayNamespace := maas.Spec.GatewayRef.Namespace
	gatewayName := maas.Spec.GatewayRef.Name

	log.V(4).Info("Gateway configuration from MaaS spec",
		"gatewayNamespace", gatewayNamespace,
		"gatewayName", gatewayName)

	authPolicyFound := false
	destinationRuleFound := false

	for idx := range rr.Resources {
		resource := &rr.Resources[idx]
		resourceGVK := resource.GroupVersionKind()

		switch {
		case resourceGVK == gvk.AuthPolicyv1 && resource.GetName() == GatewayAuthPolicyName:
			authPolicyFound = true
			if err := configureAuthPolicy(log, resource, gatewayNamespace, gatewayName); err != nil {
				return err
			}

		case resourceGVK == gvk.DestinationRule && resource.GetName() == GatewayDestinationRuleName:
			destinationRuleFound = true
			configureDestinationRule(log, resource, gatewayNamespace)
		}
	}

	if !authPolicyFound {
		log.V(1).Info("AuthPolicy not found in rendered resources",
			"expectedName", GatewayAuthPolicyName,
			"expectedGVK", gvk.AuthPolicyv1.String())
	}

	if !destinationRuleFound {
		log.V(1).Info("DestinationRule not found in rendered resources",
			"expectedName", GatewayDestinationRuleName,
			"expectedGVK", gvk.DestinationRule.String())
	}

	return nil
}

// configureAuthPolicy updates the AuthPolicy resource to use the correct gateway namespace and name.
func configureAuthPolicy(log logr.Logger, resource *unstructured.Unstructured, gatewayNamespace, gatewayName string) error {
	log.V(4).Info("Configuring AuthPolicy",
		"name", resource.GetName(),
		"originalNamespace", resource.GetNamespace(),
		"newNamespace", gatewayNamespace,
		"newTargetGateway", gatewayName)

	resource.SetNamespace(gatewayNamespace)

	if err := unstructured.SetNestedField(resource.Object, gatewayName, "spec", "targetRef", "name"); err != nil {
		return fmt.Errorf("failed to set spec.targetRef.name on AuthPolicy: %w", err)
	}

	return nil
}

// configureDestinationRule updates the DestinationRule resource to use the correct gateway namespace.
func configureDestinationRule(log logr.Logger, resource *unstructured.Unstructured, gatewayNamespace string) {
	log.V(4).Info("Configuring DestinationRule",
		"name", resource.GetName(),
		"originalNamespace", resource.GetNamespace(),
		"newNamespace", gatewayNamespace)

	resource.SetNamespace(gatewayNamespace)
}

// manageMaasApiRBAC adds a ClusterRole and ClusterRoleBinding so the maas-api ServiceAccount
// can list/get/watch maassubscriptions and maasmodels (maas.opendatahub.io) at cluster scope.
// This fixes RBAC when the MaaS manifests do not grant these permissions.
func manageMaasApiRBAC(ctx context.Context, rr *types.ReconciliationRequest) error {
	appNamespace, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return err
	}

	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: MaasApiClusterRoleName,
			Labels: map[string]string{
				labels.ODH.Component(ComponentName): labels.True,
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{MaasCRDAPIGroup},
				Resources: []string{"maassubscriptions", "maasmodels"},
				Verbs:     []string{"list", "get", "watch"},
			},
		},
	}

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: MaasApiClusterRoleBindingName,
			Labels: map[string]string{
				labels.ODH.Component(ComponentName): labels.True,
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Namespace: appNamespace,
				Name:      MaasApiServiceAccountName,
				APIGroup:  "",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: gvk.ClusterRole.Group,
			Kind:     gvk.ClusterRole.Kind,
			Name:     MaasApiClusterRoleName,
		},
	}

	if err := rr.AddResources(cr, crb); err != nil {
		return fmt.Errorf("failed to add maas-api RBAC resources: %w", err)
	}
	return nil
}
