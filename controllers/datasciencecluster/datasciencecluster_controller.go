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

package datasciencecluster

import (
	"context"
	"fmt"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/client-go/util/retry"

	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/codeflare"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/ray"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/workbenches"
	appsv1 "k8s.io/api/apps/v1"
	netv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	corev1 "k8s.io/api/core/v1"
	authv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DataScienceClusterReconciler reconciles a DataScienceCluster object
type DataScienceClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
	// Recorder to generate events
	Recorder              record.EventRecorder
	ApplicationsNamespace string
}

//+kubebuilder:rbac:groups="datasciencepipelinesapplications.opendatahub.io",resources=datasciencepipelinesapplications/status,verbs=update;patch
//+kubebuilder:rbac:groups="datasciencepipelinesapplications.opendatahub.io",resources=datasciencepipelinesapplications/finalizers,verbs=update;patch
//+kubebuilder:rbac:groups="datasciencepipelinesapplications.opendatahub.io",resources=datasciencepipelinesapplications,verbs=create;delete;list;update;watch;patch

//+kubebuilder:rbac:groups="datasciencecluster.opendatahub.io",resources=datascienceclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="datasciencecluster.opendatahub.io",resources=datascienceclusters/finalizers,verbs=update;patch
//+kubebuilder:rbac:groups="datasciencecluster.opendatahub.io",resources=datascienceclusters,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups="opendatahub.io",resources=odhdashboardconfigs,verbs=create;get;patch;watch;update;delete;list
// +kubebuilder:rbac:groups="console.openshift.io",resources=odhquickstarts,verbs=create;get;patch;list;delete
// +kubebuilder:rbac:groups="dashboard.opendatahub.io",resources=odhdocuments,verbs=create;get;patch;list;delete
// +kubebuilder:rbac:groups="dashboard.opendatahub.io",resources=odhapplications,verbs=create;get;patch;list;delete
// +kubebuilder:rbac:groups="console.openshift.io",resources=consolelinks,verbs=create;get;patch;delete

// +kubebuilder:rbac:groups=operators.coreos.com,resources=clusterserviceversions,verbs=get;list;watch

// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch
// +kubebuilder:rbac:groups="operators.coreos.com",resources=customresourcedefinitions,verbs=create;get;patch;delete

// +kubebuilder:rbac:groups="user.openshift.io",resources=users,verbs=list;watch;patch;delete
// +kubebuilder:rbac:groups="user.openshift.io",resources=groups,verbs=get;create;list;watch;patch;delete

// +kubebuilder:rbac:groups="template.openshift.io",resources=templates,verbs=*

// +kubebuilder:rbac:groups="tekton.dev",resources=*,verbs=*

// +kubebuilder:rbac:groups="snapshot.storage.k8s.io",resources=volumesnapshots,verbs=create;delete;patch

// +kubebuilder:rbac:groups="serving.kserve.io",resources=trainedmodels/status,verbs=update;patch;delete
// +kubebuilder:rbac:groups="serving.kserve.io",resources=trainedmodels,verbs=create;delete;list;update;watch;patch
// +kubebuilder:rbac:groups="serving.kserve.io",resources=servingruntimes/status,verbs=update;patch
// +kubebuilder:rbac:groups="serving.kserve.io",resources=servingruntimes/finalizers,verbs=create;delete;list;update;watch;patch;get
// +kubebuilder:rbac:groups="serving.kserve.io",resources=servingruntimes,verbs=*
// +kubebuilder:rbac:groups="serving.kserve.io",resources=predictors/status,verbs=update;patch;delete
// +kubebuilder:rbac:groups="serving.kserve.io",resources=predictors/finalizers,verbs=update;patch
// +kubebuilder:rbac:groups="serving.kserve.io",resources=predictors,verbs=create;delete;list;update;watch;patch
// +kubebuilder:rbac:groups="serving.kserve.io",resources=inferenceservices/status,verbs=update;patch;delete
// +kubebuilder:rbac:groups="serving.kserve.io",resources=inferenceservices/finalizers,verbs=create;delete;list;update;watch;patch;get
// +kubebuilder:rbac:groups="serving.kserve.io",resources=inferenceservices,verbs=create;delete;list;update;watch;patch;get
// +kubebuilder:rbac:groups="serving.kserve.io",resources=inferencegraphs/status,verbs=update;patch;delete
// +kubebuilder:rbac:groups="serving.kserve.io",resources=inferencegraphs,verbs=create;delete;list;update;watch;patch
// +kubebuilder:rbac:groups="serving.kserve.io",resources=clusterservingruntimes/status,verbs=update;patch;delete
// +kubebuilder:rbac:groups="serving.kserve.io",resources=clusterservingruntimes/finalizers,verbs=create;delete;list;update;watch;patch
// +kubebuilder:rbac:groups="serving.kserve.io",resources=clusterservingruntimes,verbs=create;delete;list;update;watch;patch

