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
	"sort"
	"strings"

	"github.com/go-logr/logr"
	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
)

type componentHandler struct{}

func NewHandler() *componentHandler { return &componentHandler{} }

// GetName returns the component name for ModelsAsService.
func (s *componentHandler) GetName() string {
	return componentApi.ModelsAsServiceComponentName
}

// Init initializes the ModelsAsService component.
func (s *componentHandler) Init(_ common.Platform, cfg operatorconfig.OperatorSettings) error {
	manifestsBasePath := cfg.ManifestsBasePath
	mi := baseManifestInfo(manifestsBasePath, BaseManifestsSourcePath)

	if err := odhdeploy.ApplyParams(mi.String(), "params.env", imagesMap, extraParamsMap); err != nil {
		return fmt.Errorf("failed to update params on path %s: %w", mi, err)
	}

	return nil
}

// NewCRObject returns the cluster-scoped ModelsAsService CR when MaaS is enabled.
// The ModelsAsService component reconciler applies maas-controller install manifests
// with controller ownership on that CR. The DataScienceCluster reconciler only ensures
// this CR exists when MaaS is enabled; it does not apply the install bundle. maas-controller
// continues to own Tenant CR lifecycle; UpdateDSCStatus only reads Tenant status.
func (s *componentHandler) NewCRObject(_ context.Context, _ client.Client, dsc *dscv2.DataScienceCluster) (common.PlatformObject, error) {
	if !s.IsEnabled(dsc) {
		return nil, nil
	}

	return &componentApi.ModelsAsService{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.ModelsAsServiceKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.ModelsAsServiceInstanceName,
		},
		Spec: componentApi.ModelsAsServiceSpec{},
	}, nil
}

// IsEnabled checks if the ModelsAsService component should be deployed.
func (s *componentHandler) IsEnabled(dsc *dscv2.DataScienceCluster) bool {
	// ModelsAsService is enabled when:
	// 1. KServe component is enabled in the DSC
	// 2. ModelsAsService sub-component is configured with ManagementState = Managed
	if dsc.Spec.Components.Kserve.ManagementState != operatorv1.Managed {
		return false
	}

	// Check ModelsAsService specific management state
	// For Technical preview release, default to Disabled if not explicitly set to Managed
	return dsc.Spec.Components.Kserve.ModelsAsService.ManagementState == operatorv1.Managed
}

// UpdateDSCStatus updates the ModelsAsService component status in the DataScienceCluster from Tenant.
func (s *componentHandler) UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	cs := metav1.ConditionUnknown

	dsc, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return cs, errors.New("failed to convert to DataScienceCluster")
	}

	rr.Conditions.MarkFalse(ReadyConditionType)

	if !s.IsEnabled(dsc) {
		ms := dsc.Spec.Components.Kserve.ModelsAsService.ManagementState
		if ms == "" {
			ms = operatorv1.Removed
		}
		rr.Conditions.MarkFalse(
			ReadyConditionType,
			conditions.WithReason(string(ms)),
			conditions.WithMessage("Component ManagementState is set to %s", string(ms)),
			conditions.WithSeverity(common.ConditionSeverityInfo),
		)
		return cs, nil
	}

	checkMaaSPrerequisites(ctx, rr)

	t := maasv1alpha1.Tenant{}
	t.Name = maasv1alpha1.TenantInstanceName
	t.Namespace = MaaSSubscriptionNamespace

	if err := rr.Client.Get(ctx, client.ObjectKeyFromObject(&t), &t); err != nil {
		switch {
		case k8serr.IsNotFound(err):
			rr.Conditions.MarkFalse(
				ReadyConditionType,
				conditions.WithReason(status.NotReadyReason),
				conditions.WithMessage("Tenant CR not available yet"),
			)
			return metav1.ConditionFalse, nil
		case apimeta.IsNoMatchError(err):
			rr.Conditions.MarkFalse(
				ReadyConditionType,
				conditions.WithReason(status.NotReadyReason),
				conditions.WithMessage("Tenant CRD not installed"),
			)
			return metav1.ConditionFalse, nil
		default:
			return cs, fmt.Errorf("failed to get Tenant %s/%s: %w", t.Namespace, t.Name, err)
		}
	}

	if !t.GetDeletionTimestamp().IsZero() {
		rr.Conditions.MarkFalse(
			ReadyConditionType,
			conditions.WithReason(status.DeletingReason),
			conditions.WithMessage(status.DeletingMessage),
		)
		return metav1.ConditionFalse, nil
	}

	if rc := apimeta.FindStatusCondition(t.Status.Conditions, status.ConditionTypeReady); rc != nil {
		rr.Conditions.MarkFrom(ReadyConditionType, metav1ConditionToCommon(*rc))
		cs = rc.Status
	} else {
		rr.Conditions.MarkFalse(
			ReadyConditionType,
			conditions.WithReason(status.NotReadyReason),
			conditions.WithMessage("Tenant CR exists but has no ready condition yet"),
		)
		cs = metav1.ConditionFalse
	}

	return cs, nil
}

