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
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// validatePrerequisites checks that required platform prerequisites are present and
// surfaces clear warnings when they are missing. Checks are either blocking (error
// severity, affects Ready state) or non-blocking (info severity, visible but does
// not prevent reconciliation).
func validatePrerequisites(ctx context.Context, rr *types.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	_, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}

	var warnings []string
	var errors []string

	// Check 1: Kuadrant/RHCL stack (AuthConfig CRD from Authorino)
	if msg := checkKuadrantAvailable(ctx, rr); msg != "" {
		warnings = append(warnings, msg)
		log.V(1).Info("Prerequisite warning", "check", "kuadrant", "message", msg)
	}

	// Check 2: Authorino TLS configuration
	if msg := checkAuthorinoTLS(ctx, rr); msg != "" {
		warnings = append(warnings, msg)
		log.V(1).Info("Prerequisite warning", "check", "authorino-tls", "message", msg)
	}

	// Check 3: Database Secret (blocking — maas-api cannot start without it)
	if msg := checkDatabaseSecret(ctx, rr); msg != "" {
		errors = append(errors, msg)
		log.Info("Prerequisite error", "check", "database-secret", "message", msg)
	}

	// Check 4: User Workload Monitoring
	if msg := checkUserWorkloadMonitoring(ctx, rr); msg != "" {
		warnings = append(warnings, msg)
		log.V(1).Info("Prerequisite warning", "check", "user-workload-monitoring", "message", msg)
	}

	// Check 5: Kuadrant monitoring (TelemetryPolicy CRD)
	if msg := checkKuadrantMonitoring(ctx, rr); msg != "" {
		warnings = append(warnings, msg)
		log.V(1).Info("Prerequisite warning", "check", "kuadrant-monitoring", "message", msg)
	}

	// If there are blocking errors, set condition with Error severity (affects Ready state)
	if len(errors) > 0 {
		allMessages := append(errors, warnings...) //nolint:gocritic // intentional append to new slice
		aggregatedMessage := strings.Join(allMessages, "; ")

		rr.Conditions.MarkFalse(
			status.ConditionPrerequisitesAvailable,
			conditions.WithReason("PrerequisitesMissing"),
			conditions.WithMessage("%s", aggregatedMessage),
		)

		return nil
	}

	// If there are only warnings, set Info severity (does not affect Ready state)
	if len(warnings) > 0 {
		aggregatedMessage := strings.Join(warnings, "; ")

		rr.Conditions.MarkFalse(
			status.ConditionPrerequisitesAvailable,
			conditions.WithReason("PrerequisitesWarning"),
			conditions.WithMessage("%s", aggregatedMessage),
			conditions.WithSeverity(common.ConditionSeverityInfo),
		)

		return nil
	}

	rr.Conditions.MarkTrue(status.ConditionPrerequisitesAvailable)

	return nil
}

// checkKuadrantAvailable verifies the Kuadrant/RHCL stack is installed by checking for the AuthConfig CRD.
func checkKuadrantAvailable(ctx context.Context, rr *types.ReconciliationRequest) string {
	has, err := cluster.HasCRD(ctx, rr.Client, gvk.AuthConfigv1beta3)
	if err != nil {
		return fmt.Sprintf("failed to check Kuadrant/RHCL availability: %v", err)
	}
	if !has {
		return "Kuadrant/RHCL stack not installed: AuthConfig CRD (authorino.kuadrant.io) not found. " +
			"Install the Kuadrant operator to enable authentication and authorization"
	}
	return ""
}

// checkAuthorinoTLS verifies that at least one Authorino instance has TLS enabled
// on its listener by inspecting spec.listener.tls.enabled and spec.listener.tls.certSecretRef.
// If the Authorino CRD is not installed, the check is skipped (Kuadrant check covers that).
// If multiple Authorino instances exist, the check passes if any one has TLS enabled.
func checkAuthorinoTLS(ctx context.Context, rr *types.ReconciliationRequest) string {
	// First check if the Authorino CRD exists
	has, err := cluster.HasCRD(ctx, rr.Client, gvk.Authorinov1beta2)
	if err != nil {
		return fmt.Sprintf("failed to check Authorino CRD availability: %v", err)
	}
	if !has {
		// Authorino CRD not present — skip TLS check (Kuadrant check handles the dependency)
		return ""
	}

	// List all Authorino instances across namespaces
	authorinoList := &unstructured.UnstructuredList{}
	authorinoList.SetGroupVersionKind(gvk.Authorinov1beta2)
	if err := rr.Client.List(ctx, authorinoList, &client.ListOptions{}); err != nil {
		return fmt.Sprintf("failed to list Authorino instances: %v", err)
	}

	if len(authorinoList.Items) == 0 {
		return "no Authorino instances found. " +
			"Authorino must be deployed and configured with TLS for MaaS authentication"
	}

	// Check if any Authorino instance has TLS enabled
	for i := range authorinoList.Items {
		item := &authorinoList.Items[i]
		enabled, _, _ := unstructured.NestedBool(item.Object, "spec", "listener", "tls", "enabled")
		certName, _, _ := unstructured.NestedString(item.Object, "spec", "listener", "tls", "certSecretRef", "name")
		if enabled && certName != "" {
			return ""
		}
	}

	return "Authorino TLS is not configured: no Authorino instance has listener.tls.enabled=true with a certSecretRef. " +
		"Patch Authorino with spec.listener.tls.enabled=true and spec.listener.tls.certSecretRef to enable TLS. " +
		"See https://docs.kuadrant.io/1.0.x/authorino/docs/user-guides/mtls-authentication/"
}

