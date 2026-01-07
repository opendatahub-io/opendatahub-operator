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

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

// validateGateway validates the Gateway specification in the ModelsAsService resource.
// It checks that:
// 1. Both namespace and name are provided (or neither, in which case defaults are used)
// 2. The specified Gateway resource exists in the cluster
func validateGateway(ctx context.Context, rr *types.ReconciliationRequest) error {
	maas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}

	// When the Gateway is omitted, use defaults
	if maas.Spec.Gateway.Namespace == "" && maas.Spec.Gateway.Name == "" {
		maas.Spec.Gateway.Namespace = DefaultGatewayNamespace
		maas.Spec.Gateway.Name = DefaultGatewayName
	}

	// If one field of the Gateway reference is specified, both are mandatory
	if maas.Spec.Gateway.Namespace == "" || maas.Spec.Gateway.Name == "" {
		return errors.New("invalid gateway specification: when specifying a custom gateway, both namespace and name must be provided")
	}

	// Validate that the Gateway exists in the cluster
	if err := validateGatewayExists(ctx, rr, maas.Spec.Gateway.Namespace, maas.Spec.Gateway.Name); err != nil {
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
func customizeManifests(_ context.Context, rr *types.ReconciliationRequest) error {
	maas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}

	gatewayParams := map[string]string{
		"gateway-namespace": maas.Spec.Gateway.Namespace,
		"gateway-name":      maas.Spec.Gateway.Name,
	}

	if err := odhdeploy.ApplyParams(rr.Manifests[0].String(), "params.env", nil, gatewayParams); err != nil {
		return fmt.Errorf("failed to update Gateway params on path %s: %w", rr.Manifests[0].String(), err)
	}

	return nil
}

// Post Render action that configures the gateway-auth-policy
// 1. Sets the namespace to match the gateway's namespace (since AuthPolicy must be in the same namespace as the gateway)
// 2. Updates spec.targetRef.name to point to the configured gateway name
func configureGatewayAuthPolicy(ctx context.Context, rr *types.ReconciliationRequest) error {
	log := logf.FromContext(ctx)
	log.V(1).Info("Entering configureGatewayAuthPolicy",
		"resourceCount", len(rr.Resources),
		"lookingFor", GatewayAuthPolicyName)

	maas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}

	gatewayNamespace := maas.Spec.Gateway.Namespace
	gatewayName := maas.Spec.Gateway.Name

	log.V(1).Info("Gateway configuration from MaaS spec",
		"gatewayNamespace", gatewayNamespace,
		"gatewayName", gatewayName)

	authPolicyFound := false
	for idx := range rr.Resources {
		resource := &rr.Resources[idx]

		// Only process AuthPolicy resources with the specific name
		if resource.GroupVersionKind() != gvk.AuthPolicyv1 {
			continue
		}

		log.V(1).Info("Found AuthPolicy resource",
			"name", resource.GetName(),
			"namespace", resource.GetNamespace(),
			"expectedName", GatewayAuthPolicyName)

		if resource.GetName() != GatewayAuthPolicyName {
			continue
		}

		authPolicyFound = true
		log.Info("Configuring gateway-auth-policy AuthPolicy",
			"originalNamespace", resource.GetNamespace(),
			"newNamespace", gatewayNamespace,
			"newTargetGateway", gatewayName)

		// Set the namespace to match the gateway's namespace
		resource.SetNamespace(gatewayNamespace)

		// Update spec.targetRef.name to point to the configured gateway
		if err := unstructured.SetNestedField(resource.Object, gatewayName, "spec", "targetRef", "name"); err != nil {
			return fmt.Errorf("failed to set spec.targetRef.name on AuthPolicy: %w", err)
		}

		log.V(1).Info("Successfully updated AuthPolicy",
			"namespace", resource.GetNamespace(),
			"targetRef.name", gatewayName)
	}

	if !authPolicyFound {
		log.V(1).Info("AuthPolicy not found in rendered resources",
			"expectedName", GatewayAuthPolicyName,
			"expectedGVK", gvk.AuthPolicyv1.String())
	}

	return nil
}
