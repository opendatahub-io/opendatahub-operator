package kserve

import (
	"context"
	"errors"
	"fmt"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	featuresv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func checkPreConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve)", rr.Instance)
	}

	if k.Spec.Serving.ManagementState != operatorv1.Managed {
		rr.Conditions.MarkFalse(
			status.ConditionServerlessAvailable,
			conditions.WithSeverity(common.ConditionSeverityInfo),
			conditions.WithReason(string(k.Spec.Serving.ManagementState)),
			conditions.WithMessage("Serving management state is set to: %s", k.Spec.Serving.ManagementState))
		rr.Conditions.MarkFalse(
			status.ConditionServiceMeshAvailable,
			conditions.WithSeverity(common.ConditionSeverityInfo),
			conditions.WithReason(string(k.Spec.Serving.ManagementState)),
			conditions.WithMessage("Serving management state is set to: %s", k.Spec.Serving.ManagementState))

		return nil
	}

	rr.Conditions.MarkUnknown(status.ConditionServerlessAvailable)
	rr.Conditions.MarkUnknown(status.ConditionServiceMeshAvailable)

	if rr.DSCI.Spec.ServiceMesh == nil || rr.DSCI.Spec.ServiceMesh.ManagementState != operatorv1.Managed {
		rr.Conditions.MarkFalse(
			status.ConditionServerlessAvailable,
			conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
			conditions.WithReason(status.ServiceMeshNotConfiguredReason),
			conditions.WithMessage(status.ServiceMeshNotConfiguredMessage),
		)

		return ErrServiceMeshNotConfigured
	}

	if found, err := cluster.OperatorExists(ctx, rr.Client, serviceMeshOperator); err != nil || !found {
		e := ErrServiceMeshOperatorNotInstalled
		if err != nil {
			e = odherrors.NewStopErrorW(err)
		}

		rr.Conditions.MarkFalse(
			status.ConditionServiceMeshAvailable,
			conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
			conditions.WithError(e),
		)

		return e
	} else {
		rr.Conditions.MarkTrue(status.ConditionServiceMeshAvailable)
	}

	if found, err := cluster.OperatorExists(ctx, rr.Client, serverlessOperator); err != nil || !found {
		e := ErrServerlessOperatorNotInstalled
		if err != nil {
			e = odherrors.NewStopErrorW(err)
		}

		rr.Conditions.MarkFalse(
			status.ConditionServerlessAvailable,
			conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
			conditions.WithError(e),
		)

		return e
	} else {
		rr.Conditions.MarkTrue(status.ConditionServerlessAvailable)
	}

	return nil
}

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = []odhtypes.ManifestInfo{
		kserveManifestInfo(kserveManifestSourcePath),
	}

	return nil
}

func devFlags(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve)", rr.Instance)
	}

	df := k.GetDevFlags()
	if df == nil {
		return nil
	}
	if len(df.Manifests) == 0 {
		return nil
	}

	kSourcePath := kserveManifestSourcePath

	for _, subcomponent := range df.Manifests {
		if !strings.Contains(subcomponent.URI, componentName) && !strings.Contains(subcomponent.URI, LegacyComponentName) {
			continue
		}

		if err := deploy.DownloadManifests(ctx, componentName, subcomponent); err != nil {
			return err
		}

		if subcomponent.SourcePath != "" {
			kSourcePath = subcomponent.SourcePath
		}

		break
	}

	rr.Manifests = []odhtypes.ManifestInfo{
		kserveManifestInfo(kSourcePath),
	}

	return nil
}

func removeLegacyFeatureTrackerOwnerRef(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	ftNames := []string{
		rr.DSCI.Spec.ApplicationsNamespace + "-serverless-serving-deployment",
		rr.DSCI.Spec.ApplicationsNamespace + "-serverless-net-istio-secret-filtering",
		rr.DSCI.Spec.ApplicationsNamespace + "-serverless-serving-gateways",
		rr.DSCI.Spec.ApplicationsNamespace + "-kserve-external-authz",
	}

	for _, ftName := range ftNames {
		obj := &featuresv1.FeatureTracker{}
		err := rr.Client.Get(ctx, client.ObjectKey{Name: ftName}, obj)
		switch {
		case k8serr.IsNotFound(err):
			continue
		case err != nil:
			return fmt.Errorf("error while retrieving FeatureTracker %s: %w", ftName, err)
		}

		if err := resources.RemoveOwnerReferences(ctx, rr.Client, obj, isLegacyOwnerRef); err != nil {
			return err
		}
	}

	return nil
}

