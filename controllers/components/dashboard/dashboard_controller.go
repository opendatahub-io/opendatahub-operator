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

package dashboard

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/dependent"
	odhrec "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	ctrlogger "github.com/opendatahub-io/opendatahub-operator/v2/pkg/logger"
	annotation "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

const (
	ComponentName = "dashboard"
)

var (
	ComponentNameUpstream = ComponentName
	PathUpstream          = deploy.DefaultManifestPath + "/" + ComponentNameUpstream + "/odh"

	ComponentNameDownstream = "rhods-dashboard"
	PathDownstream          = deploy.DefaultManifestPath + "/" + ComponentNameUpstream + "/rhoai"
	PathSelfDownstream      = PathDownstream + "/onprem"
	PathManagedDownstream   = PathDownstream + "/addon"
	OverridePath            = ""
	DefaultPath             = ""

	dashboardID = types.NamespacedName{Name: componentsv1.DashboardInstanceName}
)

// NewDashboardReconciler

func NewDashboardReconciler(ctx context.Context, mgr ctrl.Manager) error {
	r, err := odhrec.NewComponentReconciler[*componentsv1.Dashboard](ctx, mgr, ComponentName)
	if err != nil {
		return err
	}

	actionCtx := logf.IntoContext(ctx, r.Log)
	// Add Dashboard-specific actions
	r.AddAction(&InitializeAction{actions.BaseAction{Log: mgr.GetLogger().WithName("actions").WithName("initialize")}})
	r.AddAction(&SupportDevFlagsAction{actions.BaseAction{Log: mgr.GetLogger().WithName("actions").WithName("devFlags")}})
	r.AddAction(&CleanupOAuthClientAction{actions.BaseAction{Log: mgr.GetLogger().WithName("actions").WithName("cleanup")}})
	r.AddAction(&DeployComponentAction{actions.BaseAction{Log: mgr.GetLogger().WithName("actions").WithName("deploy")}})

	r.AddAction(actions.NewUpdateStatusAction(
		actionCtx,
		actions.WithUpdateStatusLabel(labels.ComponentName, ComponentName),
	))

	var componentLabelPredicate predicate.Predicate
	var componentEventHandler handler.EventHandler

	switch r.Platform {
	case cluster.SelfManagedRhods, cluster.ManagedRhods:
		componentLabelPredicate = dashboardWatchPredicate(ComponentNameUpstream)
		componentEventHandler = watchDashboardResources(ComponentNameUpstream)
	default:
		componentLabelPredicate = dashboardWatchPredicate(ComponentNameDownstream)
		componentEventHandler = watchDashboardResources(ComponentNameDownstream)
	}

	err = ctrl.NewControllerManagedBy(mgr).
		// API
		For(&componentsv1.Dashboard{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.LabelChangedPredicate{}))).
		// operands
		Watches(&corev1.ConfigMap{}, componentEventHandler, builder.WithPredicates(componentLabelPredicate)).
		Watches(&corev1.Secret{}, componentEventHandler, builder.WithPredicates(componentLabelPredicate)).
		Watches(&rbacv1.ClusterRoleBinding{}, componentEventHandler, builder.WithPredicates(componentLabelPredicate)).
		Watches(&rbacv1.ClusterRole{}, componentEventHandler, builder.WithPredicates(componentLabelPredicate)).
		Watches(&rbacv1.Role{}, componentEventHandler, builder.WithPredicates(componentLabelPredicate)).
		Watches(&rbacv1.RoleBinding{}, componentEventHandler, builder.WithPredicates(componentLabelPredicate)).
		Watches(&corev1.ServiceAccount{}, componentEventHandler, builder.WithPredicates(componentLabelPredicate)).
		// Include status changes as we need to determine the component
		// readiness by observing the status of the deployments
		Watches(&appsv1.Deployment{}, componentEventHandler, builder.WithPredicates(componentLabelPredicate)).
		// Ignore status changes
		Watches(&routev1.Route{}, componentEventHandler, builder.WithPredicates(predicate.And(
			componentLabelPredicate,
			dependent.New()))).
		Complete(r)

	if err != nil {
		return fmt.Errorf("could not create the dashboard controller: %w", err)
	}

	return nil
}

