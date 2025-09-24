package kserve

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	featuresv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

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

func customizeKserveConfigMap(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve)", rr.Instance)
	}

	kserveConfigMap := corev1.ConfigMap{}
	cmidx, err := getIndexedResource(rr.Resources, &kserveConfigMap, gvk.ConfigMap, kserveConfigMapName)
	if err != nil {
		return err
	}

	//nolint:staticcheck
	serviceClusterIPNone := true
	if k.Spec.RawDeploymentServiceConfig == componentApi.KserveRawHeaded {
		// As default is Headless, only set false here if Headed is explicitly set
		serviceClusterIPNone = false
	}

	if err := updateInferenceCM(&kserveConfigMap, serviceClusterIPNone); err != nil {
		return err
	}

	if err = replaceResourceAtIndex(rr.Resources, cmidx, &kserveConfigMap); err != nil {
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

	if err = replaceResourceAtIndex(rr.Resources, deployidx, &kserveDeployment); err != nil {
		return err
	}

	return nil
}