// +kubebuilder:rbac:groups="serving.knative.dev",resources=services/status,verbs=update;patch;delete
// +kubebuilder:rbac:groups="serving.knative.dev",resources=services/finalizers,verbs=create;delete;list;watch;update;patch
// +kubebuilder:rbac:groups="serving.knative.dev",resources=services,verbs=create;delete;list;watch;update;patch

// +kubebuilder:rbac:groups="security.openshift.io",resources=securitycontextconstraints,verbs=*,resourceNames=restricted
// +kubebuilder:rbac:groups="security.openshift.io",resources=securitycontextconstraints,verbs=*,resourceNames=anyuid
// +kubebuilder:rbac:groups="security.openshift.io",resources=securitycontextconstraints,verbs=*

// +kubebuilder:rbac:groups="route.openshift.io",resources=routes,verbs=get;list;watch;create;delete;update;patch

// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=*

// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=rolebindings,verbs=*

// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterroles,verbs=*

// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterrolebindings,verbs=*

// +kubebuilder:rbac:groups="ray.io",resources=rayservices,verbs=create;delete;list;watch;update;patch
// +kubebuilder:rbac:groups="ray.io",resources=rayjobs,verbs=create;delete;list;update;watch;patch
// +kubebuilder:rbac:groups="ray.io",resources=rayclusters,verbs=create;delete;list;patch

// +kubebuilder:rbac:groups="operators.coreos.com",resources=subscriptions,verbs=get;list;watch

// +kubebuilder:rbac:groups="operator.openshift.io",resources=consoles,verbs=list;watch;patch;delete

// +kubebuilder:rbac:groups="oauth.openshift.io",resources=oauthclients,verbs=*

// +kubebuilder:rbac:groups="networking.k8s.io",resources=networkpolicies,verbs=get;create;list;watch;delete;update;patch
// +kubebuilder:rbac:groups="networking.k8s.io",resources=ingresses,verbs=create;delete;list;update;watch;patch;get

// +kubebuilder:rbac:groups="networking.istio.io",resources=virtualservices/status,verbs=update;patch;delete
// +kubebuilder:rbac:groups="networking.istio.io",resources=virtualservices/finalizers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="networking.istio.io",resources=virtualservices,verbs=*

// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=servicemonitors,verbs=get;create;delete;update;watch;list;patch
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=podmonitors,verbs=get;create;delete;update;watch;list;patch
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=prometheusrules,verbs=get;create;patch;delete
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=prometheuses,verbs=get;create;patch;delete

// +kubebuilder:rbac:groups="mcad.ibm.com",resources=appwrappers,verbs=create;delete;list;patch

// +kubebuilder:rbac:groups="machinelearning.seldon.io",resources=seldondeployments,verbs=*

// +kubebuilder:rbac:groups="machine.openshift.io",resources=machinesets,verbs=list;patch;delete
// +kubebuilder:rbac:groups="machine.openshift.io",resources=machineautoscalers,verbs=list;patch;delete

