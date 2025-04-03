package kserve

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	featuresv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func checkPreConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve)", rr.Instance)
	}

	rr.Conditions.MarkUnknown(status.ConditionServingAvailable)

	if k.Spec.Serving.ManagementState != operatorv1.Managed {
		rr.Conditions.MarkFalse(
			status.ConditionServingAvailable,
			conditions.WithSeverity(common.ConditionSeverityInfo),
			conditions.WithReason(string(k.Spec.Serving.ManagementState)),
			conditions.WithMessage("Serving management state is set to: %s", k.Spec.Serving.ManagementState))

		return nil
	}

	if rr.DSCI.Spec.ServiceMesh == nil || rr.DSCI.Spec.ServiceMesh.ManagementState != operatorv1.Managed {
		rr.Conditions.MarkFalse(
			status.ConditionServingAvailable,
			conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
			conditions.WithReason(status.ServiceMeshNotConfiguredReason),
			conditions.WithMessage(status.ServiceMeshNotConfiguredMessage),
		)

		return ErrServiceMeshNotConfigured
	}

	var operatorsErr error

	if found, err := cluster.OperatorExists(ctx, rr.Client, serviceMeshOperator); err != nil || !found {
		if err != nil {
			return odherrors.NewStopErrorW(err)
		}

		operatorsErr = multierror.Append(operatorsErr, ErrServiceMeshOperatorNotInstalled)
	}

	if found, err := cluster.OperatorExists(ctx, rr.Client, serverlessOperator); err != nil || !found {
		if err != nil {
			return odherrors.NewStopErrorW(err)
		}

		operatorsErr = multierror.Append(operatorsErr, ErrServerlessOperatorNotInstalled)
	}

	if operatorsErr != nil {
		rr.Conditions.MarkFalse(
			status.ConditionServingAvailable,
			conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
			conditions.WithError(operatorsErr),
		)

		return odherrors.NewStopErrorW(operatorsErr)
	}

	rr.Conditions.MarkTrue(status.ConditionServingAvailable)

	return nil
}

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
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

func deleteFeatureTrackers(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	ftNames := []string{
		rr.DSCI.Spec.ApplicationsNamespace + "-serverless-serving-deployment",
		rr.DSCI.Spec.ApplicationsNamespace + "-serverless-net-istio-secret-filtering",
		rr.DSCI.Spec.ApplicationsNamespace + "-serverless-serving-gateways",
		rr.DSCI.Spec.ApplicationsNamespace + "-kserve-external-authz",
	}

	for _, n := range ftNames {
		ft := featuresv1.FeatureTracker{}
		err := rr.Client.Get(ctx, client.ObjectKey{Name: n}, &ft)
		if k8serr.IsNotFound(err) {
			continue
		} else if err != nil {
			return fmt.Errorf("failed to lookup FeatureTracker %s: %w", ft.GetName(), err)
		}

		err = rr.Client.Delete(ctx, &ft, client.PropagationPolicy(metav1.DeletePropagationForeground))
		if k8serr.IsNotFound(err) {
			continue
		} else if err != nil {
			return fmt.Errorf("failed to delete FeatureTracker %s: %w", ft.GetName(), err)
		}
	}

	return nil
}

func addServingCertResourceIfManaged(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve)", rr.Instance)
	}

	if k.Spec.Serving.ManagementState == operatorv1.Managed {
		if rr.DSCI.Spec.ServiceMesh == nil {
			return errors.New("ServiceMesh needs to be configured and 'Managed' in DSCI CR, " +
				"it is required by KServe serving")
		}

		err := createServingCertResource(ctx, rr.Client, &rr.DSCI.Spec, k)
		if err != nil {
			return fmt.Errorf("unable to create serverless serving certificate secret: %w", err)
		}
	}
	return nil
}

