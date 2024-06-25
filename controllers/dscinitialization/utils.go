package dscinitialization

import (
	"context"
	"crypto/rand"
	"reflect"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	userv1 "github.com/openshift/api/user/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

var (
	resourceInterval = 10 * time.Second
	resourceTimeout  = 1 * time.Minute
)

// createOdhNamespace creates a Namespace with given name and with ODH defaults. The defaults include:
// - Odh specific labels
// - Pod security labels for baseline permissions
// - ConfigMap  'odh-common-config'
// - Network Policies 'opendatahub' that allow traffic between the ODH namespaces
// - RoleBinding 'opendatahub'.
func (r *DSCInitializationReconciler) createOdhNamespace(ctx context.Context, dscInit *dsciv1.DSCInitialization, name string) error {
	// Expected application namespace for the given name
	desiredNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				labels.ODH.OwnedNamespace: "true",
				labels.SecurityEnforce:    "baseline",
			},
		},
	}

	// Create Application Namespace if it doesn't exist
	foundNamespace := &corev1.Namespace{}
	err := r.Get(ctx, client.ObjectKey{Name: name}, foundNamespace)
	if err != nil {
		if k8serr.IsNotFound(err) {
			r.Log.Info("Creating namespace", "name", name)
			// Set Controller reference
			// err = ctrl.SetControllerReference(dscInit, desiredNamespace, r.Scheme)
			// if err != nil {
			//	 r.Log.Error(err, "Unable to add OwnerReference to the Namespace")
			//	 return err
			// }
			err = r.Create(ctx, desiredNamespace)
			if err != nil && !k8serr.IsAlreadyExists(err) {
				r.Log.Error(err, "Unable to create namespace", "name", name)
				return err
			}
		} else {
			r.Log.Error(err, "Unable to fetch namespace", "name", name)
			return err
		}
		// Patch Application Namespace if it exists
	} else if dscInit.Spec.Monitoring.ManagementState == operatorv1.Managed {
		r.Log.Info("Patching application namespace for Managed cluster", "name", name)
		labelPatch := `{"metadata":{"labels":{"openshift.io/cluster-monitoring":"true","pod-security.kubernetes.io/enforce":"baseline","opendatahub.io/generated-namespace": "true"}}}`
		err = r.Patch(ctx, foundNamespace, client.RawPatch(types.MergePatchType,
			[]byte(labelPatch)))
		if err != nil {
			return err
		}
	}
	// Create Monitoring Namespace if it is enabled and not exists
	if dscInit.Spec.Monitoring.ManagementState == operatorv1.Managed {
		foundMonitoringNamespace := &corev1.Namespace{}
		monitoringName := dscInit.Spec.Monitoring.Namespace
		err := r.Get(ctx, client.ObjectKey{Name: monitoringName}, foundMonitoringNamespace)
		if err != nil {
			if k8serr.IsNotFound(err) {
				r.Log.Info("Not found monitoring namespace", "name", monitoringName)
				desiredMonitoringNamespace := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: monitoringName,
						Labels: map[string]string{
							labels.ODH.OwnedNamespace: "true",
							labels.SecurityEnforce:    "baseline",
							labels.ClusterMonitoring:  "true",
						},
					},
				}
				err = r.Create(ctx, desiredMonitoringNamespace)
				if err != nil && !k8serr.IsAlreadyExists(err) {
					r.Log.Error(err, "Unable to create namespace", "name", monitoringName)
					return err
				}
			} else {
				r.Log.Error(err, "Unable to fetch monitoring namespace", "name", monitoringName)
				return err
			}
		} else { // force to patch monitoring namespace with label for cluster-monitoring
			r.Log.Info("Patching monitoring namespace", "name", monitoringName)
			labelPatch := `{"metadata":{"labels":{"openshift.io/cluster-monitoring":"true", "pod-security.kubernetes.io/enforce":"baseline","opendatahub.io/generated-namespace": "true"}}}`

			err = r.Patch(ctx, foundMonitoringNamespace, client.RawPatch(types.MergePatchType, []byte(labelPatch)))
			if err != nil {
				return err
			}
		}
	}

	// Create default NetworkPolicy for the namespace
	err = r.reconcileDefaultNetworkPolicy(ctx, name, dscInit)
	if err != nil {
		r.Log.Error(err, "error reconciling network policy ", "name", name)
		return err
	}

	// Create odh-common-config Configmap for the Namespace
	err = r.createOdhCommonConfigMap(ctx, name, dscInit)
	if err != nil {
		r.Log.Error(err, "error creating configmap", "name", "odh-common-config")
		return err
	}

	// Create default Rolebinding for the namespace
	err = r.createDefaultRoleBinding(ctx, name, dscInit)
	if err != nil {
		r.Log.Error(err, "error creating rolebinding", "name", name)
		return err
	}
	return nil
}

