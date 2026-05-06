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

package monitoring

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

var monitoringGVK = schema.GroupVersionKind{
	Group:   "services.platform.opendatahub.io",
	Version: "v1alpha1",
	Kind:    "Monitoring",
}

type handler struct {
	modules.BaseHandler
}

func NewHandler() modules.ModuleHandler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:        serviceApi.MonitoringServiceName,
				GVK:         monitoringGVK,
				CRName:      serviceApi.MonitoringInstanceName,
				ReleaseName: "odh-observability",
				ChartDir:    "odh-observability",
			},
		},
	}
}

// IsEnabled always returns true. Monitoring is a platform service configured
// via the DSCI, not a DSC component. The module operator is always deployed;
// ManagementState in the Monitoring CR controls whether it actively manages
// resources (Managed) or tears them down (Removed).
func (h *handler) IsEnabled(_ *dscv2.DataScienceCluster) bool {
	return true
}

// BuildModuleCR fetches the DSCI, reads Spec.Monitoring, and projects the
// fields into the Monitoring CR spec. ManagementState is forced to Managed
// on managed RHOAI clusters regardless of what the DSCI specifies.
func (h *handler) BuildModuleCR(
	ctx context.Context,
	cli client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	dsciList := &dsciv2.DSCInitializationList{}
	if err := cli.List(ctx, dsciList); err != nil {
		return nil, fmt.Errorf("listing DSCInitialization: %w", err)
	}
	if len(dsciList.Items) == 0 {
		return nil, fmt.Errorf("no DSCInitialization found")
	}

	monSpec := dsciList.Items[0].Spec.Monitoring

	// On managed RHOAI, monitoring is always Managed regardless of DSCI config.
	if platform.Release.Name == cluster.ManagedRhoai {
		monSpec.ManagementState = "Managed"
	}

	specBytes, err := json.Marshal(monSpec)
	if err != nil {
		return nil, fmt.Errorf("marshalling monitoring spec: %w", err)
	}
	var specMap map[string]any
	if err := json.Unmarshal(specBytes, &specMap); err != nil {
		return nil, fmt.Errorf("unmarshalling monitoring spec: %w", err)
	}

	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(monitoringGVK)
	cr.SetName(h.Config.CRName)
	cr.Object["spec"] = specMap

	return cr, nil
}
