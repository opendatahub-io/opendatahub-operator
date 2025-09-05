/*
Copyright 2023.

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

package gateway

import (
	"context"
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	sr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

//nolint:gochecknoinits
func init() {
	sr.Add(&serviceHandler{})
}

type serviceHandler struct {
}

func (h *serviceHandler) Init(_ common.Platform) error {
	return nil
}

func (h *serviceHandler) GetName() string {
	return ServiceName
}

func (h *serviceHandler) GetManagementState(platform common.Platform, dsci *dsciv1.DSCInitialization) operatorv1.ManagementState {
	// Only enable Gateway service when networking mode is set to "gateway-api"
	if dsci != nil && dsci.Spec.Networking != nil && dsci.Spec.Networking.Mode == "gateway-api" {
		return operatorv1.Managed
	}
	return operatorv1.Removed
}

func (h *serviceHandler) NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &serviceApi.Gateway{}).
		// operands - owned
		Owns(&gwapiv1.Gateway{}).
		Owns(&gwapiv1.GatewayClass{}).
		Owns(&corev1.Secret{}).
		Owns(&certmanagerv1.Certificate{}).
		Owns(&certmanagerv1.ClusterIssuer{}).
		// actions
		WithAction(initialize).
		WithAction(createCertificateResources).
		WithAction(template.NewAction()).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		Build(ctx)

	if err != nil {
		return fmt.Errorf("could not create the gateway controller: %w", err)
	}

	return nil
}
