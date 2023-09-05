/*
Copyright 2022.

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

package kfdefappskubefloworg

import (
	"context"
	"fmt"
	"github.com/ghodss/yaml"
	"github.com/go-logr/logr"
	ocappsv1 "github.com/openshift/api/apps/v1"
	ocbuildv1 "github.com/openshift/api/build/v1"
	ocimgv1 "github.com/openshift/api/image/v1"
	"io/ioutil"
	admv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	"os"
	"path"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strings"

	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	olmclientset "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned/typed/operators/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	apiserv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kftypesv3 "github.com/opendatahub-io/opendatahub-operator/apis/apps"
	kfdefappskubefloworgv1 "github.com/opendatahub-io/opendatahub-operator/apis/kfdef.apps.kubeflow.org/v1"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfapp/coordinator"
	kfloaders "github.com/opendatahub-io/opendatahub-operator/pkg/kfconfig/loaders"
	kfutils "github.com/opendatahub-io/opendatahub-operator/pkg/utils"
)

const (
	finalizer = "kfdef-finalizer.kfdef.apps.kubeflow.org"
	// finalizerMaxRetries defines the maximum number of attempts to add finalizers.
	finalizerMaxRetries = 10
	// deleteConfigMapLabel is the label for configMap used to trigger operator uninstall
	// TODO: Label should be updated if addon name changes
	deleteConfigMapLabel = "api.openshift.com/addon-managed-odh-delete"
	// odhGeneratedNamespaceLabel is the label added to all the namespaces genereated by odh-deployer
	odhGeneratedNamespaceLabel = "opendatahub.io/generated-namespace"
)

// kfdefInstances keep all KfDef CRs watched by the operator
var kfdefInstances = make(map[string]struct{})

// Add logger for helper functions
var kfdefLog logr.Logger

// the stop Context for the 2nd controller
//var stopCtx context.Context

// KfDefReconciler reconciles a KfDef object
type KfDefReconciler struct {
	Client     client.Client
	Scheme     *runtime.Scheme
	RestConfig *rest.Config
	Log        logr.Logger
	// Recorder to generate events
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=*,resources=*,verbs=*

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the KfDef object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.2/pkg/reconcile

func (r *KfDefReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("Reconciling KfDef resources", "Request.Namespace", request.Namespace, "Request.Name", request.Name)

	instance := &kfdefappskubefloworgv1.KfDef{}
	err := r.Client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			if hasDeleteConfigMap(r.Client) {
				r.Recorder.Eventf(instance, v1.EventTypeWarning, "UninstallInProgress",
					"Resource deletion restricted as the operator uninstall is in progress")
				return ctrl.Result{}, fmt.Errorf("error while operator uninstall: %v",
					r.operatorUninstall(request))

			}
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	deleted := instance.GetDeletionTimestamp() != nil
	finalizers := sets.NewString(instance.GetFinalizers()...)
	if deleted {
		if !finalizers.Has(finalizer) {
			r.Log.Info("Kfdef instance deleted.", "instance", instance.Name)
			if hasDeleteConfigMap(r.Client) {
				// if delete configmap exists, requeue the request to handle operator uninstall
				return ctrl.Result{Requeue: true}, err
			}
			return ctrl.Result{}, nil
		}
		r.Log.Info("Deleting kfdef instance", "instance", instance.Name)

		// Uninstall Kubeflow
		err = kfDelete(instance)
		if err == nil {
			r.Log.Info("KubeFlow Deployment Deleted.")
			r.Recorder.Eventf(instance, v1.EventTypeNormal, "KfDefDeletionSuccessful",
				"KF instance %s deleted successfully", instance.Name)
		} else {
			// log an error and continue for cleanup. It does not make sense to retry the delete.
			r.Recorder.Eventf(instance, v1.EventTypeWarning, "KfDefDeletionFailed",
				"Error deleting KF instance %s", instance.Name)
			r.Log.Error(fmt.Errorf("failed to delete Kubeflow"), "", "instance", instance.Name)
		}

		// Delete the kfapp directory
		kfAppDir := path.Join("/tmp", instance.GetNamespace(), instance.GetName())
		if err := os.RemoveAll(kfAppDir); err != nil {
			r.Log.Error(err, "Failed to delete the app directory")
			return ctrl.Result{}, err
		}
		r.Log.Info("kfAppDir deleted.")

		// Remove this KfDef instance
		delete(kfdefInstances, strings.Join([]string{instance.GetName(), instance.GetNamespace()}, "."))

		// Remove finalizer once kfDelete is completed.
		finalizers.Delete(finalizer)
		instance.SetFinalizers(finalizers.List())
		finalizerError := r.Client.Update(context.TODO(), instance)
		for retryCount := 0; errors.IsConflict(finalizerError) && retryCount < finalizerMaxRetries; retryCount++ {
			// Based on Istio operator at https://github.com/istio/istio/blob/master/operator/pkg/controller/istiocontrolplane/istiocontrolplane_controller.go
			// for finalizer removal errors workaround.
			r.Log.Info("Conflict during finalizer removal, retrying.")
			_ = r.Client.Get(ctx, request.NamespacedName, instance)
			finalizers = sets.NewString(instance.GetFinalizers()...)
			finalizers.Delete(finalizer)
			instance.SetFinalizers(finalizers.List())
			finalizerError = r.Client.Update(ctx, instance)
		}
		if finalizerError != nil {
			r.Log.Error(finalizerError, "error removing finalizer")
			return ctrl.Result{}, finalizerError
		}
		if hasDeleteConfigMap(r.Client) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, nil
	} else if !finalizers.Has(finalizer) {
		r.Log.Info("Normally this should not happen. Adding the finalizer", finalizer, request)
		finalizers.Insert(finalizer)
		instance.SetFinalizers(finalizers.List())
		err = r.Client.Update(ctx, instance)
		if err != nil {
			r.Log.Error(err, "failed to update kfdef with finalizer")
			return ctrl.Result{}, err
		}
	}

	// If this is a kfdef change, for now, remove the kfapp config path
	if request.Name == instance.GetName() && request.Namespace == instance.GetNamespace() {
		kfAppDir := path.Join("/tmp", instance.GetNamespace(), instance.GetName())
		if err = os.RemoveAll(kfAppDir); err != nil {
			r.Log.Error(err, "failed to delete the app directory")
			return ctrl.Result{}, err
		}
	}

	if hasDeleteConfigMap(r.Client) {
		for key, _ := range kfdefInstances {
			keyVal := strings.Split(key, ".")
			if len(keyVal) == 2 {
				instanceName, namespace := keyVal[0], keyVal[1]
				currentInstance := &kfdefappskubefloworgv1.KfDef{
					ObjectMeta: metav1.ObjectMeta{
						Name:      instanceName,
						Namespace: namespace,
					},
				}

				if err := r.Client.Delete(ctx, currentInstance, []client.DeleteOption{}...); err != nil {
					if !errors.IsNotFound(err) {
						return ctrl.Result{}, err
					}
				}
			} else {
				return ctrl.Result{}, fmt.Errorf("error getting kfdef instance name and namespace")
			}

		}

		return ctrl.Result{Requeue: true}, nil
	}

	err = getReconcileStatus(instance, kfApply(instance))
	if err == nil {
		r.Log.Info("KubeFlow Deployment Completed.")
		r.Recorder.Eventf(instance, v1.EventTypeNormal, "KfDefCreationSuccessful",
			"KfDef instance %s created and deployed successfully", instance.Name)

		// add to kfdefInstances if not exists
		if _, ok := kfdefInstances[strings.Join([]string{instance.GetName(), instance.GetNamespace()}, ".")]; !ok {
			kfdefInstances[strings.Join([]string{instance.GetName(), instance.GetNamespace()}, ".")] = struct{}{}
		}

	}

	// set status of the KfDef resource
	if reconcileError := r.reconcileStatus(instance); reconcileError != nil {
		return ctrl.Result{}, reconcileError
	}

	return ctrl.Result{}, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *KfDefReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Log.Info("Adding controller for kfdef.")

	watchKfdefHandler := handler.EnqueueRequestsFromMapFunc(r.watchKfDef)
	watchedHandler := handler.EnqueueRequestsFromMapFunc(r.watchKubeflowResources)

	err := ctrl.NewControllerManagedBy(mgr).Named("kfdef-controller").
		For(&kfdefappskubefloworgv1.KfDef{}).
		Watches(&source.Kind{Type: &kfdefappskubefloworgv1.KfDef{}}, watchKfdefHandler, builder.WithPredicates(kfdefPredicates)).
		Watches(&source.Kind{Type: &appsv1.Deployment{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &v1.Namespace{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &v1.PersistentVolumeClaim{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &v1.Service{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &appsv1.DaemonSet{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &appsv1.StatefulSet{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &ocappsv1.DeploymentConfig{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &ocimgv1.ImageStream{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &ocbuildv1.BuildConfig{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &apiextensionsv1.CustomResourceDefinition{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &apiregistrationv1.APIService{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &netv1.Ingress{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &admv1.MutatingWebhookConfiguration{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &admv1.ValidatingWebhookConfiguration{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &v1.Secret{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &v1.ConfigMap{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &v1.ServiceAccount{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &rbacv1.Role{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &rbacv1.RoleBinding{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &rbacv1.ClusterRole{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Watches(&source.Kind{Type: &rbacv1.ClusterRoleBinding{}}, watchedHandler, builder.WithPredicates(ownedResourcePredicates)).
		Complete(r)

	if err != nil {
		return err
	}
	kfdefLog = r.Log
	return nil
}

func (r *KfDefReconciler) watchKfDef(a client.Object) (requests []reconcile.Request) {
	namespacedName := types.NamespacedName{Name: a.GetName(), Namespace: a.GetNamespace()}
	finalizers := sets.NewString(a.GetFinalizers()...)
	if !finalizers.Has(finalizer) {
		// assume this is a CREATE event
		r.Log.Info("Adding finalizer", finalizer, namespacedName)
		finalizers.Insert(finalizer)
		instance := &kfdefappskubefloworgv1.KfDef{}
		err := r.Client.Get(context.TODO(), namespacedName, instance)
		if err != nil {
			r.Log.Error(err, "Failed to get kfdef CR.")
			return nil
		}
		instance.SetFinalizers(finalizers.List())
		err = r.Client.Update(context.TODO(), instance)
		if err != nil {
			r.Log.Error(err, "Failed to update kfdef with finalizer. Error: %v.")
		}
		// let the UPDATE event request queue
		return nil
	}
	r.Log.Info("Watch a change for KfDef CR", "instance", a.GetName(), "namespace", a.GetNamespace())
	return []reconcile.Request{{NamespacedName: namespacedName}}

}

// watch is monitoring changes for kfctl resources managed by the operator
func (r *KfDefReconciler) watchKubeflowResources(a client.Object) (requests []reconcile.Request) {
	anns := a.GetAnnotations()
	kfdefAnn := strings.Join([]string{kfutils.KfDefAnnotation, kfutils.KfDefInstance}, "/")
	_, found := anns[kfdefAnn]
	if found {
		kfdefCr := strings.Split(anns[kfdefAnn], ".")
		namespacedName := types.NamespacedName{Name: kfdefCr[0], Namespace: kfdefCr[1]}
		instance := &kfdefappskubefloworgv1.KfDef{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: kfdefCr[0], Namespace: kfdefCr[1]}, instance)
		if err != nil {
			if errors.IsNotFound(err) {
				// KfDef CR may have been deleted
				return nil
			}
		} else if instance.GetDeletionTimestamp() != nil {
			// KfDef is being deleted
			return nil
		}
		r.Log.Info("Watch a change for Kubeflow resource", "instance", a.GetName(), "namespace", a.GetNamespace())
		return []reconcile.Request{{NamespacedName: namespacedName}}
	} else if a.GetObjectKind().GroupVersionKind().Kind == "ConfigMap" {
		labels := a.GetLabels()
		if val, ok := labels[deleteConfigMapLabel]; ok {
			if val == "true" {
				for k := range kfdefInstances {
					kfdefCr := strings.Split(k, ".")
					return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: kfdefCr[0], Namespace: kfdefCr[1]}}}
				}
			}
		}
	}
	return nil

}

var kfdefPredicates = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		return true
	},
	GenericFunc: func(e event.GenericEvent) bool {
		return true
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return false
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		return true
	},
}

var ownedResourcePredicates = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		// handle create event if object has kind configMap
		if e.Object.GetObjectKind().GroupVersionKind().Kind == "ConfigMap" {
			labels := e.Object.GetLabels()
			if val, ok := labels[deleteConfigMapLabel]; ok {
				if val == "true" {
					return true
				}
			}
		}

		return false
	},
	GenericFunc: func(_ event.GenericEvent) bool {
		// no action
		return false
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		// handle deletion event
		object, err := meta.Accessor(e.Object)
		if err != nil {
			return false
		}
		// if this object has an owner, let the owner handle the appropriate recovery
		if len(object.GetOwnerReferences()) > 0 {
			return false
		}
		return true
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		// handle update events
		object, err := meta.Accessor(e.ObjectOld)
		if err != nil {
			return false
		}
		// if this object has an owner, let the owner handle the appropriate recovery
		if len(object.GetOwnerReferences()) > 0 {
			return false
		}
		// TODO:  Add update log message when plugin is integrated. We need to only log events for the resources with 'configurable' label
		return true
	},
}

// kfApply is equivalent of kfctl apply
func kfApply(instance *kfdefappskubefloworgv1.KfDef) error {
	kfdefLog.Info("Creating a new KubeFlow Deployment", "KubeFlow.Namespace", instance.Namespace)
	kfApp, err := kfLoadConfig(instance, "apply")
	if err != nil {
		kfdefLog.Error(err, "failed to load KfApp")
		return err
	}
	// Apply kfApp.
	err = kfApp.Apply(kftypesv3.K8S)
	return err
}

// kfDelete is equivalent of kfctl delete
func kfDelete(instance *kfdefappskubefloworgv1.KfDef) error {
	kfdefLog.Info("Uninstall Kubeflow.", "KubeFlow.Namespace", instance.Namespace)
	kfApp, err := kfLoadConfig(instance, "delete")
	if err != nil {
		kfdefLog.Error(err, "Failed to load KfApp")
		return err
	}
	// Delete kfApp.
	err = kfApp.Delete(kftypesv3.K8S)
	return err
}

func kfLoadConfig(instance *kfdefappskubefloworgv1.KfDef, action string) (kftypesv3.KfApp, error) {
	// Define kfApp
	kfdefBytes, _ := yaml.Marshal(instance)

	// Make the kfApp directory
	kfAppDir := path.Join("/tmp", instance.GetNamespace(), instance.GetName())
	if err := os.MkdirAll(kfAppDir, 0755); err != nil {
		kfdefLog.Error(err, "Failed to create the app directory")
		return nil, err
	}

	configFilePath := path.Join(kfAppDir, "config.yaml")
	err := ioutil.WriteFile(configFilePath, kfdefBytes, 0644)
	if err != nil {
		kfdefLog.Error(err, "Failed to write config.yaml")
		return nil, err
	}

	if action == "apply" {
		// Indicate to add annotation to the top level resources
		setAnnotationAnn := strings.Join([]string{kfutils.KfDefAnnotation, kfutils.SetAnnotation}, "/")
		setAnnotations(configFilePath, map[string]string{
			setAnnotationAnn: "true",
		})
	}

	if action == "delete" {
		// Enable force delete since inClusterConfig has no ./kube/config file to pass the delete safety check.
		forceDeleteAnn := strings.Join([]string{kfutils.KfDefAnnotation, kfutils.ForceDelete}, "/")
		setAnnotations(configFilePath, map[string]string{
			forceDeleteAnn: "true",
		})

		// Indicate the Kubeflow is installed by the operator
		byOperatorAnn := strings.Join([]string{kfutils.KfDefAnnotation, kfutils.InstallByOperator}, "/")
		setAnnotations(configFilePath, map[string]string{
			byOperatorAnn: "true",
		})
	}

	kfApp, err := coordinator.NewLoadKfAppFromURI(configFilePath)
	if err != nil {
		kfdefLog.Error(err, "failed to build kfApp from URI", "uri", configFilePath)

		return nil, err
	}
	return kfApp, nil
}

func setAnnotations(configPath string, annotations map[string]string) error {
	config, err := kfloaders.LoadConfigFromURI(configPath)
	if err != nil {
		return err
	}
	anns := config.GetAnnotations()
	if anns == nil {
		anns = map[string]string{}
	}
	for ann, val := range annotations {
		anns[ann] = val
	}
	config.SetAnnotations(anns)
	return kfloaders.WriteConfigToFile(*config)
}

// getClusterServiceVersion retries the clusterserviceversions available in the operator namespace.
func getClusterServiceVersion(cfg *rest.Config, watchNameSpace string) (*ofapi.ClusterServiceVersion, error) {

	operatorClient, err := olmclientset.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error getting operator client %v", err)
	}
	csvs, err := operatorClient.ClusterServiceVersions(watchNameSpace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// get csv with CR KfDef
	if len(csvs.Items) != 0 {
		for _, csv := range csvs.Items {
			for _, operatorCR := range csv.Spec.CustomResourceDefinitions.Owned {
				if operatorCR.Kind == string(kftypesv3.KFDEF) {
					return &csv, nil
				}
			}
		}
	}
	return nil, nil
}

// operatorUninstall deletes all the externally generated resources. This includes monitoring resources and applications
// installed by KfDef.
func (r *KfDefReconciler) operatorUninstall(request reconcile.Request) error {

	// Delete namespace for the given request
	namespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: request.Namespace,
	}}

	if err := r.Client.Delete(context.TODO(), namespace); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("error deleting current namespace :%v", err)
		}
	}
	r.Recorder.Eventf(namespace, v1.EventTypeNormal, "NamespaceDeletionSuccessful",
		"Namespace %s deleted as a part of uninstall.", namespace.Name)
	kfdefLog.Info("Namespace deleted as a part of uninstall.", "namespace", namespace.Name)

	// Delete any unavailable api services
	apiservices := &apiserv1.APIServiceList{}
	if err := r.Client.List(context.TODO(), apiservices); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("error getting dangling apiservices : %v", err)
		}
	}

	if len(apiservices.Items) != 0 {
		for _, apiservice := range apiservices.Items {
			conditionsLength := len(apiservice.Status.Conditions)
			if conditionsLength >= 1 {
				if apiservice.Status.Conditions[conditionsLength-1].Status == apiserv1.ConditionFalse {
					if err := r.Client.Delete(context.TODO(), &apiservice, []client.DeleteOption{}...); err != nil {
						return fmt.Errorf("error deleting apiservice %v: %v", apiservice.Name, err)
					}
				}
			}
			kfdefLog.Info("Unavailable api service is deleted", "api", apiservice.Name)

		}
	}

	// Wait until all kfdef instances and corresponding namespaces are deleted
	if len(kfdefInstances) != 0 {
		return fmt.Errorf("waiting for KfDef instances to be deleted")
	}

	// Delete generated namespaces that do not have KfDef instance
	generatedNamespaces := &v1.NamespaceList{}
	nsOptions := []client.ListOption{
		client.MatchingLabels{odhGeneratedNamespaceLabel: "true"},
	}
	if err := r.Client.List(context.TODO(), generatedNamespaces, nsOptions...); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("error getting generated namespaces : %v", err)
		}
	}
	if len(generatedNamespaces.Items) != 0 {
		for _, namespace := range generatedNamespaces.Items {
			if namespace.Status.Phase == v1.NamespaceActive {
				if err := r.Client.Delete(context.TODO(), &namespace, []client.DeleteOption{}...); err != nil {
					return fmt.Errorf("error deleting namespace %v: %v", namespace.Name, err)
				}
				r.Recorder.Eventf(&namespace, v1.EventTypeNormal, "NamespaceDeletionSuccessful",
					"Namespace %s deleted as a part of uninstall.", namespace.Name)
				kfdefLog.Info("Namespace deleted as a part of uninstall.", "namespace", namespace.Name)
			}
		}
	}
	kfdefLog.Info("All resources deleted as part of uninstall. Removing the operator csv")
	return removeCsv(r.Client, r.RestConfig)
}

// hasDeleteConfigMap returns true if delete configMap is added to the operator namespace by managed-tenants repo.
// It returns false in all other cases.
func hasDeleteConfigMap(c client.Client) bool {
	// Get watchNamespace
	operatorNamespace, err := getOperatorNamespace()
	if err != nil {
		return false
	}

	// If delete configMap is added, uninstall the operator and the resources
	deleteConfigMapList := &v1.ConfigMapList{}
	cmOptions := []client.ListOption{
		client.InNamespace(operatorNamespace),
		client.MatchingLabels{deleteConfigMapLabel: "true"},
	}

	if err := c.List(context.TODO(), deleteConfigMapList, cmOptions...); err != nil {
		return false
	}
	return len(deleteConfigMapList.Items) != 0
}

func removeCsv(c client.Client, r *rest.Config) error {
	operatorNamespace, err := getOperatorNamespace()
	if err != nil {
		return err
	}

	operatorCsv, err := getClusterServiceVersion(r, operatorNamespace)
	if err != nil {
		return err
	}

	if operatorCsv != nil {
		kfdefLog.Info("Deleting the csv", operatorCsv.Name)
		err = c.Delete(context.TODO(), operatorCsv, []client.DeleteOption{}...)
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("error deleting clusterserviceversion: %v", err)
		}
		kfdefLog.Info("Clusterserviceversion deleted as a part of uninstall.", "csvName", operatorCsv.Name)
	}
	kfdefLog.Info("No clusterserviceversion for the operator found.")
	return nil
}

// getOperatorNamespace returns the Namespace the operator is installed in
func getOperatorNamespace() (string, error) {
	var watchNamespaceEnvVar = "OPERATOR_NAMESPACE"

	ns, found := os.LookupEnv(watchNamespaceEnvVar)
	if !found {
		return "", fmt.Errorf("%s must be set", watchNamespaceEnvVar)
	}
	return ns, nil
}
