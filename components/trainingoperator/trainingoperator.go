// Package trainingoperator provides utility functions to config trainingoperator as part of the stack
// which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists
// +groupName=datasciencecluster.opendatahub.io
package trainingoperator

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/go-logr/logr"
	operatorv1 "github.com/openshift/api/operator/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

var (
	ComponentName        = "trainingoperator"
	TrainingOperatorPath = deploy.DefaultManifestPath + "/" + ComponentName + "/rhoai"
)

// Verifies that TrainingOperator implements ComponentInterface.
var _ components.ComponentInterface = (*TrainingOperator)(nil)

// TrainingOperator struct holds the configuration for the TrainingOperator component.
// +kubebuilder:object:generate=true
type TrainingOperator struct {
	components.Component `json:""`
}

func (t *TrainingOperator) OverrideManifests(ctx context.Context, _ cluster.Platform) error {
	// If devflags are set, update default manifests path
	if len(t.DevFlags.Manifests) != 0 {
		manifestConfig := t.DevFlags.Manifests[0]
		if err := deploy.DownloadManifests(ctx, ComponentName, manifestConfig); err != nil {
			return err
		}
		// If overlay is defined, update paths
		defaultKustomizePath := "rhoai"
		if manifestConfig.SourcePath != "" {
			defaultKustomizePath = manifestConfig.SourcePath
		}
		TrainingOperatorPath = filepath.Join(deploy.DefaultManifestPath, ComponentName, defaultKustomizePath)
	}

	return nil
}

func (t *TrainingOperator) GetComponentName() string {
	return ComponentName
}

func (t *TrainingOperator) ReconcileComponent(ctx context.Context, cli client.Client, logger logr.Logger,
	owner metav1.Object, dscispec *dsciv1.DSCInitializationSpec, platform cluster.Platform, _ bool) (conditionsv1.Condition, error) {
	l := t.ConfigComponentLogger(logger, ComponentName, dscispec)

	var imageParamMap = map[string]string{
		"odh-training-operator-controller-image": "RELATED_IMAGE_ODH_TRAINING_OPERATOR_IMAGE",
	}

	enabled := t.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed

	if enabled {
		if t.DevFlags != nil {
			// Download manifests and update paths
			if err := t.OverrideManifests(ctx, platform); err != nil {
				return status.FailedComponentCondition(ComponentName, err)
			}
		}
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (t.DevFlags == nil || len(t.DevFlags.Manifests) == 0) {
			if err := deploy.ApplyParams(TrainingOperatorPath, imageParamMap); err != nil {
				return status.FailedComponentCondition(ComponentName, err)
			}
		}
	}
	// Deploy Training Operator
	if err := deploy.DeployManifestsFromPath(ctx, cli, owner, TrainingOperatorPath, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
		return status.FailedComponentCondition(ComponentName, fmt.Errorf("failed to apply manifests %s: %w", TrainingOperatorPath, err))
	}
	l.Info("apply manifests done")

	if enabled {
		if err := cluster.WaitForDeploymentAvailable(ctx, cli, ComponentName, dscispec.ApplicationsNamespace, 20, 2); err != nil {
			return status.FailedComponentCondition(ComponentName, fmt.Errorf("deployment for %s is not ready to server: %w", ComponentName, err))
		}
	}

	// CloudService Monitoring handling
	if platform == cluster.ManagedRhods {
		if err := t.UpdatePrometheusConfig(cli, l, enabled && monitoringEnabled, ComponentName); err != nil {
			return status.FailedComponentCondition(ComponentName, err)
		}
		if err := deploy.DeployManifestsFromPath(ctx, cli, owner,
			filepath.Join(deploy.DefaultManifestPath, "monitoring", "prometheus", "apps"),
			dscispec.Monitoring.Namespace,
			"prometheus", true); err != nil {
			return status.FailedComponentCondition(ComponentName, err)
		}
		l.Info("updating SRE monitoring done")
	}

	return status.SuccessComponentCondition(ComponentName), nil
}
