/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package components

import (
	"context"
	"errors"
	"fmt"
	"github.com/opendatahub-io/opendatahub-operator/v2/apis/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	v1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"path/filepath"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	ComponentNameUpstream = "dashboard"
	PathUpstream          = deploy.DefaultManifestPath + "/" + ComponentNameUpstream + "/odh"

	ComponentNameDownstream = "rhods-dashboard"
	PathDownstream          = deploy.DefaultManifestPath + "/" + ComponentNameUpstream + "/rhoai"
	PathSelfDownstream      = PathDownstream + "/onprem"
	PathManagedDownstream   = PathDownstream + "/addon"
	OverridePath            = ""
	DefaultPath             = ""
)

var (
	DSCIInstance *dsciv1.DSCInitialization
	DSCInstance  *dscv1.DataScienceCluster
)

// DashboardReconciler reconciles a Dashboard object
type DashboardReconciler struct {
	*BaseReconciler
	// Any dashboard specific reconcile request fields can be added below
}

func NewDashboardReconciler(mgr ctrl.Manager) *DashboardReconciler {
	r := &DashboardReconciler{
		BaseReconciler: NewBaseReconciler(mgr, "dashboard"),
	}

	// Add Dashboard-specific actions
	r.AddAction(&InitializeAction{})
	r.AddAction(&SupportDevFlagsAction{})
	r.AddAction(&CleanupOAuthClientAction{})
	r.AddAction(&DeployComponentAction{})
	r.AddAction(&UpdateStatusAction{})

	return r
}

func CreateDashboardInstance(ctx context.Context, cli client.Client, dsci *dsciv1.DSCInitialization, dsc *dscv1.DataScienceCluster) error {
	// Set DSC and DSCI instances
	DSCIInstance = dsci
	DSCInstance = dsc
	dashboardInstance := &componentsv1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dashboard",
		},
		Spec: componentsv1.DashboardSpec{
			DSCDashboard: componentsv1.DSCDashboard{
				Component: components.Component{
					ManagementState: v1.Managed,
					DevFlags:        dsc.Spec.Components.Dashboard.DevFlags,
				},
			},
		},
	}
	if err := ctrl.SetControllerReference(dsc, dashboardInstance, cli.Scheme()); err != nil {
		return err
	}

	if err := cli.Create(ctx, dashboardInstance); err != nil {
		if k8serr.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	return nil
}

//+kubebuilder:rbac:groups=components.opendatahub.io,resources=dashboards,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=components.opendatahub.io,resources=dashboards/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=components.opendatahub.io,resources=dashboards/finalizers,verbs=update
// +kubebuilder:rbac:groups="opendatahub.io",resources=odhdashboardconfigs,verbs=create;get;patch;watch;update;delete;list
// +kubebuilder:rbac:groups="console.openshift.io",resources=odhquickstarts,verbs=create;get;patch;list;delete
// +kubebuilder:rbac:groups="dashboard.opendatahub.io",resources=odhdocuments,verbs=create;get;patch;list;delete
// +kubebuilder:rbac:groups="dashboard.opendatahub.io",resources=odhapplications,verbs=create;get;patch;list;delete
// +kubebuilder:rbac:groups="dashboard.opendatahub.io",resources=acceleratorprofiles,verbs=create;get;patch;list;delete

// +kubebuilder:rbac:groups="operators.coreos.com",resources=clusterserviceversions,verbs=get;list;watch;delete;update
// +kubebuilder:rbac:groups="operators.coreos.com",resources=customresourcedefinitions,verbs=create;get;patch;delete
// +kubebuilder:rbac:groups="operators.coreos.com",resources=subscriptions,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="operators.coreos.com",resources=operatorconditions,verbs=get;list;watch
// +kubebuilder:rbac:groups="user.openshift.io",resources=groups,verbs=get;create;list;watch;patch;delete
// +kubebuilder:rbac:groups="console.openshift.io",resources=consolelinks,verbs=create;get;patch;delete
// +kubebuilder:rbac:groups="authorization.openshift.io",resources=roles,verbs=*
// +kubebuilder:rbac:groups="authorization.openshift.io",resources=rolebindings,verbs=*
// +kubebuilder:rbac:groups="authorization.openshift.io",resources=clusterroles,verbs=*
// +kubebuilder:rbac:groups="authorization.openshift.io",resources=clusterrolebindings,verbs=*

// +kubebuilder:rbac:groups="argoproj.io",resources=workflows,verbs=*