// +kubebuilder:rbac:groups="kubeflow.org",resources=*,verbs=*
// +kubebuilder:rbac:groups="kfdef.apps.kubeflow.org",resources=kfdefs,verbs=get;list;watch;patch;delete

// +kubebuilder:rbac:groups="integreatly.org",resources=rhmis,verbs=list;watch;patch;delete

// +kubebuilder:rbac:groups="image.openshift.io",resources=imagestreams,verbs=patch;create;update;delete
// +kubebuilder:rbac:groups="image.openshift.io",resources=imagestreams,verbs=create;list;watch;patch;delete

// +kubebuilder:rbac:groups="extensions",resources=replicasets,verbs=*
// +kubebuilder:rbac:groups="extensions",resources=ingresses,verbs=list;watch;patch;delete;get

// +kubebuilder:rbac:groups="dscinitialization.opendatahub.io",resources=dscinitializations/status,verbs=get;update;patch;delete
// +kubebuilder:rbac:groups="dscinitialization.opendatahub.io",resources=dscinitializations/finalizers,verbs=get;update;patch;delete
// +kubebuilder:rbac:groups="dscinitialization.opendatahub.io",resources=dscinitializations,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups="custom.tekton.dev",resources=pipelineloops,verbs=*

// +kubebuilder:rbac:groups="core",resources=services/finalizers,verbs=create;delete;list;update;watch;patch
// +kubebuilder:rbac:groups="core",resources=services,verbs=get;create;watch;update;patch;list;delete
// +kubebuilder:rbac:groups="core",resources=services,verbs=*
// +kubebuilder:rbac:groups="*",resources=services,verbs=*

// +kubebuilder:rbac:groups="core",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups="core",resources=secrets,verbs=*

// +kubebuilder:rbac:groups="core",resources=rhmis,verbs=watch;list

// +kubebuilder:rbac:groups="core",resources=pods/log,verbs=*
// +kubebuilder:rbac:groups="core",resources=pods/exec,verbs=*
// +kubebuilder:rbac:groups="core",resources=pods,verbs=*

// +kubebuilder:rbac:groups="core",resources=persistentvolumes,verbs=*
// +kubebuilder:rbac:groups="core",resources=persistentvolumeclaims,verbs=*

// +kubebuilder:rbac:groups="core",resources=namespaces/finalizers,verbs=update;list;watch;patch;delete
// +kubebuilder:rbac:groups="core",resources=namespaces,verbs=update;patch;delete
// +kubebuilder:rbac:groups="core",resources=namespaces,verbs=get;create;patch;delete;watch

// +kubebuilder:rbac:groups="core",resources=events,verbs=get;create;watch;update;list;patch;delete
// +kubebuilder:rbac:groups="core",resources=events,verbs=delete
// +kubebuilder:rbac:groups="events.k8s.io",resources=events,verbs=list;watch;patch;delete

// +kubebuilder:rbac:groups="core",resources=endpoints,verbs=watch;list

// +kubebuilder:rbac:groups="core",resources=configmaps/status,verbs=get;update;patch;delete
// +kubebuilder:rbac:groups="core",resources=configmaps,verbs=get;create;watch;patch;delete

// +kubebuilder:rbac:groups="core",resources=clusterversions,verbs=watch;list
// +kubebuilder:rbac:groups="config.openshift.io",resources=clusterversions,verbs=watch;list

// +kubebuilder:rbac:groups="coordination.k8s.io",resources=leases,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups="controller-runtime.sigs.k8s.io",resources=controllermanagerconfigs,verbs=get;create;patch;delete

// +kubebuilder:rbac:groups="codeflare.codeflare.dev",resources=mcads,verbs=create;patch
// +kubebuilder:rbac:groups="codeflare.codeflare.dev",resources=instascales,verbs=create;patch

