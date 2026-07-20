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
	rbacv1 "k8s.io/api/rbac/v1"
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
	if rr.SkipDeploy {
		return nil
	}

	// Check if PersesDashboard CRD exists in either v1alpha2 or v1alpha1 (COO installed)
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

func ensureNamespacedRBAC(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	logger := log.FromContext(ctx)

	appNamespace, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return err
	}

	saName := "odh-dashboard"
	if rr.Release.Name == cluster.SelfManagedRhoai || rr.Release.Name == cluster.ManagedRhoai {
		saName = "rhods-dashboard"
	}

	notebooksNS, err := resolveNotebooksNamespace(ctx, rr)
	if err != nil {
		return fmt.Errorf("failed to resolve notebooks namespace: %w", err)
	}
	if notebooksNS != "" {
		logger.V(1).Info("ensuring Dashboard RBAC in notebooks namespace", "namespace", notebooksNS)
		if err := addNamespacedRBAC(rr, saName, appNamespace, notebooksNS, "notebooks", notebooksRBACRules()); err != nil {
			return fmt.Errorf("failed to add notebooks RBAC resources: %w", err)
		}
	}

	modelRegistryNS, err := resolveModelRegistryNamespace(ctx, rr)
	if err != nil {
		return fmt.Errorf("failed to resolve model-registry namespace: %w", err)
	}
	if modelRegistryNS != "" {
		logger.V(1).Info("ensuring Dashboard RBAC in model-registry namespace", "namespace", modelRegistryNS)
		if err := addNamespacedRBAC(rr, saName, appNamespace, modelRegistryNS, "model-registries", modelRegistryRBACRules()); err != nil {
			return fmt.Errorf("failed to add model-registry RBAC resources: %w", err)
		}
	}

	return nil
}

func resolveNotebooksNamespace(ctx context.Context, rr *odhtypes.ReconciliationRequest) (string, error) {
	logger := log.FromContext(ctx)

	wb := &componentApi.Workbenches{}
	err := rr.Client.Get(ctx, client.ObjectKey{Name: componentApi.WorkbenchesInstanceName}, wb)
	if err != nil {
		if k8serr.IsNotFound(err) {
			logger.V(1).Info("Workbenches CR not found, skipping notebooks RBAC")
			return "", nil
		}
		return "", fmt.Errorf("failed to get Workbenches CR: %w", err)
	}

	ns := wb.Spec.WorkbenchNamespace
	if ns == "" {
		switch rr.Release.Name {
		case cluster.SelfManagedRhoai, cluster.ManagedRhoai:
			ns = cluster.DefaultNotebooksNamespaceRHOAI
		case cluster.OpenDataHub:
			ns = cluster.DefaultNotebooksNamespaceODH
		}
	}

	exists, err := cluster.NamespaceExists(ctx, rr.Client, ns)
	if err != nil {
		return "", fmt.Errorf("failed to check notebooks namespace %q: %w", ns, err)
	}
	if !exists {
		logger.V(1).Info("notebooks namespace not found, skipping RBAC", "namespace", ns)
		return "", nil
	}

	return ns, nil
}

func resolveModelRegistryNamespace(ctx context.Context, rr *odhtypes.ReconciliationRequest) (string, error) {
	logger := log.FromContext(ctx)

	mr := &componentApi.ModelRegistry{}
	err := rr.Client.Get(ctx, client.ObjectKey{Name: componentApi.ModelRegistryInstanceName}, mr)
	if err != nil {
		if k8serr.IsNotFound(err) {
			logger.V(1).Info("ModelRegistry CR not found, skipping model-registry RBAC")
			return "", nil
		}
		return "", fmt.Errorf("failed to get ModelRegistry CR: %w", err)
	}

	ns := mr.Spec.RegistriesNamespace
	if ns == "" {
		logger.V(1).Info("ModelRegistry registriesNamespace is empty, skipping RBAC")
		return "", nil
	}

	exists, err := cluster.NamespaceExists(ctx, rr.Client, ns)
	if err != nil {
		return "", fmt.Errorf("failed to check model-registry namespace %q: %w", ns, err)
	}
	if !exists {
		logger.V(1).Info("model-registry namespace not found, skipping RBAC", "namespace", ns)
		return "", nil
	}

	return ns, nil
}

func addNamespacedRBAC(rr *odhtypes.ReconciliationRequest, saName, saNamespace, targetNamespace, roleSuffix string, rules []rbacv1.PolicyRule) error {
	roleName := saName + "-" + roleSuffix

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: targetNamespace,
		},
		Rules: rules,
	}

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: targetNamespace,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     roleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      saName,
				Namespace: saNamespace,
			},
		},
	}

	return rr.AddResources(role, rb)
}

func notebooksRBACRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"persistentvolumeclaims"},
			Verbs:     []string{"create", "get"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"configmaps"},
			Verbs:     []string{"create", "get", "update"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"secrets"},
			Verbs:     []string{"create", "get", "update"},
		},
	}
}

func modelRegistryRBACRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"secrets"},
			Verbs:     []string{"delete", "list", "patch"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"configmaps"},
			Verbs:     []string{"create", "list"},
		},
	}
}
