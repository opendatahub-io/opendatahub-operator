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
	"fmt"
	"path/filepath"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	featuresv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	rp "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/logger"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	finalizerName = "dscinitialization.opendatahub.io/finalizer"
	fieldManager  = "dscinitialization.opendatahub.io"
)

// DSCInitializationReconciler reconciles a DSCInitialization object.
type DSCInitializationReconciler struct {
	Client           client.Client
	Scheme           *runtime.Scheme
	Recorder         events.EventRecorder
	OperatorSettings operatorconfig.OperatorSettings
}

type DSCInitializationCondition struct {
	Type         string
	ReadyReason  string
	ReadyMessage string
	ReadyStatus  metav1.ConditionStatus
}

// Reconcile contains controller logic specific to DSCInitialization instance updates.
func (r *DSCInitializationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) { //nolint:funlen,maintidx,gocyclo
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

		r.Recorder.Eventf(ref, nil, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Reconcile",
			"Failed to retrieve DSCInitialization instance: %v", err)

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
			newInstance := &dsciv2.DSCInitialization{}
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
		instance, err := status.UpdateWithRetry(ctx, r.Client, instance, func(saved *dsciv2.DSCInitialization) {
			status.SetProgressingCondition(&saved.Status.Conditions, reason, message)
			saved.Status.Phase = status.PhaseProgressing
			saved.Status.Release = currentOperatorRelease
		})
		if err != nil {
			log.Error(err, "Failed to add conditions to status of DSCInitialization resource.", "DSCInitialization", req.Namespace, "Request.Name", req.Name)
			r.Recorder.Eventf(instance, nil, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Reconcile",
				"%s for instance %s: %v", message, instance.Name, err)

			return reconcile.Result{}, err
		}
	}

	// upgrade case to update release version in status
	if !instance.Status.Release.Version.Equals(currentOperatorRelease.Version.Version) {
		message := "Updating DSCInitialization status"
		instance, err := status.UpdateWithRetry(ctx, r.Client, instance, func(saved *dsciv2.DSCInitialization) {
			saved.Status.Release = currentOperatorRelease
		})
		if err != nil {
			log.Error(err, "Failed to update release version for DSCInitialization resource.", "DSCInitialization", req.Namespace, "Request.Name", req.Name)
			r.Recorder.Eventf(instance, nil, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Reconcile",
				"%s for instance %s: %v", message, instance.Name, err)
			return reconcile.Result{}, err
		}
	}

	// Deal with application namespace, configmap, networpolicy etc
	if err := r.createOperatorResource(ctx, instance, platform); err != nil {
		if _, err := status.UpdateWithRetry(ctx, r.Client, instance, func(saved *dsciv2.DSCInitialization) {
			status.SetProgressingCondition(&saved.Status.Conditions, status.ReconcileFailed, err.Error())
			saved.Status.Phase = status.PhaseError
		}); err != nil {
			log.Error(err, "Failed to update DSCInitialization conditions", "DSCInitialization", req.Namespace, "Request.Name", req.Name)

			r.Recorder.Eventf(instance, nil, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Reconcile",
				"%s for instance %s", err.Error(), instance.Name)
		}

		// no need to log error as it was already logged in createOperatorResource
		r.Recorder.Eventf(instance, nil, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Reconcile",
			"failed to create operator resources for instance %s: %s", instance.Name, err.Error())

		return reconcile.Result{}, err
	}

	if platform == cluster.ManagedRhoai {
		osdConfigsPath := filepath.Join(r.OperatorSettings.ManifestsBasePath, "osd-configs")
		if err = deploy.DeployManifestsFromPath(ctx, r.Client, instance, osdConfigsPath, instance.Spec.ApplicationsNamespace, "osd", true); err != nil {
			log.Error(err, "Failed to apply osd specific configs from manifests", "Manifests path", osdConfigsPath)
			r.Recorder.Eventf(instance, nil, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Reconcile",
				"Failed to apply %s: %v", osdConfigsPath, err)

			return reconcile.Result{}, err
		}
	}

	switch instance.Spec.Monitoring.ManagementState {
	case operatorv1.Managed:
		if err = r.newMonitoringCR(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
	case operatorv1.Removed:
		if err = r.deleteMonitoringCR(ctx); err != nil {
			return reconcile.Result{}, err
		}
	default:
		// Unknown or empty state: do nothing
	}

	// legacy ServiceMesh FeatureTracker cleanup, retained from the remove ServiceMesh controller
	// TODO where exactly to put this logic ?
	ftNames := []string{
		instance.Spec.ApplicationsNamespace + "-mesh-shared-configmap",
		instance.Spec.ApplicationsNamespace + "-mesh-control-plane-creation",
		instance.Spec.ApplicationsNamespace + "-mesh-metrics-collection",
		instance.Spec.ApplicationsNamespace + "-enable-proxy-injection-in-authorino-deployment",
		instance.Spec.ApplicationsNamespace + "-mesh-control-plane-external-authz",
	}
	for _, name := range ftNames {
		ft := featuresv1.FeatureTracker{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		}

		err := r.Client.Delete(ctx, &ft, client.PropagationPolicy(metav1.DeletePropagationForeground))
		if k8serr.IsNotFound(err) {
			continue
		} else if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete FeatureTracker %s: %w", ft.GetName(), err)
		}
	}

	// Create Auth
	if err = r.CreateAuth(ctx, platform); err != nil {
		log.Info("failed to create Auth")
		return ctrl.Result{}, err
	}

	// Create GatewayConfig, always have one in the cluster but up to user to config.
	if err = r.CreateGatewayConfig(ctx, instance); err != nil {
		log.Info("failed to create GatewayConfig")
		return ctrl.Result{}, err
	}

	// Create default HWProfile CR
	if err = r.ManageDefaultAndCustomHWProfileCR(ctx, instance, platform); err != nil {
		log.Info("failed to manage default and custom HardwareProfile CR")
		return ctrl.Result{}, err
	}

	// Finish reconciling
	monitoringConditions := r.GetMonitoringReadyCondition(ctx)
	_, err = status.UpdateWithRetry(ctx, r.Client, instance, func(saved *dsciv2.DSCInitialization) {
		status.SetCompleteCondition(&saved.Status.Conditions, status.ReconcileCompleted, status.ReconcileCompletedMessage)
		for _, c := range monitoringConditions {
			status.SetCondition(&saved.Status.Conditions, c.Type, c.ReadyReason, c.ReadyMessage, c.ReadyStatus)
		}
		saved.Status.Phase = status.PhaseReady
	})
	if err != nil {
		log.Error(err, "failed to update DSCInitialization status after successfully completed reconciliation")
		r.Recorder.Eventf(instance, nil, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Reconcile",
			"Failed to update DSCInitialization status: %v", err)
	}

	return ctrl.Result{}, nil
}