// +kubebuilder:rbac:groups="cert-manager.io",resources=certificates;issuers,verbs=create;patch

// +kubebuilder:rbac:groups="build.openshift.io",resources=builds,verbs=create;patch;delete;list;watch
// +kubebuilder:rbac:groups="build.openshift.io",resources=buildconfigs/instantiate,verbs=create;patch;delete;get;list;watch
// +kubebuilder:rbac:groups="build.openshift.io",resources=buildconfigs,verbs=list;watch;create;patch;delete

// +kubebuilder:rbac:groups="batch",resources=jobs/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="batch",resources=jobs,verbs=*
// +kubebuilder:rbac:groups="batch",resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="batch",resources=cronjobs,verbs=create;get;patch

// +kubebuilder:rbac:groups="autoscaling",resources=horizontalpodautoscalers,verbs=watch;create;update;delete;list;patch
// +kubebuilder:rbac:groups="autoscaling.openshift.io",resources=machinesets,verbs=list;patch;delete
// +kubebuilder:rbac:groups="autoscaling.openshift.io",resources=machineautoscalers,verbs=list;patch;delete

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

// +kubebuilder:rbac:groups="apps.openshift.io",resources=deploymentconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="apps.openshift.io",resources=deploymentconfigs/instantiate,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups="apiextensions.k8s.io",resources=customresourcedefinitions,verbs=get;list;watch;create;patch;delete

// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=validatingwebhookconfigurations,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=mutatingwebhookconfigurations,verbs=create;delete;list;update;watch;patch

// +kubebuilder:rbac:groups="addons.managed.openshift.io",resources=addons,verbs=get

// +kubebuilder:rbac:groups="*",resources=statefulsets,verbs=create;update;get;list;watch;patch;delete

// +kubebuilder:rbac:groups="*",resources=replicasets,verbs=*

// +kubebuilder:rbac:groups="*",resources=customresourcedefinitions,verbs=get;list;watch

// +kubebuilder:rbac:groups="maistra.io",resources=servicemeshcontrolplanes,verbs=create;get;list;patch;update;use;watch
// +kubebuilder:rbac:groups="maistra.io",resources=servicemeshmemberrolls,verbs=create;get;list;patch;update;use;watch
// +kubebuilder:rbac:groups="maistra.io",resources=servicemeshmembers,verbs=create;get;list;patch;update;use;watch
// +kubebuilder:rbac:groups="maistra.io",resources=servicemeshmembers/finalizers,verbs=create;get;list;patch;update;use;watch

