package monitoring

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
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
}

// initialize handles all pre-deployment configurations.
func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	log := logf.FromContext(ctx)
	// Only handle manifests setup and initial configurations
	platform := rr.Release.Name
	switch platform {
	case cluster.ManagedRhoai:
		// Only set prometheus configmap path
		rr.Manifests = []odhtypes.ManifestInfo{
			{
				Path:       odhdeploy.DefaultManifestPath,
				ContextDir: "monitoring/prometheus/apps",
			},
		}

	default:
		log.V(3).Info("Monitoring enabled, won't apply changes in this mode", "cluster", platform)
	}

	return nil
}

// if DSC has component as Removed, we remove component's Prom Rules.
// only when DSC has component as Managed and component CR is in "Ready" state, we add rules to Prom Rules.
// all other cases, we do not change Prom rules for component.
func updatePrometheusConfigMap(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// Map component names to their rule prefixes
	dscList := &dscv1.DataScienceClusterList{}
	if err := rr.Client.List(ctx, dscList); err != nil {
		return fmt.Errorf("failed to list DSC: %w", err)
	}
	if len(dscList.Items) == 0 {
		return nil
	}
	if len(dscList.Items) > 1 {
		return errors.New("multiple DataScienceCluster found")
	}

	dsc := &dscList.Items[0]

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

func updateStatus(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	m, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	// TODO: deprecate phase
	m.Status.Phase = "NotReady"
	// condition
	nc := metav1.Condition{
		Type:    string(ReadyConditionType),
		Status:  metav1.ConditionFalse,
		Reason:  status.PhaseNotReady,
		Message: "Prometheus deployment is not ready",
	}

	promDeployment := &appsv1.DeploymentList{}
	err := rr.Client.List(
		ctx,
		promDeployment,
		client.InNamespace(rr.DSCI.Spec.Monitoring.Namespace),
	)
	if err != nil {
		return fmt.Errorf("error fetching promethus deployments: %w", err)
	}

	ready := 0
	for _, deployment := range promDeployment.Items {
		if deployment.Status.ReadyReplicas == deployment.Status.Replicas {
			ready++
		}
	}

	if len(promDeployment.Items) == ready {
		// TODO: deprecate phase
		m.Status.Phase = "Ready"
		// condition
		nc.Status = metav1.ConditionTrue
		nc.Reason = status.ReconcileCompleted
		nc.Message = status.ReconcileCompletedMessage
	}
	meta.SetStatusCondition(&m.Status.Conditions, nc)
	m.Status.ObservedGeneration = m.GetObjectMeta().GetGeneration()

	return nil
}
