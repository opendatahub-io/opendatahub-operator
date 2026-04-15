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
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	t := maasv1alpha1.Tenant{}
	t.Name = maasv1alpha1.TenantInstanceName
	t.Namespace = MaaSSubscriptionNamespace

	if err := rr.Client.Get(ctx, client.ObjectKeyFromObject(&t), &t); err != nil && !k8serr.IsNotFound(err) {
		return cs, nil
	}

	dsc, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return cs, errors.New("failed to convert to DataScienceCluster")
	}

	rr.Conditions.MarkFalse(ReadyConditionType)

	if !t.GetDeletionTimestamp().IsZero() {
		rr.Conditions.MarkFalse(
			ReadyConditionType,
			conditions.WithReason(status.DeletingReason),
			conditions.WithMessage(status.DeletingMessage),
		)
		return metav1.ConditionFalse, nil
	}

	if s.IsEnabled(dsc) {
		if rc := apimeta.FindStatusCondition(t.Status.Conditions, status.ConditionTypeReady); rc != nil {
			rr.Conditions.MarkFrom(ReadyConditionType, metav1ConditionToCommon(*rc))
			cs = rc.Status
		} else {
			cs = metav1.ConditionFalse
		}
	} else {
		if dsc.Spec.Components.Kserve.ManagementState != operatorv1.Managed {
			rr.Conditions.MarkFalse(
				ReadyConditionType,
				conditions.WithReason("KServeDisabled"),
				conditions.WithMessage("KServe component is not managed, ModelsAsService requires KServe to be enabled"),
				conditions.WithSeverity(common.ConditionSeverityInfo),
			)
		} else {
			rr.Conditions.MarkFalse(
				ReadyConditionType,
				conditions.WithReason(string(operatorv1.Removed)),
				conditions.WithMessage("Component ManagementState is set to Removed"),
				conditions.WithSeverity(common.ConditionSeverityInfo),
			)
		}
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
