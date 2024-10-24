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

package ray

import (
	"context"
	"fmt"
	"path/filepath"

	operatorv1 "github.com/openshift/api/operator/v1"
	securityv1 "github.com/openshift/api/security/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odhrec "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	ctrlogger "github.com/opendatahub-io/opendatahub-operator/v2/pkg/logger"
	annotation "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

const (
	ComponentName = componentsv1.RayComponentName
)

var (
	DefaultPath = deploy.DefaultManifestPath + "/" + ComponentName + "/openshift"
	RayID       = types.NamespacedName{Name: componentsv1.RayInstanceName}
)

func NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	r, err := odhrec.NewComponentReconciler[*componentsv1.Ray](ctx, mgr, ComponentName)
	if err != nil {
		return err
	}

	actionCtx := logf.IntoContext(ctx, r.Log)
	// Add Ray-specific actions
	r.AddAction(&InitializeAction{actions.BaseAction{Log: mgr.GetLogger().WithName("actions").WithName("initialize")}})
	r.AddAction(&SupportDevFlagsAction{actions.BaseAction{Log: mgr.GetLogger().WithName("actions").WithName("devFlags")}})
	r.AddAction(&DeployComponentAction{actions.BaseAction{Log: mgr.GetLogger().WithName("actions").WithName("deploy")}})

	r.AddAction(actions.NewUpdateStatusAction(
		actionCtx,
		actions.WithUpdateStatusLabel(labels.ComponentName, ComponentName),
	))

	var componentLabelPredicate = watchPredicate(ComponentName)
	var componentEventHandler = watchResources(ComponentName)
	err = ctrl.NewControllerManagedBy(mgr).
		// Ray Component
		For(&componentsv1.Ray{}, builder.WithPredicates(predicate.Or(
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
		Watches(&appsv1.Deployment{}, componentEventHandler, builder.WithPredicates(componentLabelPredicate)).
		Watches(&apiextensionsv1.CustomResourceDefinition{}, componentEventHandler, builder.WithPredicates(componentLabelPredicate)).
		Watches(&securityv1.SecurityContextConstraints{}, componentEventHandler, builder.WithPredicates(componentLabelPredicate)).
		Complete(r)

	if err != nil {
		return err // no need customize error, it is done in the caller main
	}

	return nil
}

// TODO maybe more this into a common package.
//
//nolint:ireturn
func watchResources(componentName string) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(_ context.Context, a client.Object) []reconcile.Request {
		switch {
		case a.GetLabels()[labels.ODH.Component(componentName)] == "true":
			return []reconcile.Request{{NamespacedName: RayID}}
		case a.GetLabels()[labels.ComponentName] == ComponentName:
			return []reconcile.Request{{NamespacedName: RayID}}
		}

		return nil
	})
}

// TODO maybe more this into a common package.
func watchPredicate(componentName string) predicate.Funcs {
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

// for DSC to get compoment Ray's CR.
func GetComponentCR(dsc *dscv1.DataScienceCluster) *componentsv1.Ray {
	rayAnnotations := make(map[string]string)

	switch dsc.Spec.Components.Ray.ManagementState {
	case operatorv1.Managed, operatorv1.Removed:
		rayAnnotations[annotation.ManagementStateAnnotation] = string(dsc.Spec.Components.Ray.ManagementState)
	default: // Force and Unmanaged case for unknown values, we do not support these yet
		rayAnnotations[annotation.ManagementStateAnnotation] = "Unknown"
	}

	return &componentsv1.Ray{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Ray",
			APIVersion: "components.opendatahub.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentsv1.RayInstanceName,
			Annotations: rayAnnotations,
		},
		Spec: componentsv1.RaySpec{
			DSCRay: dsc.Spec.Components.Ray,
		},
	}
}

// Init for set images.
func Init(platform cluster.Platform) error {
	imageParamMap := map[string]string{
		"odh-kuberay-operator-controller-image": "RELATED_IMAGE_ODH_KUBERAY_OPERATOR_CONTROLLER_IMAGE",
	}

	if err := deploy.ApplyParams(DefaultPath, imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", DefaultPath, err)
	}

	return nil
}

// Actions.
type InitializeAction struct {
	actions.BaseAction
}

func (a *InitializeAction) Execute(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// 1. Update manifests

	if err := deploy.ApplyParams(DefaultPath, nil, map[string]string{"namespace": rr.DSCI.Spec.ApplicationsNamespace}); err != nil {
		return fmt.Errorf("failed to update params.env from %s : %w", rr.Manifests[rr.Platform], err)
	}

	return nil
}

type SupportDevFlagsAction struct {
	actions.BaseAction
}

func (a *SupportDevFlagsAction) Execute(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	ray, ok := rr.Instance.(*componentsv1.Ray)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentsv1.Ray)", rr.Instance)
	}

	if ray.Spec.DevFlags == nil {
		return nil
	}
	// Implement devflags support logic
	// If dev flags are set, update default manifests path
	if len(ray.Spec.DevFlags.Manifests) != 0 {
		manifestConfig := ray.Spec.DevFlags.Manifests[0]
		if err := deploy.DownloadManifests(ctx, ComponentName, manifestConfig); err != nil {
			return err
		}
		if manifestConfig.SourcePath != "" {
			rr.Manifests[rr.Platform] = filepath.Join(deploy.DefaultManifestPath, ComponentName, manifestConfig.SourcePath)
		}
	}

	if rr.DSCI.Spec.DevFlags != nil {
		mode := rr.DSCI.Spec.DevFlags.LogMode
		a.Log = ctrlogger.NewNamedLogger(logf.FromContext(ctx), ComponentName, mode)
	}

	return nil
}

type DeployComponentAction struct {
	actions.BaseAction
}

func (a *DeployComponentAction) Execute(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	if err := deploy.DeployManifestsFromPathWithLabels(ctx, rr.Client, rr.Instance, DefaultPath, rr.DSCI.Spec.ApplicationsNamespace, ComponentName, true, map[string]string{
		labels.ComponentName: ComponentName,
	}); err != nil {
		return fmt.Errorf("failed to apply manifests from %s: %w", ComponentName, err)
	}

	a.Log.Info("apply manifests done")

	return nil
}
