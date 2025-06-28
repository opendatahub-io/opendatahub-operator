package dashboard

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	infraAPI "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = []odhtypes.ManifestInfo{defaultManifestInfo(rr.Release.Name)}

	return nil
}

func devFlags(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	dashboard, ok := rr.Instance.(*componentApi.Dashboard)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Dashboard)", rr.Instance)
	}

	if dashboard.Spec.DevFlags == nil {
		return nil
	}
	// Implement devflags support logic
	// If dev flags are set, update default manifests path
	if len(dashboard.Spec.DevFlags.Manifests) != 0 {
		manifestConfig := dashboard.Spec.DevFlags.Manifests[0]
		if err := odhdeploy.DownloadManifests(ctx, ComponentName, manifestConfig); err != nil {
			return err
		}
		if manifestConfig.SourcePath != "" {
			rr.Manifests[0].Path = odhdeploy.DefaultManifestPath
			rr.Manifests[0].ContextDir = ComponentName
			rr.Manifests[0].SourcePath = manifestConfig.SourcePath
		}
	}

	return nil
}

func customizeResources(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	for i := range rr.Resources {
		if rr.Resources[i].GroupVersionKind() == gvk.OdhDashboardConfig {
			// mark the resource as not supposed to be managed by the operator
			resources.SetAnnotation(&rr.Resources[i], annotations.ManagedByODHOperator, "false")
			break
		}
	}

	return nil
}

func setKustomizedParams(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	extraParamsMap, err := computeKustomizeVariable(ctx, rr.Client, rr.Release.Name, &rr.DSCI.Spec)
	if err != nil {
		return errors.New("failed to set variable for url, section-title etc")
	}

	if err := odhdeploy.ApplyParams(rr.Manifests[0].String(), nil, extraParamsMap); err != nil {
		return fmt.Errorf("failed to update params.env from %s : %w", rr.Manifests[0].String(), err)
	}
	return nil
}

func configureDependencies(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	if rr.Release.Name == cluster.OpenDataHub {
		return nil
	}

	err := rr.AddResources(&corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "anaconda-ce-access",
			Namespace: rr.DSCI.Spec.ApplicationsNamespace,
		},
		Type: corev1.SecretTypeOpaque,
	})

	if err != nil {
		return fmt.Errorf("failed to create access-secret for anaconda: %w", err)
	}

	return nil
}

func updateStatus(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	d, ok := rr.Instance.(*componentApi.Dashboard)
	if !ok {
		return errors.New("instance is not of type *odhTypes.Dashboard")
	}

	// url
	rl := routev1.RouteList{}
	err := rr.Client.List(
		ctx,
		&rl,
		client.InNamespace(rr.DSCI.Spec.ApplicationsNamespace),
		client.MatchingLabels(map[string]string{
			labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
		}),
	)

	if err != nil {
		return fmt.Errorf("failed to list routes: %w", err)
	}

	d.Status.URL = ""
	if len(rl.Items) == 1 {
		d.Status.URL = resources.IngressHost(rl.Items[0])
	}

	return nil
}

func migrateHardwareProfiles(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	dashboardHardwareProfiles := &unstructured.UnstructuredList{}
	dashboardHardwareProfiles.SetGroupVersionKind(gvk.DashboardHardwareProfile.GroupVersion().WithKind("HardwareProfileList"))

	err := rr.Client.List(ctx, dashboardHardwareProfiles)
	if err != nil {
		return fmt.Errorf("failed to list dashboard hardware profiles: %w", err)
	}

	logger := log.FromContext(ctx)
	for _, hwprofile := range dashboardHardwareProfiles.Items {
		annotations := hwprofile.GetAnnotations()
		if _, migrated := annotations["migrated-to"]; !migrated {
			logger.Info("Found unmigrated dashboard HardwareProfile", "name", hwprofile.GetName())

			var dashboardHardwareProfile infraAPI.DashboardHardwareProfile
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(hwprofile.Object, &dashboardHardwareProfile); err != nil {
				return fmt.Errorf("failed to convert dashboard hardware profile: %w", err)
			}

			if err = createInfraHardwareProfile(ctx, rr, &dashboardHardwareProfile); err != nil {
				return fmt.Errorf("failed to create infra hardware profile: %w", err)
			}

			if annotations == nil {
				annotations = make(map[string]string)
			}
			annotations["migrated-to"] = fmt.Sprintf("hardwareprofiles.infrastructure.opendatahub.io/%s", hwprofile.GetName())
			hwprofile.SetAnnotations(annotations)

			if err := rr.Client.Update(ctx, &hwprofile); err != nil {
				return fmt.Errorf("failed to update dashboard hardware profile with migration annotation: %w", err)
			}
			logger.Info("migrated completed for dashboard hardware profile", "name", hwprofile.GetName())
		}
	}

	return nil
}

func createInfraHardwareProfile(ctx context.Context, rr *odhtypes.ReconciliationRequest, dashboardhwprofile *infraAPI.DashboardHardwareProfile) error {
	infraHardwareProfile := &infraAPI.HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dashboardhwprofile.Name,
			Namespace: dashboardhwprofile.Namespace,
			Annotations: map[string]string{
				"migrated-from":               fmt.Sprintf("hardwareprofiles.dashboard.opendatahub.io/%s", dashboardhwprofile.Name),
				"opendatahub.io/display-name": dashboardhwprofile.Spec.DisplayName,
				"opendatahub.io/description":  dashboardhwprofile.Spec.Description,
				"opendatahub.io/disabled":     strconv.FormatBool(!dashboardhwprofile.Spec.Enabled),
			},
		},
		Spec: infraAPI.HardwareProfileSpec{
			SchedulingSpec: &infraAPI.SchedulingSpec{
				SchedulingType: infraAPI.NodeScheduling,
				Node: &infraAPI.NodeSchedulingSpec{
					NodeSelector: dashboardhwprofile.Spec.NodeSelector,
					Tolerations:  dashboardhwprofile.Spec.Tolerations,
				},
			},
			Identifiers: dashboardhwprofile.Spec.Identifiers,
		},
	}

	if err := rr.Client.Create(ctx, infraHardwareProfile); err != nil {
		return err
	}

	return nil
}