// +kubebuilder:rbac:groups="security.istio.io",resources=peerauthentications,verbs=create;get;list;patch;update;use;watch
// +kubebuilder:rbac:groups="telemetry.istio.io",resources=telemetries,verbs=create;get;list;patch;update;use;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DataScienceClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("Reconciling DataScienceCluster resources", "Request.Namespace", req.Namespace, "Request.Name", req.Name)

	instance := &dsc.DataScienceCluster{}

	// First check if instance is being deleted, return
	if instance.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	// Second check if instance exists, return
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: req.Name}, instance)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// DataScienceCluster instance not found
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Last check if multiple instances of DataScienceCluster exist
	instanceList := &dsc.DataScienceClusterList{}
	err = r.Client.List(context.TODO(), instanceList)
	if err != nil {
		return ctrl.Result{}, err
	}

	if len(instanceList.Items) > 1 {
		message := fmt.Sprintf("only one instance of DataScienceCluster object is allowed. Update existing instance on namespace %s and name %s", req.Namespace, req.Name)
		_ = r.reportError(err, &instanceList.Items[0], message)
		return ctrl.Result{}, fmt.Errorf(message)
	}

	// Start reconciling
	if instance.Status.Conditions == nil {
		reason := status.ReconcileInit
		message := "Initializing DataScienceCluster resource"
		instance, err = r.updateStatus(instance, func(saved *dsc.DataScienceCluster) {
			status.SetProgressingCondition(&saved.Status.Conditions, reason, message)
			saved.Status.Phase = status.PhaseProgressing
		})
		if err != nil {
			_ = r.reportError(err, instance, fmt.Sprintf("failed to add conditions to status of DataScienceCluster resource on namespace %s and name %s", req.Namespace, req.Name))
			return ctrl.Result{}, err
		}
	}

	// Verify a valid DSCInitialization instance is created
	dsciInstances := &dsci.DSCInitializationList{}
	err = r.Client.List(ctx, dsciInstances)
	if err != nil {
		r.Log.Error(err, "Failed to retrieve DSCInitialization resource.", "DSCInitialization", req.Namespace, "Request.Name", req.Name)
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Failed to retrieve DSCInitialization instance")
		return ctrl.Result{}, err
	}

	// Update phase to error state if DataScienceCluster is created without valid DSCInitialization
	if len(dsciInstances.Items) == 0 {
		reason := status.ReconcileFailed
		message := "Failed to get a valid DSCInitialization instance"
		instance, err = r.updateStatus(instance, func(saved *dsc.DataScienceCluster) {
			status.SetProgressingCondition(&saved.Status.Conditions, reason, message)
			saved.Status.Phase = status.PhaseError
		})
		if err != nil {
			r.Log.Error(err, "failed to update DataScienceCluster condition")
			return ctrl.Result{}, err
		} else {
			return ctrl.Result{}, nil
		}
	} else if len(dsciInstances.Items) == 1 {
		// Set Applications namespace defined in DSCInitialization
		r.ApplicationsNamespace = dsciInstances.Items[0].Spec.ApplicationsNamespace
	} else {
		return ctrl.Result{}, fmt.Errorf(fmt.Sprintf("only one instance of DSCInitialization object is allowed."))
	}

	// Ensure all omitted components show up as explicitly disabled
	instance, err = r.updateComponents(instance)
	if err != nil {
		_ = r.reportError(err, instance, "error updating list of components in the CR")
		return ctrl.Result{}, err
	}

	// Initialize error list, instead of returning errors after every component is deployed
	componentErrorList := make(map[string]error)

	// reconcile dashboard component
	if instance, err = r.reconcileSubComponent(instance, dashboard.ComponentName, instance.Spec.Components.Dashboard.Enabled,
		&(instance.Spec.Components.Dashboard)); err != nil {
		// no need to log any errors as this is done in the reconcileSubComponent method
		componentErrorList[dashboard.ComponentName] = err
	}

	// reconcile DataSciencePipelines component
	if instance, err = r.reconcileSubComponent(instance, datasciencepipelines.ComponentName, instance.Spec.Components.DataSciencePipelines.Enabled,
		&(instance.Spec.Components.DataSciencePipelines)); err != nil {
		// no need to log any errors as this is done in the reconcileSubComponent method
		componentErrorList[datasciencepipelines.ComponentName] = err
	}

	// reconcile Workbench component
	if instance, err = r.reconcileSubComponent(instance, workbenches.ComponentName, instance.Spec.Components.Workbenches.Enabled,
		&(instance.Spec.Components.Workbenches)); err != nil {
		// no need to log any errors as this is done in the reconcileSubComponent method
		componentErrorList[workbenches.ComponentName] = err
	}

	// reconcile Kserve component
	if instance, err = r.reconcileSubComponent(instance, kserve.ComponentName, instance.Spec.Components.Kserve.Enabled, &(instance.Spec.Components.Kserve)); err != nil {
		// no need to log any errors as this is done in the reconcileSubComponent method
		componentErrorList[kserve.ComponentName] = err
	}

	// reconcile ModelMesh component
	if instance, err = r.reconcileSubComponent(instance, modelmeshserving.ComponentName, instance.Spec.Components.ModelMeshServing.Enabled,
		&(instance.Spec.Components.ModelMeshServing)); err != nil {
		// no need to log any errors as this is done in the reconcileSubComponent method
		componentErrorList[modelmeshserving.ComponentName] = err
	}

	// reconcile CodeFlare component
	if instance, err = r.reconcileSubComponent(instance, codeflare.ComponentName, instance.Spec.Components.CodeFlare.Enabled, &(instance.Spec.Components.CodeFlare)); err != nil {
		// no need to log any errors as this is done in the reconcileSubComponent method
		componentErrorList[codeflare.ComponentName] = err
	}

	// reconcile Ray component
	if instance, err = r.reconcileSubComponent(instance, ray.ComponentName, instance.Spec.Components.Ray.Enabled, &(instance.Spec.Components.Ray)); err != nil {
		// no need to log any errors as this is done in the reconcileSubComponent method
		componentErrorList[ray.ComponentName] = err
	}

	// Process errors for components
	if componentErrorList != nil && len(componentErrorList) != 0 {
		r.Log.Info("DataScienceCluster Deployment Incomplete.")
		instance, err = r.updateStatus(instance, func(saved *dsc.DataScienceCluster) {
			status.SetCompleteCondition(&saved.Status.Conditions, status.ReconcileCompletedWithComponentErrors,
				fmt.Sprintf("DataScienceCluster resource reconciled with component errors: %v", fmt.Sprint(componentErrorList)))
			saved.Status.Phase = status.PhaseReady
		})
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, "DataScienceClusterComponentFailures",
			"DataScienceCluster instance %s created, but have some failures in component %v", instance.Name, fmt.Sprint(componentErrorList))
		return ctrl.Result{RequeueAfter: time.Second * 10}, fmt.Errorf(fmt.Sprint(componentErrorList))
	}

	// finalize reconciliation
	instance, err = r.updateStatus(instance, func(saved *dsc.DataScienceCluster) {
		status.SetCompleteCondition(&saved.Status.Conditions, status.ReconcileCompleted, "DataScienceCluster resource reconciled successfully")
		saved.Status.Phase = status.PhaseReady
	})
	if err != nil {
		r.Log.Error(err, "failed to update DataScienceCluster conditions after successfuly completed reconciliation")
		return ctrl.Result{}, err
	}

	r.Log.Info("DataScienceCluster Deployment Completed.")
	r.Recorder.Eventf(instance, corev1.EventTypeNormal, "DataScienceClusterCreationSuccessful",
		"DataScienceCluster instance %s created and deployed successfully", instance.Name)

	return ctrl.Result{}, nil
}