func getObject(gvk schema.GroupVersionKind) client.Object {
	return resources.GvkToUnstructured(gvk)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DSCInitializationReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// add predicates prevents meaningless reconciliations from being triggered
		// not use WithEventFilter() because it conflict with secret and configmap predicate
		For(
			getObject(gvk.DSCInitialization),
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{})),
		).
		Owns(
			getObject(gvk.Namespace),
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			getObject(gvk.Secret),
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			getObject(gvk.ConfigMap),
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			getObject(gvk.NetworkPolicy),
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			getObject(gvk.Role),
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			getObject(gvk.RoleBinding),
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			getObject(gvk.ClusterRole),
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			getObject(gvk.ClusterRoleBinding),
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			getObject(gvk.Deployment),
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			getObject(gvk.ServiceAccount),
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			getObject(gvk.Service),
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			getObject(gvk.Route),
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns(
			getObject(gvk.PersistentVolumeClaim),
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))).
		Owns( // ensure always have default one for AcceleratorProfile/HardwareProfile blocking
			getObject(gvk.ValidatingAdmissionPolicy),
		).
		Owns( // ensure always have default one for AcceleratorProfile/HardwareProfile blocking
			getObject(gvk.ValidatingAdmissionPolicyBinding),
		).
		Owns( // ensure always have one platform's HardwareProfile in the cluster.
			getObject(gvk.HardwareProfile),
			builder.WithPredicates(rp.Deleted())).
		Watches(
			getObject(gvk.Auth),
			handler.EnqueueRequestsFromMapFunc(r.watchAuthResource),
		).
		Watches(
			getObject(gvk.GatewayConfig),
			handler.EnqueueRequestsFromMapFunc(r.watchGatewayConfigResource),
		).
		Watches(
			getObject(gvk.Monitoring),
			handler.EnqueueRequestsFromMapFunc(r.watchMonitoringResource),
		).
		Watches( // TODO: this might not be needed after v3.3.
			getObject(gvk.CustomResourceDefinition),
			handler.EnqueueRequestsFromMapFunc(r.watchHWProfileCRDResource),
			builder.WithPredicates(predicate.Or(
				rp.CreatedOrUpdatedName("acceleratorprofiles.dashboard.opendatahub.io"),
				rp.CreatedOrUpdatedName("hardwareprofiles.dashboard.opendatahub.io"),
			)),
		).
		Complete(r)
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

