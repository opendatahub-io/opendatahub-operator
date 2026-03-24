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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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
func initialize(_ context.Context, rr *types.ReconciliationRequest) error { //nolint:unparam
	rr.Manifests = []types.ManifestInfo{
		baseManifestInfo(BaseManifestsSourcePath),
	}

	return nil
}

// customizeManifests applies component-specific customizations to the manifests.
func customizeManifests(ctx context.Context, rr *types.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	maas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}

	appNamespace, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return err
	}

	params := map[string]string{
		"gateway-namespace": maas.Spec.GatewayRef.Namespace,
		"gateway-name":      maas.Spec.GatewayRef.Name,
		"app-namespace":     appNamespace,
	}

	// Add API key configuration if specified
	if maas.Spec.APIKeys != nil && maas.Spec.APIKeys.MaxExpirationDays != nil {
		params["api-key-max-expiration-days"] = strconv.FormatInt(int64(*maas.Spec.APIKeys.MaxExpirationDays), 10)
		log.V(4).Info("Configuring API key max expiration days", "value", *maas.Spec.APIKeys.MaxExpirationDays)
	}

	if err := odhdeploy.ApplyParams(rr.Manifests[0].String(), "params.env", nil, params); err != nil {
		return fmt.Errorf("failed to update params on path %s: %w", rr.Manifests[0].String(), err)
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

// configureTelemetryPolicy is a post-render action that creates a TelemetryPolicy
// resource based on the ModelsAsService telemetry configuration.
//
// The TelemetryPolicy is generated programmatically (not from manifests) because
// its content is entirely dynamic based on the spec.telemetry.metrics configuration.
func configureTelemetryPolicy(ctx context.Context, rr *types.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	maas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}

	gatewayNamespace := maas.Spec.GatewayRef.Namespace
	gatewayName := maas.Spec.GatewayRef.Name

	// Build the labels map based on telemetry configuration
	metricLabels := buildTelemetryLabels(log, maas.Spec.Telemetry)

	// Create OwnerReference for the TelemetryPolicy
	controller := true
	ownerRef := metav1.OwnerReference{
		APIVersion:         maas.APIVersion,
		Kind:               maas.Kind,
		Name:               maas.Name,
		UID:                maas.UID,
		Controller:         &controller,
		BlockOwnerDeletion: &controller,
	}

	// Create the TelemetryPolicy resource
	telemetryPolicy := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "extensions.kuadrant.io/v1alpha1",
			"kind":       "TelemetryPolicy",
			"metadata": map[string]interface{}{
				"name":      TelemetryPolicyName,
				"namespace": gatewayNamespace,
				"labels": map[string]interface{}{
					"app.kubernetes.io/part-of": "maas-observability",
				},
			},
			"spec": map[string]interface{}{
				"targetRef": map[string]interface{}{
					"group": "gateway.networking.k8s.io",
					"kind":  "Gateway",
					"name":  gatewayName,
				},
				"metrics": map[string]interface{}{
					"default": map[string]interface{}{
						"labels": metricLabels,
					},
				},
			},
		},
	}

	// Set OwnerReferences using the unstructured API
	telemetryPolicy.SetOwnerReferences([]metav1.OwnerReference{ownerRef})

	log.V(2).Info("Creating TelemetryPolicy",
		"name", TelemetryPolicyName,
		"namespace", gatewayNamespace,
		"targetGateway", gatewayName,
		"labels", metricLabels)

	// Add to resources for deployment
	rr.Resources = append(rr.Resources, *telemetryPolicy)

	return nil
}

