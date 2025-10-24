// +kubebuilder:skip
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
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/validation"
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

func Initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// Validate required fields
	if rr.Client == nil {
		return errors.New("client is required but was nil")
	}

	if rr.DSCI == nil {
		return errors.New("DSCI is required but was nil")
	}

	// Validate Instance type
	_, ok := rr.Instance.(*componentApi.Dashboard)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Dashboard", rr.Instance)
	}

	rr.Manifests = []odhtypes.ManifestInfo{DefaultManifestInfo(rr.Release.Name)}

	return nil
}

func setKustomizedParams(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	extraParamsMap, err := computeKustomizeVariable(ctx, rr.Client, rr.Release.Name)
	if err != nil {
		return fmt.Errorf("failed to set variable for url, section-title etc: %w", err)
	}

	if len(rr.Manifests) == 0 {
		return errors.New("no manifests available")
	}

	if err := odhdeploy.ApplyParams(rr.Manifests[0].String(), "params.env", nil, extraParamsMap); err != nil {
		return fmt.Errorf("failed to update params.env from %s : %w", rr.Manifests[0].String(), err)
	}
	return nil
}

// resourceExists checks if a resource with the same Group/Version/Kind/Namespace/Name
// already exists in the ReconciliationRequest's Resources slice.
func resourceExists(resources []unstructured.Unstructured, candidate client.Object) bool {
	if candidate == nil {
		return false
	}

	candidateName := candidate.GetName()
	candidateNamespace := candidate.GetNamespace()
	candidateGVK := candidate.GetObjectKind().GroupVersionKind()

	for _, existing := range resources {
		if existing.GetName() == candidateName &&
			existing.GetNamespace() == candidateNamespace &&
			existing.GroupVersionKind() == candidateGVK {
			return true
		}
	}

	return false
}

func ConfigureDependencies(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	if rr.Release.Name == cluster.OpenDataHub {
		return nil
	}

	// Check for nil client before proceeding
	if rr.Client == nil {
		return errors.New("client cannot be nil")
	}

	// Check for nil DSCI before accessing its properties
	if rr.DSCI == nil {
		return errors.New("DSCI cannot be nil")
	}

	// Validate namespace before attempting to create resources
	if err := validation.ValidateNamespace(rr.DSCI.Spec.ApplicationsNamespace); err != nil {
		return fmt.Errorf("invalid namespace: %w", err)
	}

	// Create the anaconda secret resource
	anacondaSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      AnacondaSecretName,
			Namespace: rr.DSCI.Spec.ApplicationsNamespace,
		},
		Type: corev1.SecretTypeOpaque,
	}

	// Check if the resource already exists to avoid duplicates
	if resourceExists(rr.Resources, anacondaSecret) {
		return nil
	}

	err := rr.AddResources(anacondaSecret)
	if err != nil {
		return fmt.Errorf("failed to create access-secret for anaconda: %w", err)
	}

	return nil
}

func UpdateStatus(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	if rr == nil {
		return errors.New("reconciliation request is nil")
	}

	if rr.Instance == nil {
		return errors.New("instance is nil")
	}

	if rr.Client == nil {
		return errors.New("client is nil")
	}

	if rr.DSCI == nil {
		return errors.New("DSCI is nil")
	}

	d, ok := rr.Instance.(*componentApi.Dashboard)
	if !ok {
		return errors.New("instance is not of type *componentApi.Dashboard")
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
		host := resources.IngressHost(rl.Items[0])
		if host != "" {
			d.Status.URL = "https://" + host
		}
	}

	return nil
}

func ReconcileHardwareProfiles(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	if rr.Client == nil {
		return errors.New("client is nil")
	}

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
		if err := ProcessHardwareProfile(ctx, rr, logger, hwprofile); err != nil {
			return err
		}
	}
	return nil
}

// ProcessHardwareProfile processes a single dashboard hardware profile.
func ProcessHardwareProfile(ctx context.Context, rr *odhtypes.ReconciliationRequest, logger logr.Logger, hwprofile unstructured.Unstructured) error {
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
		if err = CreateInfraHWP(ctx, rr, logger, &dashboardHardwareProfile); err != nil {
			return fmt.Errorf("failed to create infrastructure hardware profile: %w", err)
		}
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to get infrastructure hardware profile: %w", err)
	}

	err = UpdateInfraHWP(ctx, rr, logger, &dashboardHardwareProfile, infraHWP)
	if err != nil {
		return fmt.Errorf("failed to update existing infrastructure hardware profile: %w", err)
	}

	return nil
}

func CreateInfraHWP(ctx context.Context, rr *odhtypes.ReconciliationRequest, logger logr.Logger, dashboardhwp *DashboardHardwareProfile) error {
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

func UpdateInfraHWP(
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
