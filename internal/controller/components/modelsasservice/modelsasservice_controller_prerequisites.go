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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// checkDependencies uses the dependency action framework to monitor CRD availability.
// This sets the DependenciesAvailable condition based on CRD presence.
func checkDependencies() actions.Fn {
	return dependency.NewAction(
		// Kuadrant/RHCL stack — required for gateway authentication
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK: gvk.AuthConfigv1beta3,
		}),
		// Kuadrant monitoring — optional, only affects showback/FinOps views
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:      gvk.TelemetryPolicyv1alpha1,
			Severity: common.ConditionSeverityInfo,
		}),
	)
}

// validatePrerequisites checks platform prerequisites that require custom validation
// beyond simple CRD presence checks (which are handled by checkDependencies).
// Checks are either blocking (error severity, affects Ready state) or non-blocking
// (info severity, visible but does not prevent reconciliation).
func validatePrerequisites(ctx context.Context, rr *types.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	_, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}

	var warnings []string
	var errors []string

	// Check 1: Authorino TLS configuration (non-blocking — RHCL is a separate operator and
	// customers may configure Authorino in ways we don't detect; a false positive here
	// should not block the DSC from being marked as Ready)
	if msg := checkAuthorinoTLS(ctx, rr); msg != "" {
		warnings = append(warnings, msg)
		log.V(1).Info("MaaS prerequisite warning", "check", "authorino-tls", "message", msg)
	}

	// Check 2: Database Secret (blocking — maas-api cannot start without it)
	if msg := checkDatabaseSecret(ctx, rr); msg != "" {
		errors = append(errors, msg)
		log.Error(nil, "MaaS prerequisite error", "check", "database-secret", "message", msg)
	}

	// Check 3: User Workload Monitoring (non-blocking — only affects showback views)
	if msg := checkUserWorkloadMonitoring(ctx, rr); msg != "" {
		warnings = append(warnings, msg)
		log.V(1).Info("MaaS prerequisite warning", "check", "user-workload-monitoring", "message", msg)
	}

	// If there are blocking errors, set condition with Error severity (affects Ready state)
	if len(errors) > 0 {
		allMessages := append(errors, warnings...) //nolint:gocritic // intentional append to new slice
		aggregatedMessage := strings.Join(allMessages, "; ")

		rr.Conditions.MarkFalse(
			status.ConditionMaaSPrerequisitesAvailable,
			conditions.WithReason("PrerequisitesMissing"),
			conditions.WithMessage("%s", aggregatedMessage),
		)

		return odherrors.NewStopError("blocking prerequisites missing: %s", aggregatedMessage)
	}

	// If there are only warnings, set Info severity (does not affect Ready state)
	if len(warnings) > 0 {
		aggregatedMessage := strings.Join(warnings, "; ")

		rr.Conditions.MarkFalse(
			status.ConditionMaaSPrerequisitesAvailable,
			conditions.WithReason("PrerequisitesWarning"),
			conditions.WithMessage("%s", aggregatedMessage),
			conditions.WithSeverity(common.ConditionSeverityInfo),
		)

		return nil
	}

	rr.Conditions.MarkTrue(status.ConditionMaaSPrerequisitesAvailable)

	return nil
}

// checkAuthorinoTLS verifies that at least one Authorino instance has TLS enabled
// on its listener by inspecting spec.listener.tls.enabled and spec.listener.tls.certSecretRef.
// If the Authorino CRD is not installed, the check is skipped (Kuadrant check covers that).
// If multiple Authorino instances exist, the check passes if any one has TLS enabled.
func checkAuthorinoTLS(ctx context.Context, rr *types.ReconciliationRequest) string {
	// First check if the Authorino CRD exists
	has, err := cluster.HasCRD(ctx, rr.Client, gvk.Authorinov1beta1)
	if err != nil {
		logf.FromContext(ctx).Error(err, "failed to check Authorino CRD availability")
		return "failed to check Authorino CRD availability due to a cluster API error"
	}
	if !has {
		// Authorino CRD not present — skip TLS check (Kuadrant check handles the dependency)
		return ""
	}

	// List all Authorino instances across namespaces
	authorinoList := &unstructured.UnstructuredList{}
	authorinoList.SetGroupVersionKind(gvk.Authorinov1beta1)
	if err := rr.Client.List(ctx, authorinoList, &client.ListOptions{}); err != nil {
		logf.FromContext(ctx).Error(err, "failed to list Authorino instances")
		return "failed to list Authorino instances due to a cluster API error"
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
		logf.FromContext(ctx).Error(err, "failed to determine application namespace")
		return "failed to determine application namespace due to a cluster API error"
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
		logf.FromContext(ctx).Error(err, "failed to check database Secret", "name", MaaSDBSecretName, "namespace", appNamespace)
		return fmt.Sprintf("failed to check database Secret '%s' in namespace '%s' due to a cluster API error",
			MaaSDBSecretName, appNamespace)
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
		// Access errors (e.g., RBAC) — surface as a warning so the user knows verification failed
		logf.FromContext(ctx).Error(err, "unable to verify User Workload Monitoring status")
		return "unable to verify User Workload Monitoring status due to a cluster API error. " +
			"Ensure User Workload Monitoring is enabled for showback functionality"
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
