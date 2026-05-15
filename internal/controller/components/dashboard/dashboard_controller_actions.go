// +kubebuilder:skip
package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/conversion"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/plugins"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

type DashboardHardwareProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec DashboardHardwareProfileSpec `json:"spec"`
}

type DashboardHardwareProfileSpec struct {
	DisplayName  string                       `json:"displayName"`
	Enabled      bool                         `json:"enabled"`
	Description  string                       `json:"description,omitempty"`
	Tolerations  []corev1.Toleration          `json:"tolerations,omitempty"`
	Identifiers  []infrav1.HardwareIdentifier `json:"identifiers,omitempty"`
	NodeSelector map[string]string            `json:"nodeSelector,omitempty"`
}

type DashboardHardwareProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []DashboardHardwareProfile `json:"items"`
}

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error { //nolint:unparam
	rr.Manifests = []odhtypes.ManifestInfo{defaultManifestInfo(rr.ManifestsBasePath, rr.Release.Name)}

	return nil
}

func deployObservabilityManifests(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// Check if PersesDashboard CRD exists, preferring v1alpha2 over v1alpha1 (COO installed)
	v2Exists, err := cluster.HasCRD(ctx, rr.Client, gvk.PersesDashboardV1Alpha2)
	if err != nil {
		return odherrors.NewStopError("failed to check if %s CRD exists: %w", gvk.PersesDashboardV1Alpha2, err)
	}
	if !v2Exists {
		v1Exists, err := cluster.HasCRD(ctx, rr.Client, gvk.PersesDashboardV1Alpha1)
		if err != nil {
			return odherrors.NewStopError("failed to check if %s CRD exists: %w", gvk.PersesDashboardV1Alpha1, err)
		}
		if !v1Exists {
			return nil
		}
	}

	// Get the monitoring namespace from DSCI with platform-specific fallback
	monitoringNamespace, err := cluster.MonitoringNamespace(ctx, rr.Client)
	if err != nil {
		if rr.Release.Name == cluster.OpenDataHub {
			monitoringNamespace = cluster.DefaultMonitoringNamespaceODH
		} else {
			monitoringNamespace = cluster.DefaultMonitoringNamespaceRHOAI
		}
	}

	// Safety check: do not deploy if monitoring namespace is empty
	if monitoringNamespace == "" {
		return nil
	}

	manifestPath := observabilityManifestInfo(rr.ManifestsBasePath, rr.Release.Name).String()

	// When v1alpha2 is available, transform manifests to use the preferred API version
	// to avoid deprecation warnings and SSA conversion conflicts that cause infinite reconciliation
	if v2Exists {
		if err := deployManifestsWithPersesVersionUpgrade(ctx, rr.Client, rr.Instance, manifestPath, monitoringNamespace); err != nil {
			return fmt.Errorf("failed to deploy observability manifests: %w", err)
		}
		return nil
	}

	err = odhdeploy.DeployManifestsFromPath(
		ctx,
		rr.Client,
		rr.Instance, // owner for GC
		manifestPath,
		monitoringNamespace, // deploy to monitoring namespace
		ComponentName,       // "dashboard"
		true,                // enabled
	)
	if err != nil {
		return fmt.Errorf("failed to deploy observability manifests: %w", err)
	}

	return nil
}

// deployManifestsWithPersesVersionUpgrade renders kustomize manifests and upgrades
// any perses.dev/v1alpha1 resources to v1alpha2 before applying them.
func deployManifestsWithPersesVersionUpgrade(
	ctx context.Context,
	cli client.Client,
	owner metav1.Object,
	manifestPath string,
	namespace string,
) error {
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fs := filesys.MakeFsOnDisk()

	if _, err := os.Stat(filepath.Join(manifestPath, "kustomization.yaml")); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		manifestPath = filepath.Join(manifestPath, "default")
	}

	resMap, err := k.Run(fs, manifestPath)
	if err != nil {
		return err
	}

	nsPlugin := plugins.CreateNamespaceApplierPlugin(namespace)
	if err := nsPlugin.Transform(resMap); err != nil {
		return fmt.Errorf("failed applying namespace plugin: %w", err)
	}

	resourceLabels := map[string]string{
		labels.ODH.Component(ComponentName): "true",
		labels.K8SCommon.PartOf:             ComponentName,
	}
	labelsPlugin := plugins.CreateSetLabelsPlugin(resourceLabels)
	if err := labelsPlugin.Transform(resMap); err != nil {
		return fmt.Errorf("failed applying labels plugin: %w", err)
	}

	for _, res := range resMap.Resources() {
		obj, err := conversion.ResourceToUnstructured(res)
		if err != nil {
			return fmt.Errorf("failed to convert resource to unstructured: %w", err)
		}

		upgradePersesToV1Alpha2(obj)

		if err := ctrl.SetControllerReference(owner, obj, cli.Scheme()); err != nil {
			return fmt.Errorf("failed to set controller reference for %s/%s: %w", obj.GetKind(), obj.GetName(), err)
		}

		data, err := json.Marshal(obj)
		if err != nil {
			return fmt.Errorf("failed to marshal resource: %w", err)
		}

		target := obj.DeepCopy()
		if err := cli.Patch(ctx, target, client.RawPatch(k8stypes.ApplyPatchType, data),
			client.ForceOwnership, client.FieldOwner(owner.GetName())); err != nil {
			return fmt.Errorf("failed to apply resource %s/%s: %w", obj.GetKind(), obj.GetName(), err)
		}
	}

	return nil
}