// +kubebuilder:rbac:groups="apps",resources=statefulsets,verbs=*

// +kubebuilder:rbac:groups="apps",resources=replicasets,verbs=*

// +kubebuilder:rbac:groups="apps",resources=deployments/finalizers,verbs=*
// +kubebuilder:rbac:groups="core",resources=deployments,verbs=*
// +kubebuilder:rbac:groups="apps",resources=deployments,verbs=*
// +kubebuilder:rbac:groups="*",resources=deployments,verbs=*
// +kubebuilder:rbac:groups="extensions",resources=deployments,verbs=*

// +kubebuilder:rbac:groups="apiextensions.k8s.io",resources=customresourcedefinitions,verbs=get;list;watch;create;patch;delete

// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=validatingwebhookconfigurations,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=mutatingwebhookconfigurations,verbs=create;delete;list;update;watch;patch;get

// +kubebuilder:rbac:groups="*",resources=statefulsets,verbs=create;update;get;list;watch;patch;delete

// +kubebuilder:rbac:groups="*",resources=replicasets,verbs=*

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Dashboard object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.2/pkg/reconcile
func (r *DashboardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("dashboard", req.NamespacedName)

	dashboard := &componentsv1.Dashboard{}
	err := r.Client.Get(ctx, req.NamespacedName, dashboard)
	if err != nil {
		if k8serr.IsNotFound(err) {
			log.Info("Dashboard resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Dashboard")
		return ctrl.Result{}, err
	}

	// Get DSC and DSCI instances

	// Handle deletion
	if !dashboard.GetDeletionTimestamp().IsZero() {
		return r.reconcileDelete(ctx, dashboard)
	}

	// Execute actions
	for _, action := range r.Actions {
		if err := action.Execute(ctx, r.BaseReconciler, dashboard); err != nil {
			log.Error(err, "Failed to execute action", "action", fmt.Sprintf("%T", action))
			return ctrl.Result{}, err
		}
	}

	log.Info("Dashboard reconciliation completed successfully")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DashboardReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&componentsv1.Dashboard{}).
		Watches(&appsv1.Deployment{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
			return r.watchDashboardResources(ctx, a)
		}), builder.WithPredicates(dashboardPredicates)).
		Watches(&appsv1.ReplicaSet{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
			return r.watchDashboardResources(ctx, a)
		}), builder.WithPredicates(dashboardPredicates)).
		Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
			return r.watchDashboardResources(ctx, a)
		}), builder.WithPredicates(dashboardPredicates)).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
			return r.watchDashboardResources(ctx, a)
		}), builder.WithPredicates(dashboardPredicates)).
		Watches(&corev1.PersistentVolumeClaim{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
			return r.watchDashboardResources(ctx, a)
		}), builder.WithPredicates(dashboardPredicates)).
		Watches(&rbacv1.ClusterRoleBinding{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
			return r.watchDashboardResources(ctx, a)
		}), builder.WithPredicates(dashboardPredicates)).
		Watches(&rbacv1.ClusterRole{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
			return r.watchDashboardResources(ctx, a)
		}), builder.WithPredicates(dashboardPredicates)).
		Watches(&rbacv1.Role{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
			return r.watchDashboardResources(ctx, a)
		}), builder.WithPredicates(dashboardPredicates)).
		Watches(&rbacv1.RoleBinding{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
			return r.watchDashboardResources(ctx, a)
		}), builder.WithPredicates(dashboardPredicates)).
		Watches(&corev1.ServiceAccount{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
			return r.watchDashboardResources(ctx, a)
		}), builder.WithPredicates(dashboardPredicates)).
		Complete(r)
}

func (r *DashboardReconciler) reconcileDelete(ctx context.Context, dashboard *componentsv1.Dashboard) (ctrl.Result, error) {
	log := r.Log.WithValues("dashboard", dashboard.Name)
	log.Info("Reconciling Dashboard deletion")
	// common: Deploy odh-dashboard manifests
	// TODO: check if we can have the same component name odh-dashboard for both, or still keep rhods-dashboard for RHOAI
	switch r.Platform {
	case cluster.SelfManagedRhods, cluster.ManagedRhods:

		// Cleanup RHOAI manifests
		if err := deploy.DeployManifestsFromPath(ctx, r.Client, r.DSCinstance, r.entryPath, r.DSCIinstance.Spec.ApplicationsNamespace, ComponentNameDownstream, false); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to apply manifests from %s: %w", PathDownstream, err)
		}

	default:
		// Cleanup ODH manifests
		if err := deploy.DeployManifestsFromPath(ctx, r.Client, r.DSCinstance, r.entryPath, r.DSCIinstance.Spec.ApplicationsNamespace, ComponentNameUpstream, false); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *DashboardReconciler) watchDashboardResources(ctx context.Context, a client.Object) []reconcile.Request {
	if a.GetLabels()["app.opendatahub.io/dashboard"] == "true" {
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{Name: "default-dashboard"},
		}}
	}
	return nil
}