func Init(platform cluster.Platform) error {
	imageParamMap := map[string]string{
		"odh-dashboard-image": "RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
	}

	DefaultPath = map[cluster.Platform]string{
		cluster.SelfManagedRhods: PathDownstream + "/onprem",
		cluster.ManagedRhods:     PathDownstream + "/addon",
		cluster.OpenDataHub:      PathUpstream,
		cluster.Unknown:          PathUpstream,
	}[platform]

	if err := deploy.ApplyParams(DefaultPath, imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", DefaultPath, err)
	}

	return nil
}

func GetDashboard(dsc *dscv1.DataScienceCluster) *componentsv1.Dashboard {
	dashboardAnnotations := make(map[string]string)

	switch dsc.Spec.Components.Dashboard.ManagementState {
	case operatorv1.Managed, operatorv1.Removed:
		dashboardAnnotations[annotation.ManagementStateAnnotation] = string(dsc.Spec.Components.Dashboard.ManagementState)
	default: // Force and Unmanaged case for unknown values, we do not support these yet
		dashboardAnnotations[annotation.ManagementStateAnnotation] = "Unknown"
	}

	return &componentsv1.Dashboard{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Dashboard",
			APIVersion: "components.opendatahub.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentsv1.DashboardInstanceName,
			Annotations: dashboardAnnotations,
		},
		Spec: componentsv1.DashboardSpec{
			DashboardCommonSpec: dsc.Spec.Components.Dashboard.DashboardCommonSpec,
		},
	}
}

//nolint:ireturn
func watchDashboardResources(componentName string) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(_ context.Context, a client.Object) []reconcile.Request {
		switch {
		case a.GetLabels()[labels.ODH.Component(componentName)] == "true":
			return []reconcile.Request{{NamespacedName: dashboardID}}
		case a.GetLabels()[labels.ComponentName] == ComponentName:
			return []reconcile.Request{{NamespacedName: dashboardID}}
		}

		return nil
	})
}

func dashboardWatchPredicate(componentName string) predicate.Funcs {
	label := labels.ODH.Component(componentName)
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			labelList := e.Object.GetLabels()

			if value, exist := labelList[labels.ComponentName]; exist && value == ComponentName {
				return true
			}
			if value, exist := labelList[label]; exist && value == "true" {
				return true
			}

			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			for _, labelList := range []map[string]string{e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()} {
				if value, exist := labelList[labels.ComponentName]; exist && value == ComponentName {
					return true
				}
				if value, exist := labelList[label]; exist && value == "true" {
					return true
				}
			}
			return false
		},
	}
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

type InitializeAction struct {
	actions.BaseAction
}

func (a *InitializeAction) Execute(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// 1. Update manifests
	rr.Manifests = map[cluster.Platform]string{
		cluster.SelfManagedRhods: PathDownstream + "/onprem",
		cluster.ManagedRhods:     PathDownstream + "/addon",
		cluster.OpenDataHub:      PathUpstream,
		cluster.Unknown:          PathUpstream,
	}
	// 2. Append or Update variable for component to consume
	extraParamsMap, err := updateKustomizeVariable(ctx, rr.Client, rr.Platform, &rr.DSCI.Spec)
	if err != nil {
		return errors.New("failed to set variable for extraParamsMap")
	}

	// 3. update params.env regardless devFlags is provided of not
	// We need this for downstream
	if err := deploy.ApplyParams(rr.Manifests[rr.Platform], nil, extraParamsMap); err != nil {
		return fmt.Errorf("failed to update params.env from %s : %w", rr.Manifests[rr.Platform], err)
	}

	return nil
}

type SupportDevFlagsAction struct {
	actions.BaseAction
}