func (r *DataScienceClusterReconciler) reconcileSubComponent(instance *dsc.DataScienceCluster, componentName string, enabled bool,
	component components.ComponentInterface) (*dsc.DataScienceCluster, error) {

	// First set contidions to reflect a component is about to be reconciled
	instance, err := r.updateStatus(instance, func(saved *dsc.DataScienceCluster) {
		if enabled {
			status.SetComponentCondition(&saved.Status.Conditions, componentName, status.ReconcileInit, "Component is enabled", corev1.ConditionUnknown)
		} else {
			status.SetComponentCondition(&saved.Status.Conditions, componentName, status.ReconcileInit, "Component is disabled", corev1.ConditionUnknown)
		}
	})
	if err != nil {
		instance = r.reportError(err, instance, "failed to update DataScienceCluster conditions before reconciling "+componentName)
		// try to continue with reconciliation, as further updates can fix the status
	}

	// Reconcile component
	err = component.ReconcileComponent(instance, r.Client, r.Scheme, enabled, r.ApplicationsNamespace)

	if err != nil {
		// reconciliation failed: log errors, raise event and update status accordingly
		instance = r.reportError(err, instance, "failed to reconcile "+componentName+" on DataScienceCluster")
		instance, _ = r.updateStatus(instance, func(saved *dsc.DataScienceCluster) {
			if enabled {
				status.SetComponentCondition(&saved.Status.Conditions, componentName, status.ReconcileFailed, fmt.Sprintf("Component reconciliation failed: %v", err), corev1.ConditionFalse)
			} else {
				status.SetComponentCondition(&saved.Status.Conditions, componentName, status.ReconcileFailed, fmt.Sprintf("Component removal failed: %v", err), corev1.ConditionFalse)
			}
		})
		return instance, err
	} else {
		// reconciliation succeeded: update status accordingly
		instance, err = r.updateStatus(instance, func(saved *dsc.DataScienceCluster) {
			if saved.Status.InstalledComponents == nil {
				saved.Status.InstalledComponents = make(map[string]bool)
			}
			saved.Status.InstalledComponents[componentName] = enabled
			if enabled {
				status.SetComponentCondition(&saved.Status.Conditions, componentName, status.ReconcileCompleted, "Component reconciled successfully", corev1.ConditionTrue)
			} else {
				status.RemoveComponentCondition(&saved.Status.Conditions, componentName)
			}
		})
		if err != nil {
			instance = r.reportError(err, instance, "failed to update DataScienceCluster status after reconciling "+componentName)
			return instance, err
		}
	}
	return instance, nil
}