var dashboardPredicates = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		// Reconcile not needed during creation
		return false
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		labelList := e.Object.GetLabels()
		if value, exist := labelList[labels.ODH.Component(ComponentNameUpstream)]; exist && value == "true" {
			return true
		}
		return false
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		labelList := e.ObjectOld.GetLabels()
		if value, exist := labelList[labels.ODH.Component(ComponentNameUpstream)]; exist && value == "true" {
			return true
		}
		return false
	},
}

func updateKustomizeVariable(ctx context.Context, cli client.Client, platform cluster.Platform, dscispec *dsciv1.DSCInitializationSpec) (map[string]string, error) {
	adminGroups := map[cluster.Platform]string{
		cluster.SelfManagedRhods: "rhods-admins",
		cluster.ManagedRhods:     "dedicated-admins",
		cluster.OpenDataHub:      "odh-admins",
		cluster.Unknown:          "odh-admins",
	}[platform]

	sectionTitle := map[cluster.Platform]string{
		cluster.SelfManagedRhods: "OpenShift Self Managed Services",
		cluster.ManagedRhods:     "OpenShift Managed Services",
		cluster.OpenDataHub:      "OpenShift Open Data Hub",
		cluster.Unknown:          "OpenShift Open Data Hub",
	}[platform]

	consoleLinkDomain, err := cluster.GetDomain(ctx, cli)
	if err != nil {
		return nil, fmt.Errorf("error getting console route URL %s : %w", consoleLinkDomain, err)
	}
	consoleURL := map[cluster.Platform]string{
		cluster.SelfManagedRhods: "https://rhods-dashboard-" + dscispec.ApplicationsNamespace + "." + consoleLinkDomain,
		cluster.ManagedRhods:     "https://rhods-dashboard-" + dscispec.ApplicationsNamespace + "." + consoleLinkDomain,
		cluster.OpenDataHub:      "https://odh-dashboard-" + dscispec.ApplicationsNamespace + "." + consoleLinkDomain,
		cluster.Unknown:          "https://odh-dashboard-" + dscispec.ApplicationsNamespace + "." + consoleLinkDomain,
	}[platform]

	return map[string]string{
		"admin_groups":  adminGroups,
		"dashboard-url": consoleURL,
		"section-title": sectionTitle,
	}, nil
}

// Action implementations

type InitializeAction struct{}

func (a *InitializeAction) Execute(ctx context.Context, r *BaseReconciler, obj client.Object) error {
	// Implement initialization logic
	log := logf.FromContext(ctx).WithName(ComponentNameUpstream)

	imageParamMap := map[string]string{
		"odh-dashboard-image": "RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
	}
	DefaultPath = map[cluster.Platform]string{
		cluster.SelfManagedRhods: PathDownstream + "/onprem",
		cluster.ManagedRhods:     PathDownstream + "/addon",
		cluster.OpenDataHub:      PathUpstream,
		cluster.Unknown:          PathUpstream,
	}[r.Platform]

	if err := deploy.ApplyParams(DefaultPath, imageParamMap); err != nil {
		log.Error(err, "failed to update image", "path", DefaultPath)
	}

	return nil
}

type SupportDevFlagsAction struct{}

func (a *SupportDevFlagsAction) Execute(ctx context.Context, r *BaseReconciler, obj client.Object) error {
	dashboard := obj.(*componentsv1.Dashboard)
	dashboard.Spec.DevFlags = DSCInstance.Spec.Components.Dashboard.DevFlags
	// Implement devflags support logic
	// If devflags are set, update default manifests path
	if len(dashboard.Spec.DevFlags.Manifests) != 0 {
		manifestConfig := dashboard.Spec.DevFlags.Manifests[0]
		if err := deploy.DownloadManifests(ctx, ComponentNameUpstream, manifestConfig); err != nil {
			return err
		}
		if manifestConfig.SourcePath != "" {
			r.entryPath = filepath.Join(deploy.DefaultManifestPath, ComponentNameUpstream, manifestConfig.SourcePath)
		}
	}
	return nil
}

