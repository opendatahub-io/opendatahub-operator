package dscinitialization

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"reflect"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	userv1 "github.com/openshift/api/user/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

var (
	resourceInterval = 10 * time.Second
	resourceTimeout  = 1 * time.Minute
)

// createOperatorResource include steps:
// - 1. validate customized application namespace || create/update default application namespace
//   - patch application namespaces for managed cluster
//   - Odh specific labels for access
//   - Pod security labels for baseline permissions
//
// - 2. Patch monitoring namespace
// - 3. Network Policies 'opendatahub' that allow traffic between the ODH namespaces
// - 4. ConfigMap 'odh-common-config'.
func (r *DSCInitializationReconciler) createOperatorResource(ctx context.Context, dscInit *dsciv1.DSCInitialization, platform common.Platform) error {
	log := logf.FromContext(ctx)

	if err := r.appNamespaceHandler(ctx, dscInit, platform); err != nil {
		log.Error(err, "error handle application namespace")
		return err
	}

	// Patch monitoring namespace: only for Managed cluster
	if platform == cluster.ManagedRhoai {
		if err := r.patchMonitoringNS(ctx, dscInit); err != nil {
			log.Error(err, "error patch monitoring namespace")
			return err
		}
	}

	// Create default NetworkPolicy for the namespace
	if err := r.reconcileDefaultNetworkPolicy(ctx, dscInit, platform); err != nil {
		log.Error(err, "error reconciling network policy ", "name", dscInit.Spec.ApplicationsNamespace)
		return fmt.Errorf("error: %w", err)
	}

	// Create odh-common-config Configmap for the Namespace
	if err := r.createOdhCommonConfigMap(ctx, dscInit); err != nil {
		log.Error(err, "error creating configmap", "name", "odh-common-config")
		return err
	}

	return nil
}

func (r *DSCInitializationReconciler) appNamespaceHandler(ctx context.Context, dscInit *dsciv1.DSCInitialization, platform common.Platform) error {
	log := logf.FromContext(ctx)

	nsList := &corev1.NamespaceList{}
	ns := &corev1.Namespace{}
	dsciNsName := dscInit.Spec.ApplicationsNamespace

	if err := r.List(ctx, nsList, client.MatchingLabels{
		labels.CustomizedAppNamespace: labels.True,
	}); err != nil {
		return err
	}

	switch len(nsList.Items) {
	case 0:
		// create namespace if not exist
		desiredAppNS := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: dsciNsName,
			},
		}
		if err := r.Get(ctx, client.ObjectKeyFromObject(desiredAppNS), ns); err != nil {
			if !k8serr.IsNotFound(err) {
				return err
			}
		}
		log.Info("Application namespace set in DSCI not found, creating it with labels", "name", dsciNsName)
		// // ensure generatedd-namespace:true and security label always on it
		return r.createAppNamespace(ctx, dsciNsName, platform, map[string]string{labels.ODH.OwnedNamespace: labels.True}) // this indicate when uninstall, namespace will be deleted
	case 1:
		if nsList.Items[0].Name != dsciNsName {
			return errors.New("DSCI must used the same namespace which has opendatahub.io/application-namespace=true label")
		}
		// ensure security label always on it
		return r.createAppNamespace(ctx, dsciNsName, platform)
	default:
		return errors.New("only support max. one namespace with label: opendatahub.io/application-namespace=true")
	}
}

func (r *DSCInitializationReconciler) createAppNamespace(ctx context.Context, nsName string, platform common.Platform, extraLabel ...map[string]string) error {
	desiredDefaultNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: nsName},
	}
	labelList := map[string]string{
		labels.SecurityEnforce: "baseline",
	}

	// label only for managed cluster
	if platform == cluster.ManagedRhoai {
		labelList["openshift.io/cluster-monitoring"] = "true"
	}

	for _, l := range extraLabel {
		for k, v := range l {
			labelList[k] = v
		}
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, desiredDefaultNS, func() error {
		resources.SetLabels(desiredDefaultNS, labelList)
		return nil
	})
	return err
}

