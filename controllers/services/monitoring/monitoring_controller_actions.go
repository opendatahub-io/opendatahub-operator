package monitoring

import (
	"context"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/set"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odhcli "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

var componentRules = map[string]string{
	componentApi.DashboardComponentName:            "rhods-dashboard*.rules",
	componentApi.WorkbenchesComponentName:          "workbenches*.rules",
	componentApi.KueueComponentName:                "kueue*.rules",
	componentApi.CodeFlareComponentName:            "codeflare*.rules",
	componentApi.DataSciencePipelinesComponentName: "data-science-pipelines-operator*.rules",
	componentApi.ModelMeshServingComponentName:     "model-mesh*.rules",
	componentApi.RayComponentName:                  "ray*.rules",
	componentApi.TrustyAIComponentName:             "trustyai*.rules",
	componentApi.KserveComponentName:               "kserve*.rules",
	componentApi.TrainingOperatorComponentName:     "trainingoperator*.rules",
	componentApi.ModelRegistryComponentName:        "model-registry-operator*.rules",
	componentApi.ModelControllerComponentName:      "odh-model-controller*.rules",
}

// initialize handles all pre-deployment configurations.
func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	// Only set prometheus configmap path
	rr.Manifests = []odhtypes.ManifestInfo{
		{
			Path:       odhdeploy.DefaultManifestPath,
			ContextDir: serviceApi.MonitoringServiceName,
			SourcePath: "prometheus/apps",
		},
	}

	return nil
}

type UpdatePrometheusConfigActionOptionFn func(*UpdatePrometheusConfigAction)

func WithComponentsRegistry(value *cr.Registry) UpdatePrometheusConfigActionOptionFn {
	return func(in *UpdatePrometheusConfigAction) {
		in.cr = value
	}
}

func NewUpdatePrometheusConfigAction(opts ...UpdatePrometheusConfigActionOptionFn) actions.Fn {
	action := UpdatePrometheusConfigAction{
		cr: cr.DefaultRegistry(),
	}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}

type UpdatePrometheusConfigAction struct {
	cr *cr.Registry
}

// if DSC has component as Removed, we remove component's Prom Rules.
// only when DSC has component as Managed and component CR is in "Ready" state, we add rules to Prom Rules.
// all other cases, we do not change Prom rules for component.
func (a *UpdatePrometheusConfigAction) run(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	err := rr.ForEachResource(func(obj *unstructured.Unstructured) (bool, error) {
		if obj.GroupVersionKind() != gvk.ConfigMap {
			return false, nil
		}

		cm := corev1.ConfigMap{}

		if err := rr.Scheme().Convert(obj, &cm, ctx); err != nil {
			return false, err
		}

		if err := a.updatePrometheusConfiguration(ctx, rr.Client, &cm); err != nil {
			return false, err
		}

		if err := rr.Scheme().Convert(&cm, obj, ctx); err != nil {
			return false, err
		}

		return true, nil
	})

	if err != nil {
		return err
	}

	return nil
}

func (a *UpdatePrometheusConfigAction) updatePrometheusConfiguration(
	ctx context.Context,
	cli *odhcli.Client,
	obj *corev1.ConfigMap,
) error {
	current := corev1.ConfigMap{}
	if err := cli.Get(ctx, client.ObjectKeyFromObject(obj), &current); err != nil && !k8serr.IsNotFound(err) {
		return err
	}

	var currentContent PrometheusConfig
	if err := resources.ExtractContent(&current, prometheusConfigurationEntry, &currentContent); err != nil {
		return err
	}

	var newContent PrometheusConfig
	if err := resources.ExtractContent(obj, prometheusConfigurationEntry, &newContent); err != nil {
		return err
	}

	newContent.RuleFiles = set.New(newContent.RuleFiles...).Insert(currentContent.RuleFiles...).SortedList()
	if err := newContent.computeRules(ctx, cli, a.cr); err != nil {
		return err
	}

	if err := resources.SetContent(obj, prometheusConfigurationEntry, newContent); err != nil {
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
	// Cannot use status.PhaseNotReady as the value here is not the
	// same as the constant ("Not Ready")
	m.Status.Phase = "NotReady"

	// condition
	nc := common.Condition{
		Type:    status.ConditionTypeReady,
		Status:  metav1.ConditionFalse,
		Reason:  "NotReady",
		Message: "Prometheus deployment is not ready",
	}

	promDeployment := &appsv1.DeploymentList{}
	err := rr.Client.List(
		ctx,
		promDeployment,
		client.InNamespace(m.Spec.Namespace),
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
		m.Status.Phase = status.PhaseReady
		// condition
		nc.Status = metav1.ConditionTrue
		nc.Reason = status.ReconcileCompleted
		nc.Message = status.ReconcileCompletedMessage
	}
	conditions.SetStatusCondition(&m.Status, nc)
	m.Status.ObservedGeneration = m.GetObjectMeta().GetGeneration()

	return nil
}