func (a *SupportDevFlagsAction) Execute(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	dashboard, ok := rr.Instance.(*componentsv1.Dashboard)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentsv1.Dashboard)", rr.Instance)
	}

	if dashboard.Spec.DevFlags == nil {
		return nil
	}
	// Implement devflags support logic
	// If dev flags are set, update default manifests path
	if len(dashboard.Spec.DevFlags.Manifests) != 0 {
		manifestConfig := dashboard.Spec.DevFlags.Manifests[0]
		if err := deploy.DownloadManifests(ctx, ComponentNameUpstream, manifestConfig); err != nil {
			return err
		}
		if manifestConfig.SourcePath != "" {
			rr.Manifests[rr.Platform] = filepath.Join(deploy.DefaultManifestPath, ComponentNameUpstream, manifestConfig.SourcePath)
		}
	}

	if rr.DSCI.Spec.DevFlags != nil {
		mode := rr.DSCI.Spec.DevFlags.LogMode
		a.Log = ctrlogger.NewNamedLogger(logf.FromContext(ctx), ComponentName, mode)
	}

	return nil
}

type CleanupOAuthClientAction struct {
	actions.BaseAction
}

func (a *CleanupOAuthClientAction) Execute(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// Remove previous oauth-client secrets
	// Check if component is going from state of `Not Installed --> Installed`
	// Assumption: Component is currently set to enabled
	name := "dashboard-oauth-client"

	// r.Log.Info("Cleanup any left secret")
	// Delete client secrets from previous installation
	oauthClientSecret := &corev1.Secret{}
	err := rr.Client.Get(ctx, client.ObjectKey{
		Namespace: rr.DSCI.Spec.ApplicationsNamespace,
		Name:      name,
	}, oauthClientSecret)
	if err != nil {
		if !k8serr.IsNotFound(err) {
			return fmt.Errorf("error getting secret %s: %w", name, err)
		}
	} else {
		if err := rr.Client.Delete(ctx, oauthClientSecret); err != nil {
			return fmt.Errorf("error deleting secret %s: %w", name, err)
		}
		// r.Log.Info("successfully deleted secret", "secret", name)
	}

	return nil
}

type DeployComponentAction struct {
	actions.BaseAction
}

func (a *DeployComponentAction) Execute(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// Implement component deployment logic
	// 1. platform specific RBAC
	if rr.Platform == cluster.OpenDataHub || rr.Platform == "" {
		if err := cluster.UpdatePodSecurityRolebinding(ctx, rr.Client, rr.DSCI.Spec.ApplicationsNamespace, "odh-dashboard"); err != nil {
			return err
		}
	} else {
		if err := cluster.UpdatePodSecurityRolebinding(ctx, rr.Client, rr.DSCI.Spec.ApplicationsNamespace, "rhods-dashboard"); err != nil {
			return err
		}
	}

	path := rr.Manifests[rr.Platform]
	name := ComponentNameUpstream

	// common: Deploy odh-dashboard manifests
	// TODO: check if we can have the same component name odh-dashboard for both, or still keep rhods-dashboard for RHOAI
	switch rr.Platform {
	case cluster.SelfManagedRhods, cluster.ManagedRhods:
		// anaconda
		blckownerDel := true
		ctrlr := true
		err := cluster.CreateSecret(
			ctx,
			rr.Client,
			"anaconda-ce-access",
			rr.DSCI.Spec.ApplicationsNamespace,
			// set owner reference so it gets deleted when the Dashboard resource get deleted as well
			cluster.WithOwnerReference(metav1.OwnerReference{
				APIVersion:         rr.Instance.GetObjectKind().GroupVersionKind().GroupVersion().String(),
				Kind:               rr.Instance.GetObjectKind().GroupVersionKind().Kind,
				Name:               rr.Instance.GetName(),
				UID:                rr.Instance.GetUID(),
				Controller:         &ctrlr,
				BlockOwnerDeletion: &blckownerDel,
			}),
			cluster.WithLabels(
				labels.ComponentName, ComponentName,
				labels.ODH.Component(name), "true",
				labels.K8SCommon.PartOf, name,
			),
		)

		if err != nil {
			return fmt.Errorf("failed to create access-secret for anaconda: %w", err)
		}

		name = ComponentNameDownstream
	default:
	}

	err := deploy.DeployManifestsFromPathWithLabels(ctx, rr.Client, rr.Instance, path, rr.DSCI.Spec.ApplicationsNamespace, name, true, map[string]string{
		labels.ComponentName: ComponentName,
	})

	if err != nil {
		return fmt.Errorf("failed to apply manifests from %s: %w", name, err)
	}

	a.Log.Info("apply manifests done")

	return nil
}
