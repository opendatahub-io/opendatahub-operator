/*
Copyright 2026.

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
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/api/resource"
	kyaml "sigs.k8s.io/kustomize/kyaml/yaml"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
)

const (
	ComponentName = componentApi.ModelsAsServiceComponentName

	ReadyConditionType = componentApi.ModelsAsServiceKind + status.ReadySuffix

	// Default Gateway values as specified in the spec.
	DefaultGatewayNamespace = "openshift-ingress"
	DefaultGatewayName      = "maas-default-gateway"

	// MaaSSubscriptionNamespace is the namespace where MaaS CRs live
	// (MaasTenantConfig, MaaSSubscription, MaaSAuthPolicy). Must match the
	// maas-controller --maas-subscription-namespace flag.
	MaaSSubscriptionNamespace = "models-as-a-service"

	// MaasControllerDeploymentName is the maas-controller workload Deployment name in the
	// application namespace (must match upstream manager kustomize).
	MaasControllerDeploymentName = "maas-controller"

	// Manifest paths.
	BaseManifestsSourcePath = "overlays/odh"

	// MaasManifestContextDir is the kustomize bundle directory under ManifestsBasePath.
	MaasManifestContextDir = "maas"

	// MaasClusterConfigName is the cluster-scoped maas Config singleton name (must match
	// maas-controller; see models-as-a-service lifecycle).
	MaasClusterConfigName = "default"

	// MaasTenantConfigInstanceName is the singleton MaasTenantConfig resource name enforced by the API.
	MaasTenantConfigInstanceName = "default-tenant"
)


// payloadProcessingNetworkPolicy returns a NetworkPolicy for the
// payload-processing pod in the gateway namespace. OCP 4.22 introduced a
// deny-all NetworkPolicy in openshift-ingress; without explicit rules the pod
// cannot reach the Kubernetes API server (egress) or receive ext_proc calls
// from the gateway (ingress).
func payloadProcessingNetworkPolicy(componentLabels map[string]string) unstructured.Unstructured {
	npLabels := make(map[string]any, len(componentLabels)+1)
	for k, v := range componentLabels {
		npLabels[k] = v
	}
	npLabels["app"] = "payload-processing"

	return unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "networking.k8s.io/v1",
			"kind":       "NetworkPolicy",
			"metadata": map[string]any{
				"name":      "payload-processing",
				"namespace": DefaultGatewayNamespace,
				"labels":    npLabels,
			},
			"spec": map[string]any{
				"podSelector": map[string]any{
					"matchLabels": map[string]any{
						"app": "payload-processing",
					},
				},
				"policyTypes": []any{"Ingress", "Egress"},
				"ingress": []any{
					map[string]any{
						"from": []any{
							map[string]any{
								"podSelector": map[string]any{
									"matchLabels": map[string]any{
										"gateway.networking.k8s.io/gateway-name": "data-science-gateway",
									},
								},
								"namespaceSelector": map[string]any{
									"matchLabels": map[string]any{
										"kubernetes.io/metadata.name": DefaultGatewayNamespace,
									},
								},
							},
						},
						"ports": []any{
							map[string]any{
								"protocol": "TCP",
								"port":     int64(9004),
							},
						},
					},
					map[string]any{
						"from": []any{
							map[string]any{
								"namespaceSelector": map[string]any{
									"matchLabels": map[string]any{
										"kubernetes.io/metadata.name": "openshift-monitoring",
									},
								},
							},
							map[string]any{
								"namespaceSelector": map[string]any{
									"matchLabels": map[string]any{
										"kubernetes.io/metadata.name": "openshift-user-workload-monitoring",
									},
								},
							},
						},
						"ports": []any{
							map[string]any{
								"protocol": "TCP",
								"port":     int64(9005),
							},
							map[string]any{
								"protocol": "TCP",
								"port":     int64(9090),
							},
						},
					},
				},
				"egress": []any{map[string]any{}},
			},
		},
	}
}

// Image parameter mappings for MaaS manifest substitution (informational — no longer
// applied by this package since MaaS moved to the AIGateway module handler):
//
//	"maas-controller-image":      RELATED_IMAGE_ODH_MAAS_CONTROLLER_IMAGE
//	"maas-api-image":             RELATED_IMAGE_ODH_MAAS_API_IMAGE
//	"payload-processing-image":   RELATED_IMAGE_ODH_AI_GATEWAY_PAYLOAD_PROCESSING_IMAGE
//	"maas-api-key-cleanup-image": RELATED_IMAGE_UBI_MINIMAL_IMAGE

type resourceKey struct {
	kind string
	name string
}

// gatewayNamespaceResources lists the specific resources that must remain in the
// gateway namespace after the blanket NamespaceApplierPlugin transform. Keyed by
// kind+name to avoid accidentally matching unrelated resources.
var gatewayNamespaceResources = map[resourceKey]bool{
	{kind: "Deployment", name: "payload-processing"}:                true,
	{kind: "Service", name: "payload-processing"}:                   true,
	{kind: "ServiceAccount", name: "payload-processing"}:            true,
	{kind: "ConfigMap", name: "payload-processing-plugins"}:         true,
	{kind: "NetworkPolicy", name: "payload-processing"}:             true,
	{kind: "ClusterRoleBinding", name: "payload-processing-reader"}: true,
}

// restoreGatewayNamespaceResources moves resources that belong in the gateway
// namespace back from the application namespace. The blanket NamespaceApplierPlugin
// moves everything to appNs, but payload-processing resources must stay in the
// gateway namespace for the EnvoyFilter to attach to the Gateway.
//
// For ClusterRoleBindings, the NamespaceApplierPlugin also rewrites
// subjects[].namespace; this function restores it to the gateway namespace
// so the RBAC binding matches the actual ServiceAccount location.
func restoreGatewayNamespaceResources(resMap resmap.ResMap) error {
	for _, res := range resMap.Resources() {
		k := resourceKey{kind: res.GetKind(), name: res.GetName()}
		if !gatewayNamespaceResources[k] {
			continue
		}
		if res.GetKind() == "ClusterRoleBinding" {
			if err := restoreCRBSubjectsNamespace(res, DefaultGatewayNamespace); err != nil {
				return fmt.Errorf("restore subjects namespace on ClusterRoleBinding %s: %w", res.GetName(), err)
			}
			continue
		}
		if err := res.SetNamespace(DefaultGatewayNamespace); err != nil {
			return fmt.Errorf("set namespace on %s %s: %w", res.GetKind(), res.GetName(), err)
		}
	}
	return nil
}

// restoreCRBSubjectsNamespace updates subjects[].namespace on a ClusterRoleBinding
// resource. SetNamespace only changes metadata.namespace which is meaningless for
// cluster-scoped resources; the NamespaceApplierPlugin rewrites subjects separately.
func restoreCRBSubjectsNamespace(res *resource.Resource, namespace string) error {
	m, err := res.Map()
	if err != nil {
		return err
	}
	subjects, ok := m["subjects"].([]any)
	if !ok {
		return nil
	}
	for _, s := range subjects {
		if subject, ok := s.(map[string]any); ok {
			subject["namespace"] = namespace
		}
	}
	node, err := kyaml.FromMap(m)
	if err != nil {
		return err
	}
	res.RNode = *node
	return nil
}
