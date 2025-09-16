package dashboard

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	infraAPI "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

type DashboardHardwareProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DashboardHardwareProfileSpec `json:"spec"`
}

type DashboardHardwareProfileSpec struct {
	DisplayName  string                        `json:"displayName"`
	Enabled      bool                          `json:"enabled"`
	Description  string                        `json:"description,omitempty"`
	Tolerations  []corev1.Toleration           `json:"tolerations,omitempty"`
	Identifiers  []infraAPI.HardwareIdentifier `json:"identifiers,omitempty"`
	NodeSelector map[string]string             `json:"nodeSelector,omitempty"`
}

type DashboardHardwareProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DashboardHardwareProfile `json:"items"`
}

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

	if err := odhdeploy.ApplyParams(rr.Manifests[0].String(), "params.env", nil, extraParamsMap); err != nil {
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

func reconcileHardwareProfiles(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// If the dashboard HWP CRD doesn't exist, skip any migration logic
	dashHwpCRDExists, err := cluster.HasCRD(ctx, rr.Client, gvk.DashboardHardwareProfile)
	if err != nil {
		return odherrors.NewStopError("failed to check if %s CRD exists: %w", gvk.DashboardHardwareProfile, err)
	}
	if !dashHwpCRDExists {
		return nil
	}

	dashboardHardwareProfiles := &unstructured.UnstructuredList{}
	dashboardHardwareProfiles.SetGroupVersionKind(gvk.DashboardHardwareProfile)

	err = rr.Client.List(ctx, dashboardHardwareProfiles)
	if err != nil {
		return fmt.Errorf("failed to list dashboard hardware profiles: %w", err)
	}

	logger := log.FromContext(ctx)
	for _, hwprofile := range dashboardHardwareProfiles.Items {
		var dashboardHardwareProfile DashboardHardwareProfile

		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(hwprofile.Object, &dashboardHardwareProfile); err != nil {
			return fmt.Errorf("failed to convert dashboard hardware profile: %w", err)
		}

		infraHWP := &infraAPI.HardwareProfile{}
		err := rr.Client.Get(ctx, client.ObjectKey{
			Name:      dashboardHardwareProfile.Name,
			Namespace: dashboardHardwareProfile.Namespace,
		}, infraHWP)

		if k8serr.IsNotFound(err) {
			if err = createInfraHWP(ctx, rr, logger, &dashboardHardwareProfile); err != nil {
				return fmt.Errorf("failed to create infrastructure hardware profile: %w", err)
			}
			continue
		}

		if err != nil {
			return fmt.Errorf("failed to get infrastructure hardware profile: %w", err)
		}

		err = updateInfraHWP(ctx, rr, logger, &dashboardHardwareProfile, infraHWP)
		if err != nil {
			return fmt.Errorf("failed to update existing infrastructure hardware profile: %w", err)
		}
	}
	return nil
}

func createInfraHWP(ctx context.Context, rr *odhtypes.ReconciliationRequest, logger logr.Logger, dashboardhwp *DashboardHardwareProfile) error {
	annotations := make(map[string]string)
	maps.Copy(annotations, dashboardhwp.Annotations)

	annotations["opendatahub.io/migrated-from"] = fmt.Sprintf("hardwareprofiles.dashboard.opendatahub.io/%s", dashboardhwp.Name)
	annotations["opendatahub.io/display-name"] = dashboardhwp.Spec.DisplayName
	annotations["opendatahub.io/description"] = dashboardhwp.Spec.Description
	annotations["opendatahub.io/disabled"] = strconv.FormatBool(!dashboardhwp.Spec.Enabled)

	infraHardwareProfile := &infraAPI.HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:        dashboardhwp.Name,
			Namespace:   dashboardhwp.Namespace,
			Annotations: annotations,
		},
		Spec: infraAPI.HardwareProfileSpec{
			SchedulingSpec: &infraAPI.SchedulingSpec{
				SchedulingType: infraAPI.NodeScheduling,
				Node: &infraAPI.NodeSchedulingSpec{
					NodeSelector: dashboardhwp.Spec.NodeSelector,
					Tolerations:  dashboardhwp.Spec.Tolerations,
				},
			},
			Identifiers: dashboardhwp.Spec.Identifiers,
		},
	}

	if err := rr.Client.Create(ctx, infraHardwareProfile); err != nil {
		return err
	}

	logger.Info("successfully created infrastructure hardware profile", "name", infraHardwareProfile.GetName())
	return nil
}

func updateInfraHWP(ctx context.Context, rr *odhtypes.ReconciliationRequest, logger logr.Logger, dashboardhwp *DashboardHardwareProfile, infrahwp *infraAPI.HardwareProfile) error {
	if infrahwp.Annotations == nil {
		infrahwp.Annotations = make(map[string]string)
	}

	maps.Copy(infrahwp.Annotations, dashboardhwp.Annotations)

	infrahwp.Annotations["opendatahub.io/migrated-from"] = fmt.Sprintf("hardwareprofiles.dashboard.opendatahub.io/%s", dashboardhwp.Name)
	infrahwp.Annotations["opendatahub.io/display-name"] = dashboardhwp.Spec.DisplayName
	infrahwp.Annotations["opendatahub.io/description"] = dashboardhwp.Spec.Description
	infrahwp.Annotations["opendatahub.io/disabled"] = strconv.FormatBool(!dashboardhwp.Spec.Enabled)

	infrahwp.Spec.SchedulingSpec = &infraAPI.SchedulingSpec{
		SchedulingType: infraAPI.NodeScheduling,
		Node: &infraAPI.NodeSchedulingSpec{
			NodeSelector: dashboardhwp.Spec.NodeSelector,
			Tolerations:  dashboardhwp.Spec.Tolerations,
		},
	}
	infrahwp.Spec.Identifiers = dashboardhwp.Spec.Identifiers

	if err := rr.Client.Update(ctx, infrahwp); err != nil {
		return fmt.Errorf("failed to update infrastructure hardware profile: %w", err)
	}

	logger.Info("successfully updated infrastructure hardware profile", "name", infrahwp.GetName())
	return nil
}
