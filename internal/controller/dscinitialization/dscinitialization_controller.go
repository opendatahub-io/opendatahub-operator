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
	"path/filepath"

	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	rp "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/logger"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
)

const (
	finalizerName = "dscinitialization.opendatahub.io/finalizer"
	fieldManager  = "dscinitialization.opendatahub.io"
)

// DSCInitializationReconciler reconciles a DSCInitialization object.
type DSCInitializationReconciler struct {
	Client   client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// Reconcile contains controller logic specific to DSCInitialization instance updates.
func (r *DSCInitializationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) { //nolint:funlen,gocyclo,maintidx
	log := logf.FromContext(ctx).WithName("DSCInitialization")
	log.Info("Reconciling DSCInitialization.", "DSCInitialization Request.Name", req.Name)

	currentOperatorRelease := cluster.GetRelease()
	// Set platform
	platform := currentOperatorRelease.Name

	instance, err := cluster.GetDSCI(ctx, r.Client)
	switch {
	case k8serr.IsNotFound(err):
		return ctrl.Result{}, nil
	case err != nil:
		log.Error(err, "Failed to retrieve DSCInitialization resource.", "DSCInitialization Request.Name", req.Name)

		ref := &corev1.ObjectReference{Name: req.Name, Namespace: req.Namespace}
		ref.SetGroupVersionKind(gvk.DSCInitialization)

		r.Recorder.Eventf(ref, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Failed to retrieve DSCInitialization instance")

		return ctrl.Result{}, err
	}

	if instance.Spec.DevFlags != nil {
		level := instance.Spec.DevFlags.LogLevel
		log.V(1).Info("Setting log level", "level", level)
		if err := logger.SetLevel(level); err != nil {
			log.Error(err, "Failed to set log level", "level", level)
		}
	}

	if instance.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(instance, finalizerName) {
			log.Info("Adding finalizer for DSCInitialization", "name", instance.Name, "finalizer", finalizerName)
			controllerutil.AddFinalizer(instance, finalizerName)
			if err := r.Client.Update(ctx, instance); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		log.Info("Finalization DSCInitialization start deleting instance", "name", instance.Name, "finalizer", finalizerName)

		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			newInstance := &dsciv1.DSCInitialization{}
			if err := r.Client.Get(ctx, client.ObjectKeyFromObject(instance), newInstance); err != nil {
				return err
			}
			if controllerutil.ContainsFinalizer(newInstance, finalizerName) {
				controllerutil.RemoveFinalizer(newInstance, finalizerName)
				if err := r.Client.Update(ctx, newInstance); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			log.Error(err, "Failed to remove finalizer when deleting DSCInitialization instance")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// Start reconciling
	if instance.Status.Conditions == nil {
		reason := status.ReconcileInit
		message := "Initializing DSCInitialization resource"
		instance, err := status.UpdateWithRetry(ctx, r.Client, instance, func(saved *dsciv1.DSCInitialization) {
			status.SetProgressingCondition(&saved.Status.Conditions, reason, message)
			saved.Status.Phase = status.PhaseProgressing
			saved.Status.Release = currentOperatorRelease
		})
		if err != nil {
			log.Error(err, "Failed to add conditions to status of DSCInitialization resource.", "DSCInitialization", req.Namespace, "Request.Name", req.Name)
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError",
				"%s for instance %s", message, instance.Name)

			return reconcile.Result{}, err
		}
	}

	// upgrade case to update release version in status
	if !instance.Status.Release.Version.Equals(currentOperatorRelease.Version.Version) {
		message := "Updating DSCInitialization status"
		instance, err := status.UpdateWithRetry(ctx, r.Client, instance, func(saved *dsciv1.DSCInitialization) {
			saved.Status.Release = currentOperatorRelease
		})
		if err != nil {
			log.Error(err, "Failed to update release version for DSCInitialization resource.", "DSCInitialization", req.Namespace, "Request.Name", req.Name)
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError",
				"%s for instance %s", message, instance.Name)
			return reconcile.Result{}, err
		}
	}

	// Deal with application namespace, configmap, networpolicy etc
	if err := r.createOperatorResource(ctx, instance, platform); err != nil {
		if _, err := status.UpdateWithRetry(ctx, r.Client, instance, func(saved *dsciv1.DSCInitialization) {
			status.SetProgressingCondition(&saved.Status.Conditions, status.ReconcileFailed, err.Error())
			saved.Status.Phase = status.PhaseError
		}); err != nil {
			log.Error(err, "Failed to update DSCInitialization conditions", "DSCInitialization", req.Namespace, "Request.Name", req.Name)

			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError",
				"%s for instance %s", err.Error(), instance.Name)
		}

		// no need to log error as it was already logged in createOperatorResource
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError",
			"failed to create operator resources for instance %s: %s", instance.Name, err.Error())

		return reconcile.Result{}, err
	}

	switch req.Name {
	case "prometheus": // prometheus configmap
		if instance.Spec.Monitoring.ManagementState == operatorv1.Managed && platform == cluster.ManagedRhoai {
			log.Info("Monitoring enabled to restart deployment", "cluster", "Managed Service Mode")
			if err := r.configureManagedMonitoring(ctx, instance, "updates"); err != nil {
				return reconcile.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	case "addon-managed-odh-parameters":
		if instance.Spec.Monitoring.ManagementState == operatorv1.Managed && platform == cluster.ManagedRhoai {
			log.Info("Monitoring enabled when notification updated", "cluster", "Managed Service Mode")
			if err := r.configureManagedMonitoring(ctx, instance, "updates"); err != nil {
				return reconcile.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	case "backup": // revert back to the original prometheus.yml
		if instance.Spec.Monitoring.ManagementState == operatorv1.Managed && platform == cluster.ManagedRhoai {
			log.Info("Monitoring enabled to restore back", "cluster", "Managed Service Mode")
			if err := r.configureManagedMonitoring(ctx, instance, "revertbackup"); err != nil {
				return reconcile.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	default:
		switch platform {
		case cluster.SelfManagedRhoai:
			if instance.Spec.Monitoring.ManagementState == operatorv1.Managed {
				log.Info("Monitoring enabled", "cluster", "Self-Managed Mode")
				if err = r.configureSegmentIO(ctx, instance); err != nil {
					return reconcile.Result{}, err
				}

				if err = r.newMonitoringCR(ctx, instance); err != nil {
					return ctrl.Result{}, err
				}
			} else {
				log.Info("Monitoring disabled", "cluster", "Self-Managed Mode")
				if err := r.deleteMonitoringCR(ctx); err != nil {
					return reconcile.Result{}, err
				}
			}
		case cluster.ManagedRhoai:
			osdConfigsPath := filepath.Join(deploy.DefaultManifestPath, "osd-configs")
			if err = deploy.DeployManifestsFromPath(ctx, r.Client, instance, osdConfigsPath, instance.Spec.ApplicationsNamespace, "osd", true); err != nil {
				log.Error(err, "Failed to apply osd specific configs from manifests", "Manifests path", osdConfigsPath)
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Failed to apply "+osdConfigsPath)

				return reconcile.Result{}, err
			}
			// TODO: till we allow user to disable Monitoring in Managed cluster
			log.Info("Monitoring enabled in initialization stage", "cluster", "Managed Service Mode")
			if err = r.newMonitoringCR(ctx, instance); err != nil {
				return ctrl.Result{}, err
			}
			if err = r.configureManagedMonitoring(ctx, instance, "init"); err != nil {
				return reconcile.Result{}, err
			}
			if err = r.configureCommonMonitoring(ctx, instance); err != nil {
				return reconcile.Result{}, err
			}
		default: // TODO: see if this can be conbimed with self-managed case
			if instance.Spec.Monitoring.ManagementState == operatorv1.Managed {
				log.Info("Monitoring enabled", "cluster", "ODH Mode")
				if err = r.newMonitoringCR(ctx, instance); err != nil {
					return ctrl.Result{}, err
				}
			} else {
				log.Info("Monitoring disabled", "cluster", "ODH Mode")
				if err := r.deleteMonitoringCR(ctx); err != nil {
					return reconcile.Result{}, err
				}
			}
		}

		// Create Auth
		if err = r.CreateAuth(ctx, platform); err != nil {
			log.Info("failed to create Auth")
			return ctrl.Result{}, err
		}

		// Finish reconciling
		_, err = status.UpdateWithRetry(ctx, r.Client, instance, func(saved *dsciv1.DSCInitialization) {
			status.SetCompleteCondition(&saved.Status.Conditions, status.ReconcileCompleted, status.ReconcileCompletedMessage)
			saved.Status.Phase = status.PhaseReady
		})
		if err != nil {
			log.Error(err, "failed to update DSCInitialization status after successfully completed reconciliation")
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Failed to update DSCInitialization status")
		}

		return ctrl.Result{}, nil
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *DSCInitializationReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// add predicates prevents meaningless reconciliations from being triggered
		// not use WithEventFilter() because it conflict with secret and configmap predicate
		For(
			&dsciv1.DSCInitialization{},
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{})),
		).
		Owns(
			&corev1.Namespace{},
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			&corev1.Secret{},
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			&corev1.ConfigMap{},
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			&networkingv1.NetworkPolicy{},
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			&rbacv1.Role{},
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			&rbacv1.RoleBinding{},
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			&rbacv1.ClusterRole{},
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			&rbacv1.ClusterRoleBinding{},
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			&appsv1.Deployment{},
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			&corev1.ServiceAccount{},
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			&corev1.Service{},
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			&routev1.Route{},
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(&corev1.PersistentVolumeClaim{},
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Watches(
			&dscv1.DataScienceCluster{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
				return r.watchDSCResource(ctx)
			}),
			builder.WithPredicates(rp.DSCDeletionPredicate), // TODO: is it needed?
		).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.watchMonitoringSecretResource),
			builder.WithPredicates(rp.SecretContentChangedPredicate),
		).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.watchMonitoringConfigMapResource),
			builder.WithPredicates(rp.CMContentChangedPredicate),
		).
		Watches(
			&serviceApi.Auth{},
			handler.EnqueueRequestsFromMapFunc(r.watchAuthResource),
		).
		Complete(r)
}

func (r *DSCInitializationReconciler) watchMonitoringConfigMapResource(ctx context.Context, a client.Object) []reconcile.Request {
	log := logf.FromContext(ctx)
	if a.GetName() == "prometheus" && a.GetNamespace() == "redhat-ods-monitoring" {
		log.Info("Found monitoring configmap has updated, start reconcile")

		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "prometheus", Namespace: "redhat-ods-monitoring"}}}
	}
	return nil
}