type CleanupOAuthClientAction struct{}

func (a *CleanupOAuthClientAction) Execute(ctx context.Context, r *BaseReconciler, obj client.Object) error {
	// Remove previous oauth-client secrets
	// Check if component is going from state of `Not Installed --> Installed`
	// Assumption: Component is currently set to enabled
	name := "dashboard-oauth-client"

	r.Log.Info("Cleanup any left secret")
	// Delete client secrets from previous installation
	oauthClientSecret := &corev1.Secret{}
	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: r.DSCIinstance.Spec.ApplicationsNamespace,
		Name:      name,
	}, oauthClientSecret)
	if err != nil {
		if !k8serr.IsNotFound(err) {
			return fmt.Errorf("error getting secret %s: %w", name, err)
		}
	} else {
		if err := r.Client.Delete(ctx, oauthClientSecret); err != nil {
			return fmt.Errorf("error deleting secret %s: %w", name, err)
		}
		r.Log.Info("successfully deleted secret", "secret", name)
	}

	return nil
}

type DeployComponentAction struct{}

func (a *DeployComponentAction) Execute(ctx context.Context, r *BaseReconciler, obj client.Object) error {
	// Implement component deployment logic
	// 1. platform specific RBAC
	if r.Platform == cluster.OpenDataHub || r.Platform == "" {
		if err := cluster.UpdatePodSecurityRolebinding(ctx, r.Client, r.DSCIinstance.Spec.ApplicationsNamespace, "odh-dashboard"); err != nil {
			return err
		}
	} else {
		if err := cluster.UpdatePodSecurityRolebinding(ctx, r.Client, r.DSCIinstance.Spec.ApplicationsNamespace, "rhods-dashboard"); err != nil {
			return err
		}
	}

	// 2. Append or Update variable for component to consume
	extraParamsMap, err := updateKustomizeVariable(ctx, r.Client, r.Platform, &r.DSCIinstance.Spec)
	if err != nil {
		return errors.New("failed to set variable for extraParamsMap")
	}

	// 3. update params.env regardless devFlags is provided of not
	if err := deploy.ApplyParams(r.entryPath, nil, extraParamsMap); err != nil {
		return fmt.Errorf("failed to update params.env  from %s : %w", r.entryPath, err)
	}

	// common: Deploy odh-dashboard manifests
	// TODO: check if we can have the same component name odh-dashboard for both, or still keep rhods-dashboard for RHOAI
	switch r.Platform {
	case cluster.SelfManagedRhods, cluster.ManagedRhods:
		// anaconda
		if err := cluster.CreateSecret(ctx, r.Client, "anaconda-ce-access", r.DSCIinstance.Spec.ApplicationsNamespace); err != nil {
			return fmt.Errorf("failed to create access-secret for anaconda: %w", err)
		}
		// Deploy RHOAI manifests
		if err := deploy.DeployManifestsFromPath(ctx, r.Client, r.DSCinstance, r.entryPath, r.DSCIinstance.Spec.ApplicationsNamespace, ComponentNameDownstream, true); err != nil {
			return fmt.Errorf("failed to apply manifests from %s: %w", PathDownstream, err)
		}
		r.Log.Info("apply manifests done")

		if err := cluster.WaitForDeploymentAvailable(ctx, r.Client, ComponentNameDownstream, r.DSCIinstance.Spec.ApplicationsNamespace, 20, 3); err != nil {
			return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentNameDownstream, err)
		}

		return nil

	default:
		// Deploy ODH manifests
		if err := deploy.DeployManifestsFromPath(ctx, r.Client, r.DSCinstance, r.entryPath, r.DSCIinstance.Spec.ApplicationsNamespace, ComponentNameUpstream, true); err != nil {
			return err
		}
		r.Log.Info("apply manifests done")

		if err := cluster.WaitForDeploymentAvailable(ctx, r.Client, ComponentNameUpstream, r.DSCIinstance.Spec.ApplicationsNamespace, 20, 3); err != nil {
			return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentNameUpstream, err)
		}
	}
	return nil
}

type UpdateStatusAction struct{}

func (a *UpdateStatusAction) Execute(ctx context.Context, r *BaseReconciler, obj client.Object) error {
	return nil
}