func addTemplateFiles(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// Always add all the templates...will get removed before deploy if necessary
	templates := []odhtypes.TemplateInfo{
		{
			FS:   resourcesFS,
			Path: "resources/serving-install/service-mesh-subscription.tmpl.yaml",
		},
		{
			FS:   resourcesFS,
			Path: "resources/serving-install/knative-serving.tmpl.yaml",
		},
		{
			FS:   resourcesFS,
			Path: "resources/servicemesh/routing/istio-ingress-gateway.tmpl.yaml",
		},
		{
			FS:   resourcesFS,
			Path: "resources/servicemesh/routing/istio-kserve-local-gateway.tmpl.yaml",
		},
		{
			FS:   resourcesFS,
			Path: "resources/servicemesh/routing/istio-local-gateway.yaml",
		},
		{
			FS:   resourcesFS,
			Path: "resources/servicemesh/routing/kserve-local-gateway-svc.tmpl.yaml",
		},
		{
			FS:   resourcesFS,
			Path: "resources/servicemesh/routing/local-gateway-svc.tmpl.yaml",
		},

		// TODO these are the servicemesh ones
		{
			FS:   resourcesFS,
			Path: "resources/servicemesh/activator-envoyfilter.tmpl.yaml",
		},
		{
			FS:   resourcesFS,
			Path: "resources/servicemesh/kserve-predictor-authorizationpolicy.tmpl.yaml",
		},
		{
			FS:   resourcesFS,
			Path: "resources/servicemesh/kserve-inferencegraph-envoyfilter.tmpl.yaml",
		},
		{
			FS:   resourcesFS,
			Path: "resources/servicemesh/kserve-inferencegraph-authorizationpolicy.tmpl.yaml",
		},
	}

	rr.Templates = append(rr.Templates, templates...)
	return nil
}

func removeOwnershipFromUnmanagedResources(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve)", rr.Instance)
	}

	// unmanaged: remove ownerref & label to avoid GC
	for _, res := range rr.Resources {
		if shouldRemoveOwnerRefAndLabel(rr.DSCI.Spec.ServiceMesh, k.Spec.Serving, res) {
			err := getAndRemoveOwnerReferences(ctx, rr.Client, res, isKserveOwnerRef)
			if err != nil {
				return odherrors.NewStopErrorW(err)
			}
		}
	}

	return nil
}

func cleanUpTemplatedResources(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve)", rr.Instance)
	}

	logger := logf.FromContext(ctx)

	if rr.DSCI.Spec.ServiceMesh != nil {
		// servicemesh is set to Removed
		if rr.DSCI.Spec.ServiceMesh.ManagementState == operatorv1.Removed {
			// Delete servicemesh and serverless resources explicitly in
			// this case, since the GC won't collect them because the Kserve
			// CR generation hasn't changed.
			for _, res := range rr.Resources {
				if isForDependency("serverless")(&res) || isForDependency("servicemesh")(&res) {
					err := rr.Client.Delete(ctx, &res, client.PropagationPolicy(metav1.DeletePropagationForeground))
					if err != nil {
						if k8serr.IsNotFound(err) {
							continue
						}
						if errors.Is(err, &meta.NoKindMatchError{}) { // when CRD is missing,
							continue
						}
						return odherrors.NewStopErrorW(err)
					}
					logger.Info("Deleted", "kind", res.GetKind(), "name", res.GetName(), "namespace", res.GetNamespace())
				}
			}
		}
		// servicemesh is set to Removed or Unmanaged
		if rr.DSCI.Spec.ServiceMesh.ManagementState != operatorv1.Managed {
			if err := rr.RemoveResources(isForDependency("servicemesh")); err != nil {
				return odherrors.NewStopErrorW(err)
			}
		}
	}
	// serverless is Removed or Unamanged
	if k.Spec.Serving.ManagementState != operatorv1.Managed {
		if err := rr.RemoveResources(isForDependency("serverless")); err != nil {
			return odherrors.NewStopErrorW(err)
		}
	}

	return nil
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
	serviceClusterIPNone := true
	if k.Spec.RawDeploymentServiceConfig == componentApi.KserveRawHeaded {
		// As default is Headless, only set false here if Headed is explicitly set
		serviceClusterIPNone = false
	}
	if err := setServiceClusterIPNone(&kserveConfigMap, serviceClusterIPNone); err != nil {
		return err
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