func (r *DSCInitializationReconciler) patchMonitoringNS(ctx context.Context, dscInit *dsciv1.DSCInitialization) error {
	log := logf.FromContext(ctx)
	if dscInit.Spec.Monitoring.ManagementState != operatorv1.Managed {
		return nil
	}
	// Create Monitoring Namespace if it is enabled and not exists
	monitoringName := dscInit.Spec.Monitoring.Namespace

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

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, desiredMonitoringNamespace, func() error {
		return nil
	})
	if err != nil {
		log.Error(err, "Unable to create or patcth monitoirng namespace")
	}
	return err
}

func (r *DSCInitializationReconciler) reconcileDefaultNetworkPolicy(
	ctx context.Context,
	dscInit *dsciv1.DSCInitialization,
	platform common.Platform,
) error {
	log := logf.FromContext(ctx)
	name := dscInit.Spec.ApplicationsNamespace
	if platform == cluster.ManagedRhoai {
		// Get operator namepsace
		operatorNs, err := cluster.GetOperatorNamespace()
		if err != nil {
			log.Error(err, "error getting operator namespace for networkplicy creation")
			return err
		}
		// Deploy networkpolicy for operator namespace
		err = deploy.DeployManifestsFromPath(ctx, r.Client, dscInit, networkpolicyPath+"/operator", operatorNs, "networkpolicy", true)
		if err != nil {
			log.Error(err, "error to set networkpolicy in operator namespace", "path", networkpolicyPath)
			return err
		}
		// Deploy networkpolicy for monitoring namespace
		if dscInit.Spec.Monitoring.ManagementState == operatorv1.Managed {
			err = deploy.DeployManifestsFromPath(ctx, r.Client, dscInit, networkpolicyPath+"/monitoring", dscInit.Spec.Monitoring.Namespace, "networkpolicy", true)
			if err != nil {
				log.Error(err, "error to set networkpolicy in monitroing namespace", "path", networkpolicyPath)
				return err
			}
		}
		// Deploy networkpolicy for applications namespace
		err = deploy.DeployManifestsFromPath(ctx, r.Client, dscInit, networkpolicyPath+"/applications", dscInit.Spec.ApplicationsNamespace, "networkpolicy", true)
		if err != nil {
			log.Error(err, "error to set networkpolicy in applications namespace", "path", networkpolicyPath)
			return err
		}
		return nil
	}
	// Expected namespace for the given name in ODH
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
									labels.ODH.OwnedNamespace: labels.True,
								},
							},
						},
					},
				},
				{ // OR logic to minic customized application namespace
					From: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{ // AND logic
								MatchLabels: map[string]string{
									labels.CustomizedAppNamespace: labels.True,
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
	err := r.Client.Get(ctx, client.ObjectKeyFromObject(desiredNetworkPolicy), foundNetworkPolicy)
	if err != nil {
		if !k8serr.IsNotFound(err) {
			return err
		}
		// Set Controller reference
		err = ctrl.SetControllerReference(dscInit, desiredNetworkPolicy, r.Scheme)
		if err != nil {
			log.Error(err, "Unable to add OwnerReference to the Network policy")
			return err
		}
		err = r.Client.Create(ctx, desiredNetworkPolicy)
		if err != nil && !k8serr.IsAlreadyExists(err) {
			return err
		}
		justCreated = true
	}

	// Reconcile the NetworkPolicy spec if it has been manually modified
	if !justCreated && !CompareNotebookNetworkPolicies(*desiredNetworkPolicy, *foundNetworkPolicy) {
		log.Info("Reconciling Network policy", "name", foundNetworkPolicy.Name)
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
			log.Error(err, "Unable to reconcile the Network Policy")
			return err
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

func (r *DSCInitializationReconciler) createOdhCommonConfigMap(ctx context.Context, dscInit *dsciv1.DSCInitialization) error {
	log := logf.FromContext(ctx)
	name := dscInit.Spec.ApplicationsNamespace
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
	err := r.Client.Get(ctx, client.ObjectKeyFromObject(desiredConfigMap), foundConfigMap)
	if err != nil {
		if k8serr.IsNotFound(err) {
			// Set Controller reference
			err = ctrl.SetControllerReference(dscInit, foundConfigMap, r.Scheme)
			if err != nil {
				log.Error(err, "Unable to add OwnerReference to the odh-common-config ConfigMap")
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

	err := r.Client.Create(ctx, userGroup)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		return err
	}

	return nil
}