func (r *DSCInitializationReconciler) createDefaultRoleBinding(ctx context.Context, name string, dscInit *dsciv1.DSCInitialization) error {
	// Expected namespace for the given name
	desiredRoleBinding := &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RoleBinding",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Namespace: name,
				Name:      "default",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "system:openshift:scc:anyuid",
		},
	}

	// Create RoleBinding if doesn't exists
	foundRoleBinding := &rbacv1.RoleBinding{}
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: name,
	}, foundRoleBinding)
	if err != nil {
		if k8serr.IsNotFound(err) {
			// Set Controller reference
			err = ctrl.SetControllerReference(dscInit, desiredRoleBinding, r.Scheme)
			if err != nil {
				r.Log.Error(err, "Unable to add OwnerReference to the rolebinding")
				return err
			}
			err = r.Client.Create(ctx, desiredRoleBinding)
			if err != nil && !k8serr.IsAlreadyExists(err) {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

func (r *DSCInitializationReconciler) reconcileDefaultNetworkPolicy(ctx context.Context, name string, dscInit *dsciv1.DSCInitialization) error {
	platform, err := cluster.GetPlatform(r.Client)
	if err != nil {
		return err
	}
	if platform == cluster.ManagedRhods || platform == cluster.SelfManagedRhods {
		// Deploy networkpolicy for operator namespace
		err = deploy.DeployManifestsFromPath(r.Client, dscInit, networkpolicyPath+"/operator", "redhat-ods-operator", "networkpolicy", true)
		if err != nil {
			r.Log.Error(err, "error to set networkpolicy in operator namespace", "path", networkpolicyPath)
			return err
		}
		// Deploy networkpolicy for monitoring namespace
		err = deploy.DeployManifestsFromPath(r.Client, dscInit, networkpolicyPath+"/monitoring", dscInit.Spec.Monitoring.Namespace, "networkpolicy", true)
		if err != nil {
			r.Log.Error(err, "error to set networkpolicy in monitroing namespace", "path", networkpolicyPath)
			return err
		}
		// Deploy networkpolicy for applications namespace
		err = deploy.DeployManifestsFromPath(r.Client, dscInit, networkpolicyPath+"/applications", dscInit.Spec.ApplicationsNamespace, "networkpolicy", true)
		if err != nil {
			r.Log.Error(err, "error to set networkpolicy in applications namespace", "path", networkpolicyPath)
			return err
		}
	} else { // Expected namespace for the given name in ODH
		desiredNetworkPolicy := &networkingv1.NetworkPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetworkPolicy",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: name,
			},
			Spec: networkingv1.NetworkPolicySpec{
				// open ingress for all port for now, TODO: add explicit port per component
				// Ingress: []networkingv1.NetworkPolicyIngressRule{{}},
				// open ingress for only operator created namespaces
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					{
						From: []networkingv1.NetworkPolicyPeer{
							{ /* allow ODH namespace <->ODH namespace:
								- default notebook project: rhods-notebooks
								- redhat-odh-monitoring
								- redhat-odh-applications / opendatahub
								*/
								NamespaceSelector: &metav1.LabelSelector{ // AND logic
									MatchLabels: map[string]string{
										labels.ODH.OwnedNamespace: "true",
									},
								},
							},
						},
					},
					{ // OR logic
						From: []networkingv1.NetworkPolicyPeer{
							{ // need this to access external-> dashboard
								NamespaceSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"network.openshift.io/policy-group": "ingress",
									},
								},
							},
						},
					},
					{ // OR logic for PSI
						From: []networkingv1.NetworkPolicyPeer{
							{ // need this to access external->dashboard
								NamespaceSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"kubernetes.io/metadata.name": "openshift-host-network",
									},
								},
							},
						},
					},
					{
						From: []networkingv1.NetworkPolicyPeer{
							{ // need this for cluster-monitoring work: cluster-monitoring->ODH namespaces
								NamespaceSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"kubernetes.io/metadata.name": "openshift-monitoring",
									},
								},
							},
						},
					},
				},
				PolicyTypes: []networkingv1.PolicyType{
					networkingv1.PolicyTypeIngress,
				},
			},
		}

		// Create NetworkPolicy if it doesn't exist
		foundNetworkPolicy := &networkingv1.NetworkPolicy{}
		justCreated := false
		err = r.Client.Get(ctx, client.ObjectKey{
			Name:      name,
			Namespace: name,
		}, foundNetworkPolicy)
		if err != nil {
			if k8serr.IsNotFound(err) {
				// Set Controller reference
				err = ctrl.SetControllerReference(dscInit, desiredNetworkPolicy, r.Scheme)
				if err != nil {
					r.Log.Error(err, "Unable to add OwnerReference to the Network policy")
					return err
				}
				err = r.Client.Create(ctx, desiredNetworkPolicy)
				if err != nil && !k8serr.IsAlreadyExists(err) {
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
	}
	return nil
}

// CompareNotebookNetworkPolicies checks if two services are equal, if not return false.
func CompareNotebookNetworkPolicies(np1 networkingv1.NetworkPolicy, np2 networkingv1.NetworkPolicy) bool {
	// Two network policies will be equal if the labels and specs are identical
	return reflect.DeepEqual(np1.ObjectMeta.Labels, np2.ObjectMeta.Labels) &&
		reflect.DeepEqual(np1.Spec, np2.Spec)
}

func (r *DSCInitializationReconciler) waitForManagedSecret(ctx context.Context, name string, namespace string) (*corev1.Secret, error) {
	managedSecret := &corev1.Secret{}
	err := wait.PollUntilContextTimeout(ctx, resourceInterval, resourceTimeout, false, func(ctx context.Context) (bool, error) {
		err := r.Client.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      name,
		}, managedSecret)

		if err != nil {
			return false, client.IgnoreNotFound(err)
		}
		return true, nil
	})

	return managedSecret, err
}