func (r *DSCInitializationReconciler) watchGatewayConfigResource(ctx context.Context, a client.Object) []reconcile.Request {
	log := logf.FromContext(ctx)
	instanceList := &serviceApi.GatewayConfigList{}
	if err := r.Client.List(ctx, instanceList); err != nil {
		// do not handle if cannot get list
		log.Error(err, "Failed to get GatewayConfigList")
		return nil
	}
	if len(instanceList.Items) == 0 {
		log.Info("Found no GatewayConfig instance in cluster, reconciling to recreate one")

		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: serviceApi.GatewayConfigName}}}
	}

	return nil
}

func (r *DSCInitializationReconciler) watchMonitoringResource(ctx context.Context, _ client.Object) []reconcile.Request {
	log := logf.FromContext(ctx)
	instanceList := &serviceApi.MonitoringList{}
	if err := r.Client.List(ctx, instanceList); err != nil {
		log.Error(err, "failed to get MonitoringList")
		return nil
	}
	if len(instanceList.Items) == 0 {
		log.Info("Found no Monitoring instance in cluster, reconciling to recreate one")
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: serviceApi.MonitoringInstanceName}}}
	}

	// Monitoring CR exists — trigger DSCI reconciliation so it can propagate
	// the latest Monitoring status conditions into its own status.
	dsciList := &dsciv2.DSCInitializationList{}
	if err := r.Client.List(ctx, dsciList); err != nil {
		log.Error(err, "Failed to get DSCInitializationList")
		return nil
	}
	if len(dsciList.Items) == 0 {
		log.Info("Found no DSCInitialization instance in cluster")
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "default-dsci"}}}
	}

	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: dsciList.Items[0].Name}}}
}

func (r *DSCInitializationReconciler) GetMonitoringReadyCondition(ctx context.Context) []DSCInitializationCondition {
	monitoring := &serviceApi.Monitoring{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: serviceApi.MonitoringInstanceName}, monitoring)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return []DSCInitializationCondition{{status.ConditionMonitoringReady, status.RemovedReason, "Monitoring is not enabled", metav1.ConditionFalse}}
		}
		return []DSCInitializationCondition{{status.ConditionMonitoringReady, status.NotReadyReason,
			fmt.Sprintf("Failed to retrieve Monitoring CR status: %v", err), metav1.ConditionUnknown}}
	}

	monitoringConditions := monitoring.GetConditions()
	conditions := make([]DSCInitializationCondition, 0, len(monitoringConditions)+1)

	// we filter the conditions from the Monitoring CR to the list to be returned later
	// we only care about the ones that are related to dependant operators (e.g. Thanos, OpenTelemetry, etc.)
	for _, c := range monitoringConditions {
		switch c.Type {
		case status.ConditionTypeReady,
			status.ConditionTypeProvisioningSucceeded,
			status.ConditionMonitoringStackAvailable,
			status.ConditionThanosQuerierAvailable,
			status.ConditionOpenTelemetryCollectorAvailable,
			status.ConditionTempoAvailable,
			status.ConditionPersesAvailable,
			status.ConditionAlertingAvailable,
			status.ConditionNodeMetricsEndpointAvailable:
			conditions = append(conditions, DSCInitializationCondition{
				Type:         c.Type,
				ReadyReason:  c.Reason,
				ReadyMessage: c.Message,
				ReadyStatus:  c.Status,
			})
		}
	}

	if len(conditions) == 0 {
		return []DSCInitializationCondition{{status.ConditionMonitoringReady, status.NotReadyReason, "Monitoring stack is initializing", metav1.ConditionUnknown}}
	}

	// If Monitoring stack is initialized, we add the MonitoringReady condition as True
	conditions = append(conditions, DSCInitializationCondition{
		Type:         status.ConditionMonitoringReady,
		ReadyReason:  status.ReadyReason,
		ReadyMessage: "Monitoring stack is initialized",
		ReadyStatus:  metav1.ConditionTrue,
	})

	return conditions
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

