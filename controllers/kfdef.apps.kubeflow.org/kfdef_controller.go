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
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfapp/coordinator"
	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	"io/ioutil"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"os"
	"path"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kfdefappskubefloworgv1 "github.com/kubeflow/kfctl/v3/apis/kfdef.apps.kubeflow.org/v1"
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
var kfdefInstances = map[string]struct{}{}

// whether the 2nd controller is added
var b2ndController = false

// the manager
var kfdefManager manager.Manager

// the stop channel for the 2nd controller
var stop chan struct{}

// KfDefReconciler reconciles a KfDef object
type KfDefReconciler struct {
	client client.Client
	scheme *runtime.Scheme
	restConfig *rest.Config
	// recorder to generate events
	recorder record.EventRecorder
}


//+kubebuilder:rbac:groups=kfdef.apps.kubeflow.org.my.domain,resources=kfdefs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kfdef.apps.kubeflow.org.my.domain,resources=kfdefs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kfdef.apps.kubeflow.org.my.domain,resources=kfdefs/finalizers,verbs=update

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
	_ = log.FromContext(ctx)

	log.Infof("Reconciling KfDef resources. Request.Namespace: %v, Request.Name: %v.", request.Namespace, request.Name)

	instance := &kfdefv1.KfDef{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			if hasDeleteConfigMap(r.client) {
				r.recorder.Eventf(instance, v1.EventTypeWarning, "UninstallInProgress",
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
			log.Infof("Kfdef instance %s deleted.", instance.Name)
			if hasDeleteConfigMap(r.client) {
				// if delete configmap exists, requeue the request to handle operator uninstall
				return ctrl.Result{Requeue: true}, err
			}
			return ctrl.Result{}, nil
		}
		log.Infof("Deleting kfdef instance %s.", instance.Name)

		// stop the 2nd controller
		if len(kfdefInstances) == 1 {
			close(stop)
			b2ndController = false
		}

		// Uninstall Kubeflow
		err = kfDelete(instance)
		if err == nil {
			log.Infof("KubeFlow Deployment Deleted.")
			r.recorder.Eventf(instance, v1.EventTypeNormal, "KfDefDeletionSuccessful",
				"KF instance %s deleted successfully", instance.Name)
		} else {
			// log an error and continue for cleanup. It does not make sense to retry the delete.
			r.recorder.Eventf(instance, v1.EventTypeWarning, "KfDefDeletionFailed",
				"Error deleting KF instance %s", instance.Name)
			log.Errorf("Failed to delete Kubeflow.")

		}

		// Delete the kfapp directory
		kfAppDir := path.Join("/tmp", instance.GetNamespace(), instance.GetName())
		if err := os.RemoveAll(kfAppDir); err != nil {
			log.Errorf("Failed to delete the app directory. Error: %v.", err)
			return ctrl.Result{}, err
		}
		log.Infof("kfAppDir deleted.")

		// Remove this KfDef instance
		delete(kfdefInstances, strings.Join([]string{instance.GetName(), instance.GetNamespace()}, "."))

		// Remove finalizer once kfDelete is completed.
		finalizers.Delete(finalizer)
		instance.SetFinalizers(finalizers.List())
		finalizerError := r.client.Update(context.TODO(), instance)
		for retryCount := 0; errors.IsConflict(finalizerError) && retryCount < finalizerMaxRetries; retryCount++ {
			// Based on Istio operator at https://github.com/istio/istio/blob/master/operator/pkg/controller/istiocontrolplane/istiocontrolplane_controller.go
			// for finalizer removal errors workaround.
			log.Info("Conflict during finalizer removal, retrying.")
			_ = r.client.Get(context.TODO(), request.NamespacedName, instance)
			finalizers = sets.NewString(instance.GetFinalizers()...)
			finalizers.Delete(finalizer)
			instance.SetFinalizers(finalizers.List())
			finalizerError = r.client.Update(context.TODO(), instance)
		}
		if finalizerError != nil {
			log.Errorf("Error removing finalizer: %v.", finalizerError)
			return ctrl.Result{}, finalizerError
		}
		if hasDeleteConfigMap(r.client) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, nil
	} else if !finalizers.Has(finalizer) {
		log.Infof("Normally this should not happen. Adding finalizer %v: %v.", finalizer, request)
		finalizers.Insert(finalizer)
		instance.SetFinalizers(finalizers.List())
		err = r.client.Update(context.TODO(), instance)
		if err != nil {
			log.Errorf("Failed to update kfdef with finalizer. Error: %v.", err)
			return ctrl.Result{}, err
		}
	}

	// If this is a kfdef change, for now, remove the kfapp config path
	if request.Name == instance.GetName() && request.Namespace == instance.GetNamespace() {
		kfAppDir := path.Join("/tmp", instance.GetNamespace(), instance.GetName())
		if err = os.RemoveAll(kfAppDir); err != nil {
			log.Errorf("Failed to delete the app directory. Error: %v.", err)
			return ctrl.Result{}, err
		}
	}

	if hasDeleteConfigMap(r.client) {
		for key, _ := range kfdefInstances{
			keyVal := strings.Split(key,".")
			if len(keyVal) == 2 {
				instanceName, namespace := keyVal[0], keyVal[1]
				currentInstance := &kfdefv1.KfDef{
					ObjectMeta: metav1.ObjectMeta{
						Name:      instanceName,
						Namespace: namespace,
					},
				}

				if err := r.client.Delete(context.TODO(), currentInstance, []client.DeleteOption{}...); err != nil {
					if !errors.IsNotFound(err) {
						return ctrl.Result{}, err
					}
				}
			}else{
				return ctrl.Result{}, fmt.Errorf("error getting kfdef instance name and namespace")
			}

		}

		return ctrl.Result{Requeue: true}, nil
	}

	err = getReconcileStatus(instance, kfApply(instance))
	if err == nil {
		log.Infof("KubeFlow Deployment Completed.")
		r.recorder.Eventf(instance, v1.EventTypeNormal, "KfDefCreationSuccessful",
			"KfDef instance %s created and deployed successfully", instance.Name)

		// add to kfdefInstances if not exists
		if _, ok := kfdefInstances[strings.Join([]string{instance.GetName(), instance.GetNamespace()}, ".")]; !ok {
			kfdefInstances[strings.Join([]string{instance.GetName(), instance.GetNamespace()}, ".")] = struct{}{}
		}

		if !b2ndController {
			c, err := controller.New("kubeflow-controller", kfdefManager, controller.Options{Reconciler: r})
			if err != nil {
				return ctrl.Result{}, nil
			}
			// Watch for changes to kfdef resource and requeue the owner KfDef
			err = watchKubeflowResources(c, kfdefManager.GetClient(), WatchedKubeflowResources)
			if err != nil {
				return ctrl.Result{}, nil
			}
			stop = make(chan struct{})
			go func() {
				// Start the controller
				if err := c.Start(stop); err != nil {
					log.Error(err, "cannot run the 2nd Kubeflow controller")
				}
			}()
			log.Infof("Controller added to watch resources from CRDs created by Kubeflow deployment.")
			b2ndController = true
		}
	}


	// set status of the KfDef resource
	if err := r.reconcileStatus(instance); err != nil {
		return ctrl.Result{}, err
	}

	// If deployment created successfully - don't requeue

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KfDefReconciler) SetupWithManager(mgr ctrl.Manager) error {
	log.Infof("Adding controller for kfdef.")

	kfDefBuilder := ctrl.NewControllerManagedBy(mgr).Named("kfdef-controller")
	err := kfDefBuilder.
		Watches(&source.Kind{Type: &kfdefv1.KfDef{}}, handler.EnqueueRequestsFromMapFunc(
			func(a client.Object) []reconcile.Request {
				namespacedName := types.NamespacedName{Name: a.GetName(), Namespace: a.GetNamespace()}
				finalizers := sets.NewString(a.GetFinalizers()...)
				if !finalizers.Has(finalizer) {
					// assume this is a CREATE event
					log.Infof("Adding finalizer %v: %v.", finalizer, namespacedName)
					finalizers.Insert(finalizer)
					instance := &kfdefv1.KfDef{}
					err := mgr.GetClient().Get(context.TODO(), namespacedName, instance)
					if err != nil {
						log.Errorf("Failed to get kfdef CR. Error: %v.", err)
						return nil
					}
					instance.SetFinalizers(finalizers.List())
					err = mgr.GetClient().Update(context.TODO(), instance)
					if err != nil {
						log.Errorf("Failed to update kfdef with finalizer. Error: %v.", err)
					}
					// let the UPDATE event request queue
					return nil
				}
				log.Infof("Watch a change for KfDef CR: %v.%v.", a.GetName(), a.GetNamespace())
				return []reconcile.Request{{NamespacedName: namespacedName}}
			}), builder.WithPredicates(kfdefPredicates)).
		Complete(r)

	if err != nil {
		return err
	}

	// Watch for changes to kfdef resource and requeue the owner KfDef
	err = r.watchKubeflowResources(kfDefBuilder, WatchedResources)
	if err != nil {
		return err
	}
	log.Infof("Controller added to watch on Kubeflow resources with known GVK.")
	return nil
}