func (r *DataScienceClusterReconciler) reportError(err error, instance *dsc.DataScienceCluster, message string) *dsc.DataScienceCluster {
	r.Log.Error(err, message, "instance.Name", instance.Name)
	r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DataScienceClusterReconcileError",
		"%s for instance %s", message, instance.Name)
	// TODO:Set error phase only for creation/deletion errors of DSC CR
	//instance, err = r.updateStatus(instance, func(saved *dsc.DataScienceCluster) {
	//	status.SetErrorCondition(&saved.Status.Conditions, status.ReconcileFailed, fmt.Sprintf("%s : %v", message, err))
	//	saved.Status.Phase = status.PhaseError
	//})
	//if err != nil {
	//	r.Log.Error(err, "failed to update DataScienceCluster status after error", "instance.Name", instance.Name)
	//}
	return instance
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataScienceClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dsc.DataScienceCluster{}).
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
		Watches(&source.Kind{Type: &dsci.DSCInitialization{}}, handler.EnqueueRequestsFromMapFunc(r.watchDataScienceClusterResources)).
		// this predicates prevents meaningless reconciliations from being triggered
		WithEventFilter(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{})).
		Complete(r)
}

func (r *DataScienceClusterReconciler) updateStatus(original *dsc.DataScienceCluster, update func(saved *dsc.DataScienceCluster)) (*dsc.DataScienceCluster, error) {
	saved := &dsc.DataScienceCluster{}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {

		err := r.Client.Get(context.TODO(), client.ObjectKeyFromObject(original), saved)
		if err != nil {
			return err
		}
		// update status here
		update(saved)

		// Try to update
		err = r.Client.Status().Update(context.TODO(), saved)
		// Return err itself here (not wrapped inside another error)
		// so that RetryOnConflict can identify it correctly.
		return err
	})
	return saved, err
}

func (r *DataScienceClusterReconciler) updateComponents(original *dsc.DataScienceCluster) (*dsc.DataScienceCluster, error) {
	saved := &dsc.DataScienceCluster{}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {

		err := r.Client.Get(context.TODO(), client.ObjectKeyFromObject(original), saved)
		if err != nil {
			return err
		}

		// Try to update
		err = r.Client.Update(context.TODO(), saved)
		// Return err itself here (not wrapped inside another error)
		// so that RetryOnConflict can identify it correctly.
		return err
	})
	return saved, err
}

func (r *DataScienceClusterReconciler) watchDataScienceClusterResources(a client.Object) (requests []reconcile.Request) {
	instanceList := &dsc.DataScienceClusterList{}
	err := r.Client.List(context.TODO(), instanceList)
	if err != nil {
		return nil
	}
	if len(instanceList.Items) == 1 {
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{Name: instanceList.Items[0].Name}}}
	} else {
		return nil
	}
}
