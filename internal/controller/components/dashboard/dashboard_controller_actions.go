package dashboard

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// rfc1123NamespaceRegex is a precompiled regex for validating namespace names.
// according to RFC1123 DNS label rules.
var rfc1123NamespaceRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

type DashboardHardwareProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DashboardHardwareProfileSpec `json:"spec"`
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
	Items           []DashboardHardwareProfile `json:"items"`
}

// initialize validates the reconciliation request and prepares default manifest info.
// It returns an error if rr.Client or rr.DSCI are nil or if rr.Instance is not a *componentApi.Dashboard; on success it sets rr.Manifests to the DefaultManifestInfo for rr.Release.Name and returns nil.
func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
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

// devFlags applies development manifest flags from the Dashboard spec to the reconciliation request.
// If DevFlags is nil the function is a no-op. When DevFlags.Manifests is provided the first
// manifest entry will be downloaded; if that entry specifies a SourcePath the function updates
// rr.Manifests[0] to use the default manifest path, sets ContextDir to the dashboard component name,
// and assigns SourcePath accordingly. Returns an error if rr.Instance is not a *componentApi.Dashboard
// or if manifest download fails.
func devFlags(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	dashboard, ok := rr.Instance.(*componentApi.Dashboard)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Dashboard", rr.Instance)
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

// CustomizeResources inspects the reconciliation request's resources and, if an
// OdhDashboardConfig resource is found, sets the opendatahub.io/managed-by-odh-operator
// annotation to "false" so the operator will not manage that resource.
func CustomizeResources(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	for i := range rr.Resources {
		if rr.Resources[i].GroupVersionKind() == gvk.OdhDashboardConfig {
			// mark the resource as not supposed to be managed by the operator
			resources.SetAnnotation(&rr.Resources[i], annotations.ManagedByODHOperator, "false")
			break
		}
	}

	return nil
}

// SetKustomizedParams computes kustomize parameter substitutions for the current release
// and applies them to the first manifest's `params.env`.
//
// It returns an error if computing the variables fails, if no manifests are available,
// or if applying the parameters to the manifest fails.
func SetKustomizedParams(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	extraParamsMap, err := ComputeKustomizeVariable(ctx, rr.Client, rr.Release.Name, &rr.DSCI.Spec)
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

// validateNamespace validates that a namespace name conforms to RFC1123 DNS label rules
// validateNamespace verifies that the provided namespace is non-empty, does not exceed 63 characters, and matches the RFC1123 DNS label pattern.
// It returns an error when the namespace is empty, longer than 63 characters, or contains characters or placement that violate RFC1123 (must be lowercase alphanumeric or '-', and must start and end with an alphanumeric character).
func validateNamespace(namespace string) error {
	if namespace == "" {
		return errors.New("namespace cannot be empty")
	}

	// Check length constraint (max 63 characters)
	if len(namespace) > 63 {
		return fmt.Errorf("namespace '%s' exceeds maximum length of 63 characters (length: %d)", namespace, len(namespace))
	}

	// RFC1123 DNS label regex: must start and end with alphanumeric character,
	// can contain alphanumeric characters and hyphens in the middle
	// Pattern: ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$
	if !rfc1123NamespaceRegex.MatchString(namespace) {
		return fmt.Errorf("namespace '%s' must be lowercase and conform to RFC1123 DNS label rules: "+
			"a–z, 0–9, '-', start/end with alphanumeric", namespace)
	}

	return nil
}

// resourceExists checks if a resource with the same Group/Version/Kind/Namespace/Name
// resourceExists reports whether resources contains an object with the same
// group-version-kind, namespace, and name as the provided candidate.
// If candidate is nil, it returns false.
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

// configureDependencies ensures required cluster-scoped dependencies for the dashboard are present.
// It no-ops when the release name is OpenDataHub, validates the reconciliation request and the
// configured ApplicationsNamespace, and creates an `anaconda-access-secret` Secret in that namespace
// if it does not already exist. It returns an error for nil client/DSCI, invalid namespace names,
// or failures while adding the secret.
func configureDependencies(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
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
	if err := validateNamespace(rr.DSCI.Spec.ApplicationsNamespace); err != nil {
		return fmt.Errorf("invalid namespace: %w", err)
	}

	// Create the anaconda secret resource
	anacondaSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "anaconda-access-secret",
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

// updateStatus updates the Dashboard's Status.URL by discovering a matching OpenShift Route
// in the DSCI's applications namespace and setting the URL to "https://<host>" when a single
// route with a resolvable host is present.
//
// The function validates that the reconciliation request, its Client, DSCI, and Instance are
// non-nil and that Instance is a *componentApi.Dashboard. It lists Route resources in the
// DSCI.Spec.ApplicationsNamespace that carry the PlatformPartOf label for the dashboard.
// If exactly one route is found and an ingress host can be derived, the dashboard status URL
// is set to "https://<host>"; otherwise the status URL is cleared.
// Returns an error if validation fails or the route listing call fails.
func updateStatus(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
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

// ReconcileHardwareProfiles ensures DashboardHardwareProfile CR instances are processed and synchronized with infra HardwareProfile resources.
// 
// It verifies the Kubernetes client is present, skips work if the DashboardHardwareProfile CRD is absent, lists all DashboardHardwareProfile
// resources, and processes each entry with ProcessHardwareProfile. Returns an error if the client is nil, the CRD check or list operation fails,
// or processing any individual profile fails.
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

// ProcessHardwareProfile converts the provided unstructured DashboardHardwareProfile into a typed DashboardHardwareProfile and ensures an infrav1.HardwareProfile with the same name and namespace exists: it creates the infra resource if not found or updates it if present.
// It returns an error when conversion, retrieval, creation, or update fails.
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

// CreateInfraHWP creates an infrav1.HardwareProfile that corresponds to the provided
// DashboardHardwareProfile and persists it using the reconciliation request client.
//
// The created HardwareProfile copies annotations from the Dashboard resource and sets
// migration and metadata annotations (`opendatahub.io/migrated-from`, `opendatahub.io/display-name`,
// `opendatahub.io/description`) and a `opendatahub.io/disabled` annotation derived from
// DashboardHardwareProfile.Spec.Enabled. The spec of the infra HardwareProfile uses node
// scheduling with the DashboardHardwareProfile's NodeSelector, Tolerations, and Identifiers.
//
// Returns an error if creating the infra HardwareProfile via rr.Client fails.
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

// UpdateInfraHWP synchronizes an infrastructure HardwareProfile with the corresponding
// DashboardHardwareProfile and persists the change.
//
// It copies annotations from the dashboard profile, sets migration/display/description/disabled
// annotations, updates scheduling (node selector and tolerations) and identifiers to match
// the dashboard profile, and then updates the infra resource via the reconciliation client's Update.
//
// Returns an error if persisting the updated infra HardwareProfile fails.
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
