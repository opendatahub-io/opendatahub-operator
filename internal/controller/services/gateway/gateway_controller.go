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
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	sr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

//nolint:gochecknoinits
func init() {
	sr.Add(&ServiceHandler{})
}

type ServiceHandler struct {
}

func (h *ServiceHandler) Init(platform common.Platform) error {
	return nil
}

func (h *ServiceHandler) GetName() string {
	return ServiceName
}

func (h *ServiceHandler) GetManagementState(platform common.Platform, _ *dsciv1.DSCInitialization) operatorv1.ManagementState {
	return operatorv1.Managed
}

func (h *ServiceHandler) NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	utilruntime.Must(certmanagerv1.AddToScheme(mgr.GetScheme()))

	_, err := reconciler.ReconcilerFor(mgr, &serviceApi.Gateway{}).
		WithAction(createGatewayInfrastructure).
		WithAction(template.NewAction()).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		WithAction(gc.NewAction()).
		Build(ctx)
	if err != nil {
		return fmt.Errorf("could not create the Gateway controller: %w", err)
	}

	return nil
}