// upgradePersesToV1Alpha2 transforms a perses.dev/v1alpha1 resource to v1alpha2.
// For PersesDashboard, this also restructures the spec to nest content under spec.config.
func upgradePersesToV1Alpha2(obj *unstructured.Unstructured) {
	if obj.GetAPIVersion() != "perses.dev/v1alpha1" {
		return
	}

	obj.SetAPIVersion("perses.dev/v1alpha2")

	if obj.GetKind() != "PersesDashboard" {
		return
	}

	spec, found, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil || !found || len(spec) == 0 {
		return
	}

	updatePersesRefs(spec)

	obj.Object["spec"] = map[string]interface{}{
		"config": spec,
	}
}

// updatePersesRefs rewrites $ref paths from #/spec/... to #/spec/config/... to match
// the v1alpha2 PersesDashboard spec structure.
func updatePersesRefs(obj interface{}) {
	switch v := obj.(type) {
	case map[string]interface{}:
		for key, val := range v {
			if key == "$ref" {
				if s, ok := val.(string); ok && strings.HasPrefix(s, "#/spec/") {
					v[key] = "#/spec/config/" + strings.TrimPrefix(s, "#/spec/")
				}
			} else {
				updatePersesRefs(val)
			}
		}
	case []interface{}:
		for _, item := range v {
			updatePersesRefs(item)
		}
	}
}

func setKustomizedParams(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	extraParamsMap, err := computeKustomizeVariable(rr, rr.Release.Name)
	if err != nil {
		return fmt.Errorf("failed to set variable for url, section-title etc: %w", err)
	}

	if err := odhdeploy.ApplyParams(rr.Manifests[0].String(), "params.env", nil, extraParamsMap); err != nil {
		return fmt.Errorf("failed to update params.env from %s : %w", rr.Manifests[0].String(), err)
	}

	return nil
}

func configureDependencies(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	if rr.Release.Name == cluster.OpenDataHub {
		return nil
	}

	// Fetch application namespace from DSCI.
	appNamespace, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return err
	}

	err = rr.AddResources(&corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "anaconda-ce-access",
			Namespace: appNamespace,
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

	// Fetch application namespace from DSCI.
	appNamespace, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return err
	}

	// url
	rl := routev1.RouteList{}
	err = rr.Client.List(
		ctx,
		&rl,
		client.InNamespace(appNamespace),
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

		infraHWP := &infrav1.HardwareProfile{}
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

	infraHardwareProfile := &infrav1.HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:        dashboardhwp.Name,
			Namespace:   dashboardhwp.Namespace,
			Annotations: annotations,
		},
		Spec: infrav1.HardwareProfileSpec{
			SchedulingSpec: &infrav1.SchedulingSpec{
				SchedulingType: infrav1.NodeScheduling,
				Node: &infrav1.NodeSchedulingSpec{
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

func updateInfraHWP(
	ctx context.Context, rr *odhtypes.ReconciliationRequest, logger logr.Logger, dashboardhwp *DashboardHardwareProfile, infrahwp *infrav1.HardwareProfile) error {
	if infrahwp.Annotations == nil {
		infrahwp.Annotations = make(map[string]string)
	}

	maps.Copy(infrahwp.Annotations, dashboardhwp.Annotations)

	infrahwp.Annotations["opendatahub.io/migrated-from"] = fmt.Sprintf("hardwareprofiles.dashboard.opendatahub.io/%s", dashboardhwp.Name)
	infrahwp.Annotations["opendatahub.io/display-name"] = dashboardhwp.Spec.DisplayName
	infrahwp.Annotations["opendatahub.io/description"] = dashboardhwp.Spec.Description
	infrahwp.Annotations["opendatahub.io/disabled"] = strconv.FormatBool(!dashboardhwp.Spec.Enabled)

	infrahwp.Spec.SchedulingSpec = &infrav1.SchedulingSpec{
		SchedulingType: infrav1.NodeScheduling,
		Node: &infrav1.NodeSchedulingSpec{
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