// checkDatabaseSecret validates that the required database connection Secret exists
// in the application namespace with the expected key. This is a blocking check —
// maas-api cannot start without a valid database connection.
func checkDatabaseSecret(ctx context.Context, rr *types.ReconciliationRequest) string {
	appNamespace, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return fmt.Sprintf("failed to determine application namespace: %v", err)
	}

	secret := &corev1.Secret{}
	err = rr.Client.Get(ctx, k8stypes.NamespacedName{
		Namespace: appNamespace,
		Name:      MaaSDBSecretName,
	}, secret)

	if err != nil {
		if k8serr.IsNotFound(err) {
			return fmt.Sprintf("database Secret '%s' not found in namespace '%s'. "+
				"Create the Secret with key '%s' containing the PostgreSQL connection URL. "+
				"MaaS API cannot start without a database connection",
				MaaSDBSecretName, appNamespace, MaaSDBSecretKey)
		}
		return fmt.Sprintf("failed to check database Secret '%s' in namespace '%s': %v",
			MaaSDBSecretName, appNamespace, err)
	}

	if _, ok := secret.Data[MaaSDBSecretKey]; !ok {
		return fmt.Sprintf("database Secret '%s' in namespace '%s' is missing required key '%s'. "+
			"The Secret must contain a valid PostgreSQL connection URL",
			MaaSDBSecretName, appNamespace, MaaSDBSecretKey)
	}

	return ""
}

// checkUserWorkloadMonitoring checks if User Workload Monitoring is enabled by inspecting
// the cluster-monitoring-config ConfigMap in the openshift-monitoring namespace.
func checkUserWorkloadMonitoring(ctx context.Context, rr *types.ReconciliationRequest) string {
	cm := &corev1.ConfigMap{}
	err := rr.Client.Get(ctx, k8stypes.NamespacedName{
		Namespace: MonitoringNamespace,
		Name:      ClusterMonitoringConfigName,
	}, cm)

	if err != nil {
		if k8serr.IsNotFound(err) {
			return "User Workload Monitoring not configured: ConfigMap 'cluster-monitoring-config' not found in 'openshift-monitoring'. " +
				"Showback/FinOps usage views will not work without User Workload Monitoring enabled"
		}
		// Access errors (e.g., RBAC) should not block — log and continue
		return fmt.Sprintf("unable to verify User Workload Monitoring status: %v. "+
			"Ensure User Workload Monitoring is enabled for showback functionality", err)
	}

	configData, ok := cm.Data["config.yaml"]
	if !ok {
		return "User Workload Monitoring is not enabled. " +
			"Set enableUserWorkload: true in 'cluster-monitoring-config' ConfigMap in 'openshift-monitoring' namespace. " +
			"Showback/FinOps usage views will not work without it"
	}

	var cfg struct {
		EnableUserWorkload bool `yaml:"enableUserWorkload"`
	}
	if err := yaml.Unmarshal([]byte(configData), &cfg); err != nil {
		return "User Workload Monitoring config is invalid in 'cluster-monitoring-config'. " +
			"Ensure config.yaml is valid YAML and sets enableUserWorkload: true"
	}

	if !cfg.EnableUserWorkload {
		return "User Workload Monitoring is not enabled. " +
			"Set enableUserWorkload: true in 'cluster-monitoring-config' ConfigMap in 'openshift-monitoring' namespace. " +
			"Showback/FinOps usage views will not work without it"
	}

	return ""
}

// checkKuadrantMonitoring checks if Kuadrant monitoring is available by verifying
// the TelemetryPolicy CRD is present.
func checkKuadrantMonitoring(ctx context.Context, rr *types.ReconciliationRequest) string {
	has, err := cluster.HasCRD(ctx, rr.Client, gvk.TelemetryPolicyv1alpha1)
	if err != nil {
		return fmt.Sprintf("failed to check Kuadrant monitoring availability: %v", err)
	}
	if !has {
		return "Kuadrant monitoring not available: TelemetryPolicy CRD (extensions.kuadrant.io) not found. " +
			"Showback/FinOps usage views will not work without Kuadrant monitoring enabled"
	}
	return ""
}