// buildTelemetryLabels creates the metric labels map based on the telemetry configuration.
// Always-on dimensions (subscription, cost_center, tier) are always included for billing and access control.
// Other dimensions are configurable based on MetricsConfig settings.
func buildTelemetryLabels(log logr.Logger, config *componentApi.TelemetryConfig) map[string]interface{} {
	// Default values when config is nil or metrics is nil
	captureOrganization := true
	captureUser := false // Disabled by default for privacy/GDPR compliance
	captureGroup := false
	captureModelUsage := true

	if config != nil && config.Metrics != nil {
		metrics := config.Metrics
		if metrics.CaptureOrganization != nil {
			captureOrganization = *metrics.CaptureOrganization
		}
		if metrics.CaptureUser != nil {
			captureUser = *metrics.CaptureUser
		}
		if metrics.CaptureGroup != nil {
			captureGroup = *metrics.CaptureGroup
		}
		if metrics.CaptureModelUsage != nil {
			captureModelUsage = *metrics.CaptureModelUsage
		}
	}

	// Always-on dimensions - essential for billing and access control
	labels := map[string]interface{}{
		"subscription": "auth.identity.selected_subscription",
		"cost_center":  "auth.identity.costCenter",
		"tier":         "auth.identity.tier",
	}

	// Configurable dimensions
	if captureOrganization {
		labels["organization_id"] = "auth.identity.organizationId"
	}

	if captureUser {
		log.Info("WARNING: User identity metrics enabled - ensure GDPR/privacy compliance",
			"field", "captureUser", "value", true)
		labels["user"] = "auth.identity.userid"
	}

	if captureGroup {
		labels["group"] = "auth.identity.group"
	}

	if captureModelUsage {
		labels["model"] = "responseBodyJSON(\"/model\")"
	}

	log.V(4).Info("Built telemetry labels",
		"captureOrganization", captureOrganization,
		"captureUser", captureUser,
		"captureGroup", captureGroup,
		"captureModelUsage", captureModelUsage,
		"totalLabels", len(labels))

	return labels
}

// configureConfigHashAnnotation adds a hash annotation to the maas-api Deployment
// to trigger rolling restarts when the ConfigMap changes.
// This is necessary because env vars sourced via valueFrom.configMapKeyRef
// do not automatically trigger pod restarts when the ConfigMap is updated.
func configureConfigHashAnnotation(ctx context.Context, rr *types.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	// Find the maas-parameters ConfigMap
	var configMap *corev1.ConfigMap
	for idx := range rr.Resources {
		resource := &rr.Resources[idx]
		if resource.GroupVersionKind() == gvk.ConfigMap && resource.GetName() == MaaSParametersConfigMapName {
			cm := &corev1.ConfigMap{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, cm); err != nil {
				return fmt.Errorf("failed to convert ConfigMap: %w", err)
			}
			configMap = cm
			break
		}
	}

	if configMap == nil {
		log.V(1).Info("ConfigMap not found in rendered resources, skipping config hash annotation",
			"expectedName", MaaSParametersConfigMapName)
		return nil
	}

	// Compute hash of the ConfigMap data
	configHash := hashConfigMapData(configMap.Data)
	log.V(4).Info("Computed ConfigMap hash", "hash", configHash, "configMap", configMap.Name)

	// Find the maas-api Deployment
	var deployment *appsv1.Deployment
	var deploymentIdx int
	for idx := range rr.Resources {
		resource := &rr.Resources[idx]
		if resource.GroupVersionKind() == gvk.Deployment && resource.GetName() == MaaSAPIDeploymentName {
			dep := &appsv1.Deployment{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, dep); err != nil {
				return fmt.Errorf("failed to convert Deployment: %w", err)
			}
			deployment = dep
			deploymentIdx = idx
			break
		}
	}

	if deployment == nil {
		log.V(1).Info("Deployment not found in rendered resources, skipping config hash annotation",
			"expectedName", MaaSAPIDeploymentName)
		return nil
	}

	// Initialize annotations map if nil
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}

	// Add the config hash annotation to the pod template
	annotationKey := labels.ODHAppPrefix + "/maas-config-hash"
	deployment.Spec.Template.Annotations[annotationKey] = configHash

	log.V(4).Info("Added config hash annotation to Deployment",
		"deployment", deployment.Name,
		"annotation", annotationKey,
		"hash", configHash)

	// Convert back to unstructured and update in resources
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deployment)
	if err != nil {
		return fmt.Errorf("failed to convert Deployment back to unstructured: %w", err)
	}
	rr.Resources[deploymentIdx].Object = u

	return nil
}

// hashConfigMapData computes a SHA256 hash of the ConfigMap data.
// The hash is computed from sorted key-value pairs to ensure consistency.
func hashConfigMapData(data map[string]string) string {
	// Sort keys for consistent hashing
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build a string representation of the data
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(data[k])
		sb.WriteString("\n")
	}

	// Compute SHA256 hash
	hash := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(hash[:])
}
