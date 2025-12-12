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

package gateway

// +kubebuilder:rbac:groups=route.openshift.io,resources=routes/custom-host,verbs=create;patch

import (
	"context"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// GatewayServiceFullName is the name of the auto-created Gateway service.
// Format: <gateway-name>-<gatewayclass-name>.
var GatewayServiceFullName = DefaultGatewayName + "-" + GatewayClassName

// createOCPRoutes adds OCP Route template when in OcpRoute mode.
func createOCPRoutes(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createOCPRoutes")

	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return err
	}

	if gatewayConfig.Spec.IngressMode != serviceApi.IngressModeOcpRoute {
		l.V(1).Info("IngressMode is not OcpRoute, skipping OCP Route creation")
		return nil
	}

	l.V(1).Info("Adding OCP Route template for Gateway")

	rr.Templates = append(rr.Templates, odhtypes.TemplateInfo{
		FS:   gatewayResources,
		Path: ocpRouteTemplate,
	})

	return nil
}