func (r *DSCInitializationReconciler) watchMonitoringSecretResource(ctx context.Context, a client.Object) []reconcile.Request {
	log := logf.FromContext(ctx)
	operatorNs, err := cluster.GetOperatorNamespace()
	if err != nil {
		return nil
	}

	if a.GetName() == "addon-managed-odh-parameters" && a.GetNamespace() == operatorNs {
		log.Info("Found monitoring secret has updated, start reconcile")

		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "addon-managed-odh-parameters", Namespace: operatorNs}}}
	}
	return nil
}

func (r *DSCInitializationReconciler) watchDSCResource(ctx context.Context) []reconcile.Request {
	log := logf.FromContext(ctx)
	instanceList := &dscv1.DataScienceClusterList{}
	if err := r.Client.List(ctx, instanceList); err != nil {
		// do not handle if cannot get list
		log.Error(err, "Failed to get DataScienceClusterList")
		return nil
	}
	if len(instanceList.Items) == 0 && !upgrade.HasDeleteConfigMap(ctx, r.Client) {
		log.Info("Found no DSC instance in cluster but not in uninstalltion process, reset monitoring stack config")

		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "backup"}}}
	}
	return nil
}

func (r *DSCInitializationReconciler) watchAuthResource(ctx context.Context, a client.Object) []reconcile.Request {
	log := logf.FromContext(ctx)
	instanceList := &serviceApi.AuthList{}
	if err := r.Client.List(ctx, instanceList); err != nil {
		// do not handle if cannot get list
		log.Error(err, "Failed to get AuthList")
		return nil
	}
	if len(instanceList.Items) == 0 {
		log.Info("Found no Auth instance in cluster, reconciling to recreate")

		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "auth"}}}
	}

	return nil
}

