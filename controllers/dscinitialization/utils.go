package dscinitialization

import (
	"context"
	"crypto/rand"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	authv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1alpha1"
)

var (
	resourceInterval = 10 * time.Second
	resourceTimeout  = 1 * time.Minute
)

// createOdhNamespace creates a Namespace with given name and with ODH defaults. The defaults include:
// - Odh specific labels
// - Pod security labels for baseline permissions
// - Network Policies that allow traffic between the ODH namespaces
func (r *DSCInitializationReconciler) createOdhNamespace(dscInit *dsci.DSCInitialization, name string, ctx context.Context) error {
	// Expected namespace for the given name
	desiredNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"opendatahub.io/generated-namespace": "true",
				"pod-security.kubernetes.io/enforce": "baseline",
			},
		},
	}

	// Create Namespace if doesnot exists
	foundNamespace := &corev1.Namespace{}
	err := r.Get(ctx, client.ObjectKey{Name: name}, foundNamespace)
	if err != nil {
		if apierrs.IsNotFound(err) {
			r.Log.Info("Creating namespace", "name", name)
			// Set Controller reference
			//err = ctrl.SetControllerReference(dscInit, desiredNamespace, r.Scheme)
			//if err != nil {
			//	r.Log.Error(err, "Unable to add OwnerReference to the Namespace")
			//	return err
			//}
			err = r.Create(ctx, desiredNamespace)
			if err != nil && !apierrs.IsAlreadyExists(err) {
				r.Log.Error(err, "Unable to create namespace", "name", name)
				return err
			}
		} else {
			r.Log.Error(err, "Unable to fetch namespace", "name", name)
			return err
		}
	} else if dscInit.Spec.Monitoring.Enabled && dscInit.Spec.Monitoring.Namespace == name {
		err = r.Patch(ctx, foundNamespace, client.RawPatch(types.MergePatchType,
			[]byte(`{"metadata": {"labels": {"openshift.io/cluster-monitoring": "true"}}}`)))
		if err != nil {
			return err
		}
	}
	// Create Monitoring Namespace if it is enabled and not exists
	if dscInit.Spec.Monitoring.Enabled {
		foundMonitoringNamespace := &corev1.Namespace{}
		monitoringName := dscInit.Spec.Monitoring.Namespace
		err := r.Get(ctx, client.ObjectKey{Name: monitoringName}, foundMonitoringNamespace)
		if err != nil {
			if apierrs.IsNotFound(err) {
				r.Log.Info("Not found monitoring namespace", "name", monitoringName)
				desiredMonitoringNamespace := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: monitoringName,
						Labels: map[string]string{
							"opendatahub.io/generated-namespace": "true",
							"pod-security.kubernetes.io/enforce": "baseline",
							"openshift.io/cluster-monitoring":    "true",
						},
					},
				}
				err = r.Create(ctx, desiredMonitoringNamespace)
				if err != nil && !apierrs.IsAlreadyExists(err) {
					r.Log.Error(err, "Unable to create namespace", "name", monitoringName)
					return err
				}
			} else {
				r.Log.Error(err, "Unable to fetch monitoring namespace", "name", monitoringName)
				return err
			}
		}
	}

	// Create default NetworkPolicy for the namespace
	err = r.reconcileDefaultNetworkPolicy(dscInit, name, ctx)
	if err != nil {
		r.Log.Error(err, "error reconciling network policy ", "name", name)
		return err
	}

	// Create odh-common-config Configmap for the Namespace
	err = r.createOdhCommonConfigMap(dscInit, name, ctx)
	if err != nil {
		r.Log.Error(err, "error creating configmap", "name", "odh-common-config")
		return err
	}

	// Create default Rolebinding for the namespace
	err = r.createDefaultRoleBinding(dscInit, name, ctx)
	if err != nil {
		r.Log.Error(err, "error creating rolebinding", "name", name)
		return err
	}
	return nil
}

