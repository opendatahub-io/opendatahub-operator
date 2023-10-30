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

// Package dscinitialization contains controller logic of CRD DSCInitialization.
package dscinitialization

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"

	"github.com/go-logr/logr"
	operatorv1 "github.com/openshift/api/operator/v1"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	authv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	finalizerName = "dscinitialization.opendatahub.io/finalizer"
)

// DSCInitializationReconciler reconciles a DSCInitialization object.
type DSCInitializationReconciler struct {
	client.Client
	Scheme                *runtime.Scheme
	Log                   logr.Logger
	Recorder              record.EventRecorder
	ApplicationsNamespace string
}

// +kubebuilder:rbac:groups="dscinitialization.opendatahub.io",resources=dscinitializations/status,verbs=get;update;patch;delete
// +kubebuilder:rbac:groups="dscinitialization.opendatahub.io",resources=dscinitializations/finalizers,verbs=get;update;patch;delete
// +kubebuilder:rbac:groups="dscinitialization.opendatahub.io",resources=dscinitializations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="dscinitialization.opendatahub.io",resources=featuretrackers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="kfdef.apps.kubeflow.org",resources=kfdefs,verbs=get;list;watch;create;update;patch;delete

// Reconcile contains controller logic specific to DSCInitialization instance updates.
func (r *DSCInitializationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("Reconciling DSCInitialization.", "DSCInitialization", req.Namespace, "Request.Name", req.Name)

	instances := &dsci.DSCInitializationList{}
	if err := r.Client.List(ctx, instances); err != nil {
		r.Log.Error(err, "Failed to retrieve DSCInitialization resource.", "DSCInitialization", req.Namespace, "Request.Name", req.Name)
		r.Recorder.Eventf(instances, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Failed to retrieve DSCInitialization instance")
		return ctrl.Result{}, err
	}

	if len(instances.Items) > 1 {
		message := fmt.Sprintf("only one instance of DSCInitialization object is allowed. Update existing instance on namespace %s and name %s", req.Namespace, req.Name)

		return ctrl.Result{}, errors.New(message)
	}

	if len(instances.Items) == 0 {
		return ctrl.Result{}, nil
	}

	instance := &instances.Items[0]

	if instance.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(instance, finalizerName) {
			r.Log.Info("Adding finalizer for DSCInitialization", "name", instance.Name, "namespace", instance.Namespace, "finalizer", finalizerName)
			controllerutil.AddFinalizer(instance, finalizerName)
			if err := r.Update(ctx, instance); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		r.Log.Info("Finalization DSCInitialization start deleting instance", "name", instance.Name, "namespace", instance.Namespace, "finalizer", finalizerName)
		if err := r.cleanupServiceMesh(instance); err != nil {
			return ctrl.Result{}, err
		}
		if controllerutil.ContainsFinalizer(instance, finalizerName) {
			controllerutil.RemoveFinalizer(instance, finalizerName)
			if err := r.Update(ctx, instance); err != nil {
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	var err error
	// Start reconciling
	if instance.Status.Conditions == nil {
		reason := status.ReconcileInit
		message := "Initializing DSCInitialization resource"
		instance, err = r.updateStatus(ctx, instance, func(saved *dsci.DSCInitialization) {
			status.SetProgressingCondition(&saved.Status.Conditions, reason, message)
			saved.Status.Phase = status.PhaseProgressing
		})
		if err != nil {
			r.Log.Error(err, "Failed to add conditions to status of DSCInitialization resource.", "DSCInitialization", req.Namespace, "Request.Name", req.Name)
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError",
				"%s for instance %s", message, instance.Name)
			return reconcile.Result{}, err
		}
	}

	// Check namespace
	namespace := instance.Spec.ApplicationsNamespace
	if err := r.createOdhNamespace(ctx, instance, namespace); err != nil {
		// no need to log error as it was already logged in createOdhNamespace
		return reconcile.Result{}, err
	}

	// Get platform
	platform, err := deploy.GetPlatform(r.Client)
	if err != nil {
		r.Log.Error(err, "Failed to determine platform (managed vs self-managed)")
		return reconcile.Result{}, err
	}

	if errRHODS := r.applyRHODSConfig(ctx, instance, platform); errRHODS != nil {
		return reconcile.Result{}, errRHODS
	}

	if monitoringErr := r.handleMonitoring(ctx, instance, platform); monitoringErr != nil {
		return reconcile.Result{}, monitoringErr
	}

	if errServiceMesh := r.handleServiceMesh(instance); errServiceMesh != nil {
		return reconcile.Result{}, errServiceMesh
	}

	// Finish reconciling
	_, err = r.updateStatus(ctx, instance, func(saved *dsci.DSCInitialization) {
		status.SetCompleteCondition(&saved.Status.Conditions, status.ReconcileCompleted, status.ReconcileCompletedMessage)
		saved.Status.Phase = status.PhaseReady
	})
	if err != nil {
		r.Log.Error(err, "failed to update DSCInitialization status after successfully completed reconciliation")
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Failed to update DSCInitialization status")
	}
	return ctrl.Result{}, nil
}

func (r *DSCInitializationReconciler) handleServiceMesh(instance *dsci.DSCInitialization) error {
	shouldConfigureServiceMesh, err := deploy.ShouldConfigureServiceMesh(r.Client, &instance.Spec)
	if err != nil {
		return err
	}

	if shouldConfigureServiceMesh {
		serviceMeshInitializer := servicemesh.NewServiceMeshInitializer(&instance.Spec, configureServiceMeshFeatures)

		if err := serviceMeshInitializer.Prepare(); err != nil {
			r.Log.Error(err, "failed configuring service mesh resources")
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "failed configuring service mesh resources")

			return err
		}

		if err := serviceMeshInitializer.Apply(); err != nil {
			r.Log.Error(err, "failed applying service mesh resources")
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "failed applying service mesh resources")

			return err
		}
	}

	return nil
}

func (r *DSCInitializationReconciler) cleanupServiceMesh(instance *dsci.DSCInitialization) error {
	shouldConfigureServiceMesh, err := deploy.ShouldConfigureServiceMesh(r.Client, &instance.Spec)
	if err != nil {
		return err
	}

	if shouldConfigureServiceMesh {
		serviceMeshInitializer := servicemesh.NewServiceMeshInitializer(&instance.Spec, configureServiceMeshFeatures)
		if err := serviceMeshInitializer.Prepare(); err != nil {
			return err
		}
		if err := serviceMeshInitializer.Delete(); err != nil {
			return err
		}
	}

	return nil
}

func configureServiceMeshFeatures(s *servicemesh.ServiceMeshInitializer) error {
	var rootDir = filepath.Join(feature.BaseOutputDir, s.DSCInitializationSpec.ApplicationsNamespace)
	if err := feature.CopyEmbeddedFiles("templates", rootDir); err != nil {
		return err
	}

	serviceMeshSpec := s.ServiceMesh

	if oauth, err := feature.CreateFeature("control-plane-configure-oauth").
		For(s.DSCInitializationSpec).
		Manifests(
			path.Join(rootDir, feature.ControlPlaneDir, "base"),
			path.Join(rootDir, feature.ControlPlaneDir, "oauth"),
			path.Join(rootDir, feature.ControlPlaneDir, "filters"),
		).
		WithResources(
			feature.SelfSignedCertificate,
			servicemesh.EnvoyOAuthSecrets,
		).
		WithData(servicemesh.ClusterDetails, servicemesh.OAuthConfig).
		PreConditions(
			servicemesh.EnsureServiceMeshInstalled,
		).
		PostConditions(
			feature.WaitForPodsToBeReady(serviceMeshSpec.Mesh.Namespace),
		).
		OnDelete(
			servicemesh.RemoveOAuthClient,
			servicemesh.RemoveTokenVolumes,
		).Load(); err != nil {
		return err
	} else {
		s.Features = append(s.Features, oauth)
	}

	if cfMaps, err := feature.CreateFeature("shared-config-maps").
		For(s.DSCInitializationSpec).
		WithResources(servicemesh.ConfigMaps).
		Load(); err != nil {
		return err
	} else {
		s.Features = append(s.Features, cfMaps)
	}

	if serviceMesh, err := feature.CreateFeature("app-add-namespace-to-service-mesh").
		For(s.DSCInitializationSpec).
		Manifests(
			path.Join(rootDir, feature.ControlPlaneDir, "smm.tmpl"),
			path.Join(rootDir, feature.ControlPlaneDir, "namespace.patch.tmpl"),
		).
		WithData(servicemesh.ClusterDetails).
		Load(); err != nil {
		return err
	} else {
		s.Features = append(s.Features, serviceMesh)
	}

	if gatewayRoute, err := feature.CreateFeature("create-gateway-route").
		For(s.DSCInitializationSpec).
		Manifests(
			path.Join(rootDir, feature.ControlPlaneDir, "routing"),
		).
		WithData(servicemesh.ClusterDetails).
		PostConditions(
			feature.WaitForPodsToBeReady(serviceMeshSpec.Mesh.Namespace),
		).
		Load(); err != nil {
		return err
	} else {
		s.Features = append(s.Features, gatewayRoute)
	}

	if dataScienceProjects, err := feature.CreateFeature("app-migrate-data-science-projects").
		For(s.DSCInitializationSpec).
		WithResources(servicemesh.MigratedDataScienceProjects).
		Load(); err != nil {
		return err
	} else {
		s.Features = append(s.Features, dataScienceProjects)
	}

	if extAuthz, err := feature.CreateFeature("control-plane-setup-external-authorization").
		For(s.DSCInitializationSpec).
		Manifests(
			path.Join(rootDir, feature.AuthDir, "auth-smm.tmpl"),
			path.Join(rootDir, feature.AuthDir, "base"),
			path.Join(rootDir, feature.AuthDir, "rbac"),
			path.Join(rootDir, feature.AuthDir, "mesh-authz-ext-provider.patch.tmpl"),
		).
		WithData(servicemesh.ClusterDetails).
		PreConditions(
			feature.CreateNamespace(serviceMeshSpec.Auth.Namespace),
			feature.EnsureCRDIsInstalled("authconfigs.authorino.kuadrant.io"),
			servicemesh.EnsureServiceMeshInstalled,
		).
		PostConditions(
			feature.WaitForPodsToBeReady(serviceMeshSpec.Mesh.Namespace),
			feature.WaitForPodsToBeReady(serviceMeshSpec.Auth.Namespace),
		).
		OnDelete(servicemesh.RemoveExtensionProvider).
		Load(); err != nil {
		return err
	} else {
		s.Features = append(s.Features, extAuthz)
	}

	return nil
}

func (r *DSCInitializationReconciler) applyRHODSConfig(ctx context.Context, instance *dsci.DSCInitialization, platform deploy.Platform) error {
	// Apply Rhods specific configs
	if platform == deploy.ManagedRhods || platform == deploy.SelfManagedRhods {
		// Apply osd specific permissions
		if platform == deploy.ManagedRhods {
			osdConfigsPath := filepath.Join(deploy.DefaultManifestPath, "osd-configs")
			err := deploy.DeployManifestsFromPath(r.Client, instance, osdConfigsPath, r.ApplicationsNamespace, "osd", true)
			if err != nil {
				r.Log.Error(err, "Failed to apply osd specific configs from manifests", "Manifests path", osdConfigsPath)
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Failed to apply "+osdConfigsPath)
				return err
			}
		} else {
			// Apply self-managed rhods config
			// Create rhods-admins Group if it doesn't exist
			err := r.createUserGroup(ctx, instance, "rhods-admins")
			if err != nil {
				return err
			}
		}
		// Apply common rhods-specific config
	} else { // ODH case
		// Create odh-admins Group if it doesn't exist
		err := r.createUserGroup(ctx, instance, "odh-admins")
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *DSCInitializationReconciler) handleMonitoring(ctx context.Context, instance *dsci.DSCInitialization, platform deploy.Platform) error {
	if instance.Spec.Monitoring.ManagementState == operatorv1.Managed {
		switch platform {
		case deploy.SelfManagedRhods:
			r.Log.Info("Monitoring enabled, won't apply changes", "cluster", "Self-Managed RHODS Mode")
			err := r.configureCommonMonitoring(instance)
			if err != nil {
				return err
			}
		case deploy.ManagedRhods:
			r.Log.Info("Monitoring enabled", "cluster", "Managed Service Mode")
			err := r.configureManagedMonitoring(ctx, instance)
			if err != nil {
				// no need to log error as it was already logged in configureManagedMonitoring
				return err
			}
			err = r.configureCommonMonitoring(instance)
			if err != nil {
				return err
			}
		default:
			// TODO: ODH specific monitoring logic
			r.Log.Info("Monitoring enabled, won't apply changes", "cluster", "ODH Mode")
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DSCInitializationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dsci.DSCInitialization{}).
		Owns(&corev1.Namespace{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&netv1.NetworkPolicy{}).
		Owns(&authv1.Role{}).
		Owns(&authv1.RoleBinding{}).
		Owns(&authv1.ClusterRole{}).
		Owns(&authv1.ClusterRoleBinding{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.ReplicaSet{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Service{}).
		// this predicates prevents meaningless reconciliations from being triggered
		WithEventFilter(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{})).
		Complete(r)
}

func (r *DSCInitializationReconciler) updateStatus(ctx context.Context, original *dsci.DSCInitialization, update func(saved *dsci.DSCInitialization),
) (*dsci.DSCInitialization, error) {
	saved := &dsci.DSCInitialization{}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(original), saved); err != nil {
			return err
		}

		update(saved)

		// Return err itself here (not wrapped inside another error)
		// so that RetryOnConflict can identify it correctly.
		return r.Client.Status().Update(ctx, saved)
	})

	return saved, err
}
