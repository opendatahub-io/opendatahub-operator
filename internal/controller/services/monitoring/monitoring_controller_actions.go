package monitoring

import (
	"context"
	"errors"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

var componentRules = map[string]string{
	componentApi.DashboardComponentName:            "rhods-dashboard",
	componentApi.WorkbenchesComponentName:          "workbenches",
	componentApi.KueueComponentName:                "kueue",
	componentApi.CodeFlareComponentName:            "codeflare",
	componentApi.DataSciencePipelinesComponentName: "data-science-pipelines-operator",
	componentApi.ModelMeshServingComponentName:     "model-mesh",
	componentApi.RayComponentName:                  "ray",
	componentApi.TrustyAIComponentName:             "trustyai",
	componentApi.KserveComponentName:               "kserve",
	componentApi.TrainingOperatorComponentName:     "trainingoperator",
	componentApi.ModelRegistryComponentName:        "model-registry-operator",
	componentApi.ModelControllerComponentName:      "odh-model-controller",
	componentApi.FeastOperatorComponentName:        "feastoperator",
	componentApi.LlamaStackOperatorComponentName:   "llamastackoperator",
}

// initialize handles all pre-deployment configurations.
func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	// Only set prometheus configmap path
	rr.Manifests = []odhtypes.ManifestInfo{
		{
			Path:       odhdeploy.DefaultManifestPath,
			ContextDir: "monitoring/prometheus/apps",
		},
	}

	return nil
}

// if DSC has component as Removed, we remove component's Prom Rules.
// only when DSC has component as Managed and component CR is in "Ready" state, we add rules to Prom Rules.
// all other cases, we do not change Prom rules for component.
func updatePrometheusConfigMap(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// Skip update prom config: if cluster is NOT ManagedRhoai
	if rr.Release.Name != cluster.ManagedRhoai {
		return nil
	}

	dsc, err := cluster.GetDSC(ctx, rr.Client)
	if err != nil {
		if k8serr.IsNotFound(err) {
			// DSC doesn't exist, skip prometheus configmap update
			return nil
		}
		return fmt.Errorf("failed to retrieve DataScienceCluster: %w", err)
	}

	return cr.ForEach(func(ch cr.ComponentHandler) error {
		ci := ch.NewCRObject(dsc)
		if ch.IsEnabled(dsc) {
			ready, err := isComponentReady(ctx, rr.Client, ci)
			if err != nil {
				return fmt.Errorf("failed to get component status %w", err)
			}
			if !ready { // not ready, skip change on prom rules
				return nil
			}
			// add
			return updatePrometheusConfig(ctx, true, componentRules[ch.GetName()])
		} else {
			return updatePrometheusConfig(ctx, false, componentRules[ch.GetName()])
		}
	})
}

func createMonitoringStack(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	if monitoring.Spec.Metrics != nil && (monitoring.Spec.Metrics.Resources != nil || monitoring.Spec.Metrics.Storage != nil) {
		if msExists, _ := cluster.HasCRD(ctx, rr.Client, gvk.MonitoringStack); !msExists {
			return errors.New("MonitoringStack CRD not found")
		}
		template := []odhtypes.TemplateInfo{
			{
				FS:   resourcesFS,
				Path: MonitoringStackTemplate,
			},
		}
		rr.Templates = append(rr.Templates, template...)
	}

	return nil
}
