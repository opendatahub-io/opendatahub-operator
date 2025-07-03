package monitoring

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
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
	// componentApi.LlamaStackOperatorComponentName:        "llamastackoperator",  enable this when we are on TP
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
	// Map component names to their rule prefixes
	dsc, err := cluster.GetDSC(ctx, rr.Client)
	// If the DSC doesn't exist, we don't need to update the prometheus configmap
	if err != nil && k8serr.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get DataScienceCluster instance: %w", err)
	}

	ok, dsci, _ := checkDSCI(ctx, rr)
	if !ok {
		return nil
	}

	// If monitoring is unmanaged or the release is not managed, we don't need to update the prometheus configmap
	if dsci.Spec.Monitoring.ManagementState == operatorv1.Unmanaged || rr.Release.Name != cluster.ManagedRhoai {
		return nil
	}

	return cr.ForEach(func(ch cr.ComponentHandler) error {
		ci := ch.NewCRObject(dsc)
		ms := ch.GetManagementState(dsc) // check for modelcontroller with dependency is done in its GetManagementState()
		switch ms {
		case operatorv1.Removed: // remove
			return updatePrometheusConfig(ctx, false, componentRules[ch.GetName()])
		case operatorv1.Managed:
			ready, err := isComponentReady(ctx, rr.Client, ci)
			if err != nil {
				return fmt.Errorf("failed to get component status %w", err)
			}
			if !ready { // not ready, skip change on prom rules
				return nil
			}
			// add
			return updatePrometheusConfig(ctx, true, componentRules[ch.GetName()])
		default:
			return fmt.Errorf("unsuported management state %s", ms)
		}
	})
}

func createMonitoringStack(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	ok, _, _ := checkDSCI(ctx, rr)
	if !ok {
		return nil
	}

	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	msExists, _ := cluster.HasCRD(ctx, rr.Client, gvk.MonitoringStack)
	if !msExists {
		return errors.New("MonitoringStack CRD not found")
	}
	if monitoring.Spec.Metrics != nil {
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

func checkDSCI(ctx context.Context, rr *odhtypes.ReconciliationRequest) (bool, *dsciv1.DSCInitialization, error) {
	dsci, err := cluster.GetDSCI(ctx, rr.Client)
	// DSCI not found
	if err != nil && k8serr.IsNotFound(err) {
		return false, nil, nil
	}
	// DSCI found but error
	if err != nil {
		return false, nil, fmt.Errorf("failed to get DataScienceClusterInitialization instance: %w", err)
	}

	return true, dsci, nil
}
