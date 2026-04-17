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

	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
	operatorv1 "github.com/openshift/api/operator/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
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

// NewComponentReconciler is a no-op: Tenant platform reconciliation (kustomize, deploy, GC)
// is owned by maas-controller. ODH still materialises the CR from the DSC via
// AppendOperatorInstallManifests and aggregates status in UpdateDSCStatus.
func (s *componentHandler) NewComponentReconciler(_ context.Context, _ ctrl.Manager) error {
	ctrl.Log.WithName("controllers").WithName("modelsasservice").Info(
		"Tenant platform reconcile owned by maas-controller; no ODH component controller registered",
	)
	return nil
}

// NewCRObject returns nil — maas-controller owns Tenant CR creation via its
// ensureDefaultTenant startup runnable. The ODH operator only reads Tenant
// status (UpdateDSCStatus) and deletes it on MaaS disable.
func (s *componentHandler) NewCRObject(_ context.Context, _ client.Client, _ *dscv2.DataScienceCluster) (common.PlatformObject, error) {
	return nil, nil
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

	t := maasv1alpha1.Tenant{}
	t.Name = maasv1alpha1.TenantInstanceName
	t.Namespace = MaaSSubscriptionNamespace

	if err := rr.Client.Get(ctx, client.ObjectKeyFromObject(&t), &t); err != nil {
		rr.Conditions.MarkFalse(
			ReadyConditionType,
			conditions.WithReason(status.NotReadyReason),
			conditions.WithMessage("Tenant CR not available yet"),
		)
		return metav1.ConditionFalse, nil
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