func GenerateRandomHex(length int) ([]byte, error) {
	// Calculate the required number of bytes
	numBytes := length / 2

	// Create a byte slice with the appropriate size
	randomBytes := make([]byte, numBytes)

	// Read random bytes from the crypto/rand source
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, err
	}

	return randomBytes, nil
}

func (r *DSCInitializationReconciler) createOdhCommonConfigMap(ctx context.Context, name string, dscInit *dsciv1.DSCInitialization) error {
	// Expected configmap for the given namespace
	desiredConfigMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odh-common-config",
			Namespace: name,
		},
		Data: map[string]string{"namespace": name},
	}

	// Create Configmap if doesn't exists
	foundConfigMap := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: name,
	}, foundConfigMap)
	if err != nil {
		if k8serr.IsNotFound(err) {
			// Set Controller reference
			err = ctrl.SetControllerReference(dscInit, foundConfigMap, r.Scheme)
			if err != nil {
				r.Log.Error(err, "Unable to add OwnerReference to the odh-common-config ConfigMap")
				return err
			}
			err = r.Client.Create(ctx, desiredConfigMap)
			if err != nil && !k8serr.IsAlreadyExists(err) {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

func (r *DSCInitializationReconciler) createUserGroup(ctx context.Context, dscInit *dsciv1.DSCInitialization, userGroupName string) error {
	userGroup := &userv1.Group{
		ObjectMeta: metav1.ObjectMeta{
			Name: userGroupName,
			// Otherwise it errors with  "error": "an empty namespace may not be set during creation"
			Namespace: dscInit.Spec.ApplicationsNamespace,
		},
		// Otherwise is errors with "error": "Group.user.openshift.io \"odh-admins\" is invalid: users: Invalid value: \"null\": users in body must be of type array: \"null\""}
		Users: []string{},
	}
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      userGroup.Name,
		Namespace: dscInit.Spec.ApplicationsNamespace,
	}, userGroup)
	if err != nil {
		if k8serr.IsNotFound(err) {
			err = r.Client.Create(ctx, userGroup)
			if err != nil && !k8serr.IsAlreadyExists(err) {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}