func (r *DSCInitializationReconciler) createDefaultRoleBinding(dscInit *dsci.DSCInitialization, name string, ctx context.Context) error {
	// Expected namespace for the given name
	desiredRoleBinding := &authv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: name,
		},
		Subjects: []authv1.Subject{
			{
				Kind:      "ServiceAccount",
				Namespace: name,
				Name:      "default",
			},
		},
		RoleRef: authv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "system:openshift:scc:anyuid",
		},
	}

	// Create RoleBinding if doesnot exists
	foundRoleBinding := &authv1.RoleBinding{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: name}, foundRoleBinding)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// Set Controller reference
			err = ctrl.SetControllerReference(dscInit, desiredRoleBinding, r.Scheme)
			if err != nil {
				r.Log.Error(err, "Unable to add OwnerReference to the rolebinding")
				return err
			}
			err = r.Client.Create(ctx, desiredRoleBinding)
			if err != nil && !apierrs.IsAlreadyExists(err) {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

func (r *DSCInitializationReconciler) reconcileDefaultNetworkPolicy(dscInit *dsci.DSCInitialization, name string, ctx context.Context) error {
	// Expected namespace for the given name
	desiredNetworkPolicy := &netv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: name,
		},
		Spec: netv1.NetworkPolicySpec{
			// open ingress for all port for now, TODO: add explicit port per component
			Ingress: []netv1.NetworkPolicyIngressRule{{}},
			PolicyTypes: []netv1.PolicyType{
				netv1.PolicyTypeIngress,
			},
		},
	}

	// Create NetworkPolicy if doesnot exists
	foundNetworkPolicy := &netv1.NetworkPolicy{}
	justCreated := false
	err := r.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: name}, foundNetworkPolicy)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// Set Controller reference
			err = ctrl.SetControllerReference(dscInit, desiredNetworkPolicy, r.Scheme)
			if err != nil {
				r.Log.Error(err, "Unable to add OwnerReference to the Network policy")
				return err
			}
			err = r.Client.Create(ctx, desiredNetworkPolicy)
			if err != nil && !apierrs.IsAlreadyExists(err) {
				return err
			}
			justCreated = true
		} else {
			return err
		}
	}

	// Reconcile the NetworkPolicy spec if it has been manually modified
	if !justCreated && !CompareNotebookNetworkPolicies(*desiredNetworkPolicy, *foundNetworkPolicy) {
		r.Log.Info("Reconciling Network policy", "name", foundNetworkPolicy.Name)
		// Retry the update operation when the ingress controller eventually
		// updates the resource version field
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			// Get the last route revision
			if err := r.Get(ctx, types.NamespacedName{
				Name:      desiredNetworkPolicy.Name,
				Namespace: desiredNetworkPolicy.Namespace,
			}, foundNetworkPolicy); err != nil {
				return err
			}
			// Reconcile labels and spec field
			foundNetworkPolicy.Spec = desiredNetworkPolicy.Spec
			foundNetworkPolicy.ObjectMeta.Labels = desiredNetworkPolicy.ObjectMeta.Labels
			return r.Update(ctx, foundNetworkPolicy)
		})
		if err != nil {
			r.Log.Error(err, "Unable to reconcile the Network Policy")
			return err
		}
	}

	return nil
}

// CompareNotebookNetworkPolicies checks if two services are equal, if not return false
func CompareNotebookNetworkPolicies(np1 netv1.NetworkPolicy, np2 netv1.NetworkPolicy) bool {
	// Two network policies will be equal if the labels and specs are identical
	return reflect.DeepEqual(np1.ObjectMeta.Labels, np2.ObjectMeta.Labels) &&
		reflect.DeepEqual(np1.Spec, np2.Spec)
}

func (r *DSCInitializationReconciler) waitForManagedSecret(name, namespace string) (*corev1.Secret, error) {
	managedSecret := &corev1.Secret{}
	err := wait.Poll(resourceInterval, resourceTimeout, func() (done bool, err error) {

		err = r.Client.Get(context.TODO(), client.ObjectKey{
			Namespace: namespace,
			Name:      name,
		}, managedSecret)

		if err != nil {
			if apierrs.IsNotFound(err) {
				return false, nil
			}
			return false, err
		} else {
			return true, nil
		}
	})

	return managedSecret, err
}

func GenerateRandomHex(length int) ([]byte, error) {
	// Calculate the required number of bytes
	numBytes := length / 2

	// Create a byte slice with the appropriate size
	randomBytes := make([]byte, numBytes)

	// Read random bytes from the crypto/rand source
	_, err := rand.Read(randomBytes)
	if err != nil {
		return nil, err
	}

	return randomBytes, nil
}

func (r *DSCInitializationReconciler) createOdhCommonConfigMap(dscInit *dsci.DSCInitialization, name string, ctx context.Context) error {
	// Expected configmap for the given namespace
	desiredConfigMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odh-common-config",
			Namespace: name,
		},
		Data: map[string]string{"namespace": name},
	}

	// Create Configmap if doesnot exists
	foundConfigMap := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: name}, foundConfigMap)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// Set Controller reference
			err = ctrl.SetControllerReference(dscInit, foundConfigMap, r.Scheme)
			if err != nil {
				r.Log.Error(err, "Unable to add OwnerReference to the odh-common-config ConfigMap")
				return err
			}
			err = r.Client.Create(ctx, desiredConfigMap)
			if err != nil && !apierrs.IsAlreadyExists(err) {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}
