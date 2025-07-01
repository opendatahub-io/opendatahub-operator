package monitoring

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	// Get the monitoring instance
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *serviceApi.Monitoring")
	}

	// If monitoring is unmanaged or the release is not managed, we don't need to update the prometheus configmap
	if monitoring.Spec.ManagementState == operatorv1.Unmanaged || rr.Release.Name != cluster.ManagedRhoai {
		return nil
	}

	// Map component names to their rule prefixes
	dsc, err := cluster.GetDSC(ctx, rr.Client)
	// If the DSC doesn't exist, we don't need to update the prometheus configmap
	if err != nil && k8serr.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get DataScienceCluster instance: %w", err)
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
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	// Only create monitoring stack if monitoring is managed and has metrics config
	if monitoring.Spec.ManagementState != operatorv1.Managed || monitoring.Spec.Metrics == nil {
		return nil
	}

	msExists, _ := cluster.HasCRD(ctx, rr.Client, gvk.MonitoringStack)
	if !msExists {
		return errors.New("MonitoringStack CRD not found")
	}

	template := []odhtypes.TemplateInfo{
		{
			FS:   resourcesFS,
			Path: MonitoringStackTemplate,
		},
	}
	rr.Templates = append(rr.Templates, template...)

	return nil
}

// handleInstrumentationCR manages OpenTelemetry Instrumentation CRs using server-side apply.
func handleInstrumentationCR(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *serviceApi.Monitoring")
	}

	dsci, err := cluster.GetDSCI(ctx, rr.Client)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get DSCInitialization instance: %w", err)
	}

	instrumentationName := "opendatahub-instrumentation"
	instrumentationNamespace := monitoring.Spec.Namespace

	existingInstrumentation := &unstructured.Unstructured{}
	existingInstrumentation.SetGroupVersionKind(gvk.Instrumentation)
	existingInstrumentation.SetName(instrumentationName)
	existingInstrumentation.SetNamespace(instrumentationNamespace)

	existingErr := rr.Client.Get(ctx, client.ObjectKeyFromObject(existingInstrumentation), existingInstrumentation)
	if existingErr != nil && !k8serr.IsNotFound(existingErr) {
		return fmt.Errorf("failed to get existing instrumentation: %w", existingErr)
	}
	instrumentationExists := existingErr == nil

	switch dsci.Spec.Monitoring.ManagementState {
	case operatorv1.Managed:
		// Only create instrumentation CR if monitoring is managed and has traces config
		if monitoring.Spec.ManagementState != operatorv1.Managed || monitoring.Spec.Traces == nil {
			// If traces are not configured but instrumentation exists, remove it
			if instrumentationExists {
				if err := rr.Client.Delete(ctx, existingInstrumentation); err != nil && !k8serr.IsNotFound(err) {
					return fmt.Errorf("failed to delete instrumentation CR: %w", err)
				}
			}
			return nil
		}

		sampleRatio := *monitoring.Spec.SampleRatio

		instrumentation := &unstructured.Unstructured{}
		instrumentation.SetGroupVersionKind(gvk.Instrumentation)
		instrumentation.SetName(instrumentationName)
		instrumentation.SetNamespace(instrumentationNamespace)

		otlpEndpoint := fmt.Sprintf("http://otel-collector.%s.svc.cluster.local:4317", instrumentationNamespace)

		spec := map[string]interface{}{
			"exporter": map[string]interface{}{
				"endpoint": otlpEndpoint,
			},
			"sampler": map[string]interface{}{
				"type":     "traceidratio",
				"argument": sampleRatio,
			},
		}

		if err := unstructured.SetNestedMap(instrumentation.Object, spec, "spec"); err != nil {
			return fmt.Errorf("failed to set instrumentation spec: %w", err)
		}

		return rr.AddResources(instrumentation)

	case operatorv1.Unmanaged:
		if instrumentationExists {
			if err := rr.Client.Delete(ctx, existingInstrumentation); err != nil && !k8serr.IsNotFound(err) {
				return fmt.Errorf("failed to delete instrumentation CR: %w", err)
			}
		}
		return nil

	default:
		return fmt.Errorf("unsupported monitoring management state: %s", dsci.Spec.Monitoring.ManagementState)
	}
}
