package monitoring

import (
	"context"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
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

	err := cr.ForEach(func(ch cr.ComponentHandler) error {
		var enabled bool
		ci := ch.NewCRObject(dsc)
		// read the component instance to get tha actual status
		err := rr.Client.Get(ctx, client.ObjectKeyFromObject(ci), ci)
		switch {
		case err == nil:
			enabled = meta.IsStatusConditionTrue(ci.GetStatus().Conditions, status.ConditionTypeReady)
		case k8serr.IsNotFound(err):
			enabled = false
		default:
			enabled = false
			return fmt.Errorf("error getting component state: component=%s, enabled=%t, error=%w", ch.GetName(), enabled, err)
		}

		// Check for shared components
		if ch.GetName() == componentApi.KserveComponentName || ch.GetName() == componentApi.ModelMeshServingComponentName {
			if err := UpdatePrometheusConfig(ctx, enabled, componentRules[componentApi.ModelControllerComponentName]); err != nil {
				return err
			}
		}

		if err := UpdatePrometheusConfig(ctx, enabled, componentRules[ch.GetName()]); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
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
		Reason:  status.ReconcileInit,
		Message: status.PhaseNotReady,
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

	if len(promDeployment.Items) == 1 && ready == 1 {
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
