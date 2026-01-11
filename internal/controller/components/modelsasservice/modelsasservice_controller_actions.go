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
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/runtime"
	types2 "k8s.io/apimachinery/pkg/types"
	v1 "sigs.k8s.io/gateway-api/apis/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// validateGateway validates the Gateway specification in the ModelsAsService resource.
func validateGateway(_ context.Context, rr *types.ReconciliationRequest) error {
	maas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}

	// When the Gateway is omitted, use defaults
	if maas.Spec.Gateway.Namespace == "" && maas.Spec.Gateway.Name == "" {
		maas.Spec.Gateway.Namespace = DefaultGatewayNamespace
		maas.Spec.Gateway.Name = DefaultGatewayName
		return nil
	}

	// If one field of the Gateway reference is specified, both are mandatory
	if maas.Spec.Gateway.Namespace == "" || maas.Spec.Gateway.Name == "" {
		return errors.New("invalid gateway specification: when specifying a custom gateway, both namespace and name must be provided")
	}

	// TODO: Add validation logic to check if the specified Gateway exists
	// (For now, we'll just validate that the name and namespace are set)

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

// TODO: Remove this function. We are not expecting to programatically create the Gateway. Users would create it.
func configureMaaSGatewayHostname(ctx context.Context, rr *types.ReconciliationRequest) error {
	for idx, resource := range rr.Resources {
		if resource.GroupVersionKind() == gvk.KubernetesGateway {
			gateway := &v1.Gateway{}
			fromUnstructuredErr := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, gateway)
			if fromUnstructuredErr != nil {
				return fmt.Errorf("failed converting to Gateway type from resource %s: %w", resources.FormatObjectReference(&resource), fromUnstructuredErr)
			}

			clusterIngress := &configv1.Ingress{}
			ingressFetchErr := rr.Client.Get(ctx, types2.NamespacedName{Namespace: "", Name: "cluster"}, clusterIngress)
			if ingressFetchErr != nil {
				return fmt.Errorf("failed fetching OpenShift cluster ingress resource: %w", ingressFetchErr)
			}

			for idxListener := range gateway.Spec.Listeners {
				if gateway.Spec.Listeners[idxListener].Hostname != nil {
					hostnameTemplate := string(*gateway.Spec.Listeners[idxListener].Hostname)
					finalHostname := v1.Hostname(strings.Replace(hostnameTemplate, "${CLUSTER_DOMAIN}", clusterIngress.Spec.Domain, 1))
					gateway.Spec.Listeners[idxListener].Hostname = &finalHostname
				}
			}

			unstructuredGw, toUnstructuredErr := resources.ToUnstructured(gateway)
			if toUnstructuredErr != nil {
				return fmt.Errorf("failed converting Gateway resource to unstructured object: %w", toUnstructuredErr)
			}

			rr.Resources[idx] = *unstructuredGw
		}
	}

	return nil
}