func (r *DSCInitializationReconciler) newMonitoringCR(ctx context.Context, dsci *dsciv2.DSCInitialization) error {
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

	metricsEnabled := dsci.Spec.Monitoring.Metrics != nil && dsci.Spec.Monitoring.Metrics.Storage != nil
	tracesEnabled := dsci.Spec.Monitoring.Traces != nil

	if metricsEnabled {
		defaultMonitoring.Spec.Metrics = dsci.Spec.Monitoring.Metrics
	} else {
		defaultMonitoring.Spec.Metrics = nil
	}

	if tracesEnabled {
		defaultMonitoring.Spec.Traces = dsci.Spec.Monitoring.Traces
		// Without this, when TLS.Enabled is false, the TLS struct is not removed from the Monitoring CR and it causes an error.
		if defaultMonitoring.Spec.Traces.TLS != nil && !defaultMonitoring.Spec.Traces.TLS.Enabled {
			defaultMonitoring.Spec.Traces.TLS = nil
		}
	} else {
		defaultMonitoring.Spec.Traces = nil
	}

	defaultMonitoring.Spec.Alerting = dsci.Spec.Monitoring.Alerting

	if metricsEnabled || tracesEnabled {
		if dsci.Spec.Monitoring.CollectorReplicas != 0 {
			defaultMonitoring.Spec.CollectorReplicas = dsci.Spec.Monitoring.CollectorReplicas
		} else {
			isSNO := cluster.IsSingleNodeCluster(ctx, r.Client)
			if isSNO {
				defaultMonitoring.Spec.CollectorReplicas = 1
			} else {
				defaultMonitoring.Spec.CollectorReplicas = 2
			}
		}
	}

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

// CreateGatewayConfig creates a default GatewayConfig if it doesn't exist.
// Parameters:
//   - ctx: context for the operation
//   - instance: DSCInitialization instance
//
// Returns:
//   - error: nil on success, error if GatewayConfig creation fails
func (r *DSCInitializationReconciler) CreateGatewayConfig(ctx context.Context, instance *dsciv2.DSCInitialization) error {
	gatewayConfig := &serviceApi.GatewayConfig{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: serviceApi.GatewayConfigName}, gatewayConfig)
	if err == nil {
		return nil
	}

	if !k8serr.IsNotFound(err) {
		return err
	}

	// GatewayConfig CR isn't found, create default GatewayConfig CR.
	defaultGateway := &serviceApi.GatewayConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       serviceApi.GatewayConfigKind,
			APIVersion: serviceApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName,
		},
		Spec: serviceApi.GatewayConfigSpec{
			Certificate: &infrav1.CertificateSpec{
				Type:       infrav1.OpenshiftDefaultIngress,
				SecretName: gateway.DefaultGatewayTLSSecretName,
			},
			Cookie: serviceApi.CookieConfig{
				Expire:  metav1.Duration{Duration: 24 * time.Hour},
				Refresh: metav1.Duration{Duration: 1 * time.Hour},
			},
			AuthProxyTimeout: metav1.Duration{Duration: 5 * time.Second},
		},
	}

	// Set the DSCInitialization instance as the owner of the GatewayConfig
	if err := ctrl.SetControllerReference(instance, defaultGateway, r.Scheme); err != nil {
		return err
	}

	if err := r.Client.Create(ctx, defaultGateway); err != nil && !k8serr.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// watchHWProfileCRDResource triggers DSCI reconciliation when Dashboard AcceleratorProfile/HWProfile CRDs are created.
// This ensures VAP/VAPB resources can be created when Dashboard CRDs become available.
// TODO: this is a temporary solution to ensure VAP/VAPB resources are created when Dashboard CRDs become available, it should be removed in v3.3.
func (r *DSCInitializationReconciler) watchHWProfileCRDResource(ctx context.Context, a client.Object) []reconcile.Request {
	log := logf.FromContext(ctx)

	log.V(1).Info("Dashboard CRD change detected, triggering DSCI reconciliation for VAP/VAPB resources", "CRD", a.GetName())

	instanceList := &dsciv2.DSCInitializationList{}
	if err := r.Client.List(ctx, instanceList); err != nil {
		log.Error(err, "Failed to get DSCInitializationList")
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "default-dsci"}}}
	}

	if len(instanceList.Items) == 0 {
		// No DSCI found, but trigger anyway for default name in case of race conditions
		// If no DSCI actually exists, the reconcile request will be ignored
		log.V(1).Info("No DSCI instances found, triggering default-dsci reconciliation as fallback to create VAP/VAPB")
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "default-dsci"}}}
	}

	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: instanceList.Items[0].Name}}}
}
