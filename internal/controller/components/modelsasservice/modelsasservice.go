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

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

// GetName returns the component name for ModelsAsService.
func (s *componentHandler) GetName() string {
	return componentApi.ModelsAsServiceComponentName
}

// Init initializes the ModelsAsService component.
func (s *componentHandler) Init(_ common.Platform) error {
	mi := baseManifestInfo(BaseManifestsSourcePath)

	if err := odhdeploy.ApplyParams(mi.String(), "params.env", imagesMap, extraParamsMap); err != nil {
		return fmt.Errorf("failed to update params on path %s: %w", mi, err)
	}

	return nil
}

// NewCRObject constructs a new ModelsAsService Custom Resource.
func (s *componentHandler) NewCRObject(dsc *dscv2.DataScienceCluster) common.PlatformObject {
	// Extract ModelsAsService configuration from KServe component in DSC
	maasConfig := dsc.Spec.Components.Kserve.ModelsAsService

	// Determine management state from the DSC configuration
	managementState := operatorv1.Removed
	if maasConfig.ManagementState == operatorv1.Managed {
		managementState = maasConfig.ManagementState
	}

	// Configure Gateway spec - use defaults if not specified
	gatewaySpec := componentApi.GatewaySpec{
		Namespace: DefaultGatewayNamespace,
		Name:      DefaultGatewayName,
	}

	// Override with DSC configuration if provided
	if maasConfig.Gateway.Namespace != "" || maasConfig.Gateway.Name != "" {
		// All-or-nothing validation should be handled during reconciliation
		gatewaySpec = maasConfig.Gateway
	}

	return &componentApi.ModelsAsService{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.ModelsAsServiceKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.ModelsAsServiceInstanceName,
			Annotations: map[string]string{
				annotations.ManagementStateAnnotation: string(managementState),
			},
		},
		Spec: componentApi.ModelsAsServiceSpec{
			Gateway: gatewaySpec,
		},
	}
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

// UpdateDSCStatus updates the ModelsAsService component status in the DataScienceCluster.
func (s *componentHandler) UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	cs := metav1.ConditionUnknown

	c := componentApi.ModelsAsService{}
	c.Name = componentApi.ModelsAsServiceInstanceName

	if err := rr.Client.Get(ctx, client.ObjectKeyFromObject(&c), &c); err != nil && !k8serr.IsNotFound(err) {
		return cs, nil
	}

	dsc, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return cs, errors.New("failed to convert to DataScienceCluster")
	}

	rr.Conditions.MarkFalse(ReadyConditionType)

	if s.IsEnabled(dsc) {
		if rc := conditions.FindStatusCondition(c.GetStatus(), status.ConditionTypeReady); rc != nil {
			rr.Conditions.MarkFrom(ReadyConditionType, *rc)
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