func (r *DSCInitializationReconciler) deleteMonitoringCR(ctx context.Context) error {
	defaultMonitoring := &serviceApi.Monitoring{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.MonitoringInstanceName,
		},
	}
	err := r.Client.Delete(ctx, defaultMonitoring)
	if err != nil && !k8serr.IsNotFound(err) {
		return err
	}

	return nil
}

func (r *DSCInitializationReconciler) newMonitoringCR(ctx context.Context, dsci *dsciv1.DSCInitialization) error {
	// Create Monitoring CR singleton
	defaultMonitoring := &serviceApi.Monitoring{
		TypeMeta: metav1.TypeMeta{
			Kind:       serviceApi.MonitoringKind,
			APIVersion: serviceApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.MonitoringInstanceName,
		},
		Spec: serviceApi.MonitoringSpec{
			MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
				Namespace: dsci.Spec.Monitoring.Namespace,
			},
		},
	}

	if dsci.Spec.Monitoring.Metrics != nil {
		// when metrics has values set in resoures or storage. skip replicas since it cannot be 0 from CEL validation
		if dsci.Spec.Monitoring.Metrics.Storage != nil || dsci.Spec.Monitoring.Metrics.Resources != nil {
			defaultMonitoring.Spec.Metrics = dsci.Spec.Monitoring.Metrics
		} else { // if metrics is set to metrics:{} to avoid  invalid value "null" to Apply() existing Monitoring CR
			defaultMonitoring.Spec.Metrics = nil // explictliy set to nil, same as not set but for better readability
		}
	}

	defaultMonitoring.Spec.Traces = dsci.Spec.Monitoring.Traces
	defaultMonitoring.Spec.Alerting = dsci.Spec.Monitoring.Alerting

	if err := controllerutil.SetOwnerReference(dsci, defaultMonitoring, r.Client.Scheme()); err != nil {
		return err
	}

	err := resources.Apply(
		ctx,
		r.Client,
		defaultMonitoring,
		client.FieldOwner(fieldManager),
		client.ForceOwnership,
	)

	if err != nil && !k8serr.IsAlreadyExists(err) {
		return err
	}
	return nil
}