func metav1ConditionToCommon(c metav1.Condition) common.Condition {
	return common.Condition{
		Type:               c.Type,
		Status:             c.Status,
		Reason:             c.Reason,
		Message:            c.Message,
		ObservedGeneration: c.ObservedGeneration,
		LastTransitionTime: c.LastTransitionTime,
	}
}

// requiredGatewayAnnotations lists the annotations that must be present on
// maas-default-gateway for MaaS to work correctly.
var requiredGatewayAnnotations = map[string]string{
	annotations.ManagedByODHOperator:  "false",
	annotations.AuthorinoTLSBootstrap: "true",
}

// checkMaaSPrerequisites verifies MaaS infrastructure prerequisites and sets
// the MaaSPrerequisitesAvailable condition. Checks:
//  1. maas-default-gateway exists and has required annotations
//  2. Authorino has TLS enabled on its listener
//
// This is an informational check that does not affect ModelsAsServiceReady
// or block reconciliation.
func checkMaaSPrerequisites(ctx context.Context, rr *types.ReconciliationRequest) {
	l := logf.FromContext(ctx).WithName("checkMaaSPrerequisites")

	issues := make([]string, 0, 4)
	issues = append(issues, checkGatewayAnnotations(ctx, l, rr)...)
	issues = append(issues, checkAuthorinoTLS(ctx, l, rr)...)

	if len(issues) > 0 {
		msg := strings.Join(issues, "; ")
		rr.Conditions.MarkFalse(
			status.ConditionMaaSPrerequisitesAvailable,
			conditions.WithReason(status.MaaSPrerequisitesNotMetReason),
			conditions.WithMessage("%s", msg),
			conditions.WithSeverity(common.ConditionSeverityInfo),
		)
		return
	}

	rr.Conditions.MarkTrue(
		status.ConditionMaaSPrerequisitesAvailable,
		conditions.WithReason(status.MaaSPrerequisitesMetReason),
		conditions.WithMessage(status.MaaSPrerequisitesMetMessage),
	)
}

// checkGatewayAnnotations verifies the maas-default-gateway has required annotations.
// Returns a list of human-readable issues found (empty if all good).
func checkGatewayAnnotations(ctx context.Context, l logr.Logger, rr *types.ReconciliationRequest) []string {
	gw := &gwapiv1.Gateway{}
	err := rr.Client.Get(ctx, client.ObjectKey{
		Name:      DefaultGatewayName,
		Namespace: DefaultGatewayNamespace,
	}, gw)

	if err != nil {
		if apimeta.IsNoMatchError(err) {
			l.Info("Gateway API CRD not installed")
			return []string{"Gateway API CRD is not installed; maas-default-gateway cannot be verified"}
		}
		if k8serr.IsNotFound(err) {
			l.Info("maas-default-gateway not found", "gateway", DefaultGatewayName, "namespace", DefaultGatewayNamespace)
			return []string{status.MaaSGatewayNotFoundMessage}
		}
		l.Error(err, "Failed to get maas-default-gateway")
		return []string{fmt.Sprintf("failed to check maas-default-gateway annotations: %v", err)}
	}

	gwAnnotations := gw.GetAnnotations()
	var missing []string
	for key, expectedValue := range requiredGatewayAnnotations {
		actual, ok := gwAnnotations[key]
		if !ok || actual != expectedValue {
			missing = append(missing, fmt.Sprintf("%s=%q", key, expectedValue))
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		l.Info("MaaS gateway missing required annotations", "gateway", DefaultGatewayName, "missing", missing)
		return []string{fmt.Sprintf(
			"maas-default-gateway is missing required annotations: %s",
			strings.Join(missing, ", "))}
	}

	return nil
}

// checkAuthorinoTLS verifies that at least one Authorino CR has TLS enabled
// on its listener (spec.listener.tls.enabled). Returns a list of issues found.
func checkAuthorinoTLS(ctx context.Context, l logr.Logger, rr *types.ReconciliationRequest) []string {
	authorinoList := &unstructured.UnstructuredList{}
	authorinoList.SetGroupVersionKind(gvk.Authorinov1beta1)

	if err := rr.Client.List(ctx, authorinoList); err != nil {
		if apimeta.IsNoMatchError(err) {
			l.Info("Authorino CRD not installed")
			return []string{"Authorino CRD is not installed; Authorino TLS configuration cannot be verified"}
		}
		l.Error(err, "Failed to list Authorino CRs")
		return []string{fmt.Sprintf("failed to check Authorino TLS configuration: %v", err)}
	}

	if len(authorinoList.Items) == 0 {
		l.Info("No Authorino CR found on the cluster")
		return []string{"Authorino CR not found; Authorino must be deployed for MaaS authentication to work"}
	}

	for _, a := range authorinoList.Items {
		enabled, found, err := unstructured.NestedBool(a.Object, "spec", "listener", "tls", "enabled")
		if err != nil {
			l.Error(err, "Failed to read spec.listener.tls.enabled from Authorino CR", "name", a.GetName(), "namespace", a.GetNamespace())
			continue
		}
		if found && enabled {
			return nil
		}
	}

	l.Info("Authorino TLS is not enabled on any Authorino CR")
	return []string{"Authorino TLS is not enabled (spec.listener.tls.enabled is not true); " +
		"MaaS requires Authorino to accept TLS connections from the gateway"}
}