// watch is monitoring changes for kfctl resources managed by the operator
func (r *KfDefReconciler)  watchKubeflowResources(b *builder.Builder, watchedResources []schema.GroupVersionKind) error {
	for _, t := range watchedResources {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{
			Kind:    t.Kind,
			Group:   t.Group,
			Version: t.Version,
		})
		err := b.
			Watches(&source.Kind{Type: u}, handler.EnqueueRequestsFromMapFunc(func(a client.Object) []reconcile.Request {
			anns := a.GetAnnotations()
			kfdefAnn := strings.Join([]string{kfutils.KfDefAnnotation, kfutils.KfDefInstance}, "/")
			_, found := anns[kfdefAnn]
			if found {
				kfdefCr := strings.Split(anns[kfdefAnn], ".")
				namespacedName := types.NamespacedName{Name: kfdefCr[0], Namespace: kfdefCr[1]}
				instance := &kfdefv1.KfDef{}
				err := r.client.Get(context.TODO(), types.NamespacedName{Name: kfdefCr[0], Namespace: kfdefCr[1]}, instance)
				if err != nil {
					if errors.IsNotFound(err) {
						// KfDef CR may have been deleted
						return nil
					}
				} else if instance.GetDeletionTimestamp() != nil {
					// KfDef is being deleted
					return nil
				}
				log.Infof("Watch a change for Kubeflow resource: %v.%v.", a.GetName(), a.GetNamespace())
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
		}), builder.WithPredicates(ownedResourcePredicates)).Complete(r)
		if err != nil {
			log.Errorf("Cannot create watch for resources %v %v/%v: %v.", t.Kind, t.Group, t.Version, err)
		}
	}
	return nil
}