func configureServerless(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve)", rr.Instance)
	}

	logger := logf.FromContext(ctx)
	cli := rr.Client

	switch k.Spec.Serving.ManagementState {
	case operatorv1.Unmanaged: // Bring your own CR
		logger.Info("Serverless CR is not configured by the operator, we won't do anything")

	case operatorv1.Removed: // we remove serving CR
		logger.Info("existing Serverless CR (owned by operator) will be removed")
		if err := removeServerlessFeatures(ctx, rr.Client, k, &rr.DSCI.Spec); err != nil {
			return err
		}

	case operatorv1.Managed: // standard workflow to create CR
		if rr.DSCI.Spec.ServiceMesh == nil {
			return errors.New("ServiceMesh needs to be configured and 'Managed' in DSCI CR, " +
				"it is required by KServe serving")
		}

		switch rr.DSCI.Spec.ServiceMesh.ManagementState {
		case operatorv1.Unmanaged, operatorv1.Removed:
			return fmt.Errorf("ServiceMesh is currently set to '%s'. It needs to be set to 'Managed' in DSCI CR, "+
				"as it is required by the KServe serving field", rr.DSCI.Spec.ServiceMesh.ManagementState)
		}

		serverlessFeatures := feature.ComponentFeaturesHandler(rr.Instance, componentName, rr.DSCI.Spec.ApplicationsNamespace, configureServerlessFeatures(&rr.DSCI.Spec, k))

		if err := serverlessFeatures.Apply(ctx, cli); err != nil {
			return err
		}
	}
	return nil
}

func configureServiceMesh(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve)", rr.Instance)
	}

	cli := rr.Client

	if rr.DSCI.Spec.ServiceMesh != nil {
		if rr.DSCI.Spec.ServiceMesh.ManagementState == operatorv1.Managed {
			serviceMeshInitializer := feature.ComponentFeaturesHandler(k, componentName, rr.DSCI.Spec.ApplicationsNamespace, defineServiceMeshFeatures(ctx, cli, &rr.DSCI.Spec))
			return serviceMeshInitializer.Apply(ctx, cli)
		}
		if rr.DSCI.Spec.ServiceMesh.ManagementState == operatorv1.Unmanaged {
			return nil
		}
	}

	return removeServiceMeshConfigurations(ctx, cli, k, &rr.DSCI.Spec)
}

func customizeKserveConfigMap(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve)", rr.Instance)
	}

	logger := logf.FromContext(ctx)

	kserveConfigMap := corev1.ConfigMap{}
	cmidx, err := getIndexedResource(rr.Resources, &kserveConfigMap, gvk.ConfigMap, kserveConfigMapName)
	if err != nil {
		return err
	}

	switch k.Spec.Serving.ManagementState {
	case operatorv1.Managed, operatorv1.Unmanaged:
		if k.Spec.DefaultDeploymentMode == "" {
			// if the default mode is empty in the DSC, assume mode is "Serverless" since k.Serving is Managed
			if err := setDefaultDeploymentMode(&kserveConfigMap, componentApi.Serverless); err != nil {
				return err
			}
		} else {
			// if the default mode is explicitly specified, respect that
			if err := setDefaultDeploymentMode(&kserveConfigMap, k.Spec.DefaultDeploymentMode); err != nil {
				return err
			}
		}
	case operatorv1.Removed:
		if k.Spec.DefaultDeploymentMode == componentApi.Serverless {
			return errors.New("setting defaultdeployment mode as Serverless is incompatible with having Serving 'Removed'")
		}
		if k.Spec.DefaultDeploymentMode == "" {
			logger.Info("Serving is removed, Kserve will default to RawDeployment")
		}
		if err := setDefaultDeploymentMode(&kserveConfigMap, componentApi.RawDeployment); err != nil {
			return err
		}
	}

	err = replaceResourceAtIndex(rr.Resources, cmidx, &kserveConfigMap)
	if err != nil {
		return err
	}

	kserveConfigHash, err := hashConfigMap(&kserveConfigMap)
	if err != nil {
		return err
	}

	kserveDeployment := appsv1.Deployment{}
	deployidx, err := getIndexedResource(rr.Resources, &kserveDeployment, gvk.Deployment, "kserve-controller-manager")
	if err != nil {
		return err
	}

	kserveDeployment.Spec.Template.Annotations[labels.ODHAppPrefix+"/KserveConfigHash"] = kserveConfigHash

	err = replaceResourceAtIndex(rr.Resources, deployidx, &kserveDeployment)
	if err != nil {
		return err
	}

	return nil
}

func setStatusFields(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve)", rr.Instance)
	}

	ddm, err := getDefaultDeploymentMode(ctx, rr.Client, &rr.DSCI.Spec)
	if err != nil {
		return err
	}

	k.Status.DefaultDeploymentMode = ddm

	serviceMeshEnabled := false
	if rr.DSCI.Spec.ServiceMesh != nil {
		if rr.DSCI.Spec.ServiceMesh.ManagementState == operatorv1.Managed || rr.DSCI.Spec.ServiceMesh.ManagementState == operatorv1.Unmanaged {
			serviceMeshEnabled = true
		}
	}
	serverlessEnabled := false
	if k.Spec.Serving.ManagementState == operatorv1.Managed || k.Spec.Serving.ManagementState == operatorv1.Unmanaged {
		serverlessEnabled = true
	}
	k.Status.ServerlessMode = operatorv1.Removed
	if serverlessEnabled && serviceMeshEnabled {
		k.Status.ServerlessMode = operatorv1.Managed
	}
	return nil
}