var kfdefPredicates = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		object, _ := meta.Accessor(e.Object)
		log.Infof("Got create event for %v.%v.", object.GetName(), object.GetNamespace())
		return true
	},
	GenericFunc: func(e event.GenericEvent) bool {
		object, _ := meta.Accessor(e.Object)
		log.Infof("Got generic event for %v.%v.", object.GetName(), object.GetNamespace())
		return true
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		object, _ := meta.Accessor(e.Object)
		log.Infof("Got delete event for %v.%v.", object.GetName(), object.GetNamespace())
		return false
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		object, _ := meta.Accessor(e.ObjectOld)
		log.Infof("Got update event for %v.%v.", object.GetName(), object.GetNamespace())
		return true
	},
}

var ownedResourcePredicates = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		// handle create event
		object, err := meta.Accessor(e.Object)
		if err != nil {
			return false
		}
		// handle create event if object has kind configMap
		if e.Object.GetObjectKind().GroupVersionKind().Kind == "ConfigMap" {
			log.Infof("Got create event for %v.%v.", object.GetName(), object.GetNamespace())
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
		log.Infof("Got delete event for %v.%v.", object.GetName(), object.GetNamespace())
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
func kfApply(instance *kfdefv1.KfDef) error {
	log.Infof("Creating a new KubeFlow Deployment. KubeFlow.Namespace: %v.", instance.Namespace)
	kfApp, err := kfLoadConfig(instance, "apply")
	if err != nil {
		log.Errorf("Failed to load KfApp. Error: %v.", err)
		return err
	}
	// Apply kfApp.
	err = kfApp.Apply(kftypesv3.K8S)
	return err
}

// kfDelete is equivalent of kfctl delete
func kfDelete(instance *kfdefv1.KfDef) error {
	log.Infof("Uninstall Kubeflow. KubeFlow.Namespace: %v.", instance.Namespace)
	kfApp, err := kfLoadConfig(instance, "delete")
	if err != nil {
		log.Errorf("Failed to load KfApp. Error: %v.", err)
		return err
	}
	// Delete kfApp.
	err = kfApp.Delete(kftypesv3.K8S)
	return err
}

func kfLoadConfig(instance *kfdefv1.KfDef, action string) (kftypesv3.KfApp, error) {
	// Define kfApp
	kfdefBytes, _ := yaml.Marshal(instance)

	// Make the kfApp directory
	kfAppDir := path.Join("/tmp", instance.GetNamespace(), instance.GetName())
	if err := os.MkdirAll(kfAppDir, 0755); err != nil {
		log.Errorf("Failed to create the app directory. Error: %v.", err)
		return nil, err
	}

	configFilePath := path.Join(kfAppDir, "config.yaml")
	err := ioutil.WriteFile(configFilePath, kfdefBytes, 0644)
	if err != nil {
		log.Errorf("Failed to write config.yaml. Error: %v.", err)
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
		log.Errorf("failed to build kfApp from URI %v: Error: %v.", configFilePath, err)

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
func getClusterServiceVersion(cfg *rest.Config, watchNameSpace string) (*olm.ClusterServiceVersion, error) {

	operatorClient, err := olmclientset.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error getting operator client %v", err)
	}
	csvs, err := operatorClient.ClusterServiceVersions(watchNameSpace).List(metav1.ListOptions{})
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
	namespace := &v1.Namespace{ObjectMeta:metav1.ObjectMeta{
		Name:                       request.Namespace,
	}}

	if err := r.client.Delete(context.TODO(), namespace); err!=nil{
		if !errors.IsNotFound(err) {
			return fmt.Errorf("error deleting current namespace :%v", err)
		}
	}
	r.recorder.Eventf(namespace, v1.EventTypeNormal, "NamespaceDeletionSuccessful",
		"Namespace %s deleted as a part of uninstall.", namespace.Name )
	log.Infof("Namespace %s deleted as a part of uninstall.", namespace.Name)

	// Delete any unavailable api services
	apiservices := &apiserv1.APIServiceList{}
	if err := r.client.List(context.TODO(), apiservices); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("error getting dangling apiservices : %v", err)
		}
	}

	if len(apiservices.Items) != 0 {
		for _, apiservice := range apiservices.Items {
			conditionsLength := len(apiservice.Status.Conditions)
			if conditionsLength >= 1{
				if apiservice.Status.Conditions[conditionsLength - 1].Status == apiserv1.ConditionFalse {
					if err := r.client.Delete(context.TODO(), &apiservice, []client.DeleteOption{}...); err != nil {
						return fmt.Errorf("error deleting apiservice %v: %v", apiservice.Name, err)
					}
				}
			}
			log.Infof("Unavailable api service %v is deleted", apiservice.Name)

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
	if err := r.client.List(context.TODO(), generatedNamespaces, nsOptions...); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("error getting generated namespaces : %v", err)
		}
	}
	if len(generatedNamespaces.Items) != 0 {
		for _, namespace := range generatedNamespaces.Items {
			if namespace.Status.Phase == v1.NamespaceActive {
				if err := r.client.Delete(context.TODO(), &namespace, []client.DeleteOption{}...); err != nil {
					return fmt.Errorf("error deleting namespace %v: %v", namespace.Name, err)
				}
				r.recorder.Eventf(&namespace, v1.EventTypeNormal, "NamespaceDeletionSuccessful",
					"Namespace %s deleted as a part of uninstall.", namespace.Name )
				log.Infof("Namespace %s deleted as a part of uninstall.", namespace.Name)
			}
		}
	}
	log.Info("All resources deleted as part of uninstall. Removing the operator csv")
	return removeCsv(r.client, r.restConfig)
}

// hasDeleteConfigMap returns true if delete configMap is added to the operator namespace by managed-tenants repo.
// It returns false in all other cases.
func hasDeleteConfigMap(c client.Client) bool {
	// Get watchNamespace
	operatorNamespace, err := k8sutil.GetOperatorNamespace()
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

func removeCsv(	c client.Client, r *rest.Config) error{
	// Get watchNamespace
	operatorNamespace, err := k8sutil.GetOperatorNamespace()
	if err != nil {
		return err
	}

	operatorCsv, err := getClusterServiceVersion(r, operatorNamespace)
	if err != nil {
		return err
	}

	if operatorCsv != nil {
		log.Infof("Deleting csv %s", operatorCsv.Name)
		err = c.Delete(context.TODO(), operatorCsv, []client.DeleteOption{}...)
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("error deleting clusterserviceversion: %v", err)
		}
		log.Infof("Clusterserviceversion %s deleted as a part of uninstall.", operatorCsv.Name)
	}
	log.Info("No clusterserviceversion for the operator found.")
	return nil
}
