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
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
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
// - 1. validate customized application namespace || create/update application namespace
// - 2. patch application namespaces for managed cluster
//   - Odh specific labels for access
//   - Pod security labels for baseline permissions//
//
// - 3. Patch monitoring namespace
// - 4. Network Policies 'opendatahub' that allow traffic between the ODH namespaces
// - 5. ConfigMap  'odh-common-config'
// - 6. RoleBinding 'opendatahub'.
func (r *DSCInitializationReconciler) createOperatorResource(ctx context.Context, dscInit *dsciv1.DSCInitialization, platform cluster.Platform) error {
	log := logf.FromContext(ctx)

	if err := r.appNamespaceHandler(ctx, dscInit, platform); err != nil {
		return fmt.Errorf("error handle application namespace: %w", err)
	}

	// Patch monitoring namespace
	err := r.patchMonitoringNS(ctx, dscInit)
	if err != nil {
		log.Error(err, "error patch monitoring namespace for managed cluster")
		return err
	}

	// Create default NetworkPolicy for the namespace
	err = r.reconcileDefaultNetworkPolicy(ctx, dscInit, platform)
	if err != nil {
		log.Error(err, "error reconciling network policy ", "name", dscInit.Spec.ApplicationsNamespace)
		return fmt.Errorf("error: %w", err)
	}

	// Create odh-common-config Configmap for the Namespace
	err = r.createOdhCommonConfigMap(ctx, dscInit)
	if err != nil {
		log.Error(err, "error creating configmap", "name", "odh-common-config")
		return err
	}

	// Create default Rolebinding for the namespace
	err = r.createDefaultRoleBinding(ctx, dscInit)
	if err != nil {
		log.Error(err, "error creating rolebinding", "name", dscInit.Spec.ApplicationsNamespace)
		return err
	}
	return nil
}

func (r *DSCInitializationReconciler) appNamespaceHandler(ctx context.Context, dscInit *dsciv1.DSCInitialization, platform cluster.Platform) error {
	log := logf.FromContext(ctx)
	nsName := dscInit.Spec.ApplicationsNamespace
	// Check if application namespace has label "opendatahub.io/application-namespace:true"
	// if no such namespace exist, we create it with generated-namespace and security label
	// if namespace exist but no label, we exit
	// if namespace exist and has label, we enforce security label onto it.
	appNamespace := &corev1.Namespace{}
	desiredAppNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(desiredAppNS), appNamespace); err != nil {
		if !k8serr.IsNotFound(err) {
			return err
		}
		log.Info("Application namespace from DSCI not found, creating it", "name", nsName)
		return r.createAppNamespace(ctx, nsName, platform, map[string]string{labels.ODH.OwnedNamespace: labels.True}) // this indicate when uninstall, namespace will be deleted
	}

	l := appNamespace.GetLabels()
	if l[labels.CustomizedAppNamespace] == "true" {
		// ensure customized namespace has required label
		return r.createAppNamespace(ctx, nsName, platform, map[string]string{labels.CustomizedAppNamespace: labels.True})
	}
	if l[labels.ODH.OwnedNamespace] == "true" {
		// ensure default namespace has required label
		return r.createAppNamespace(ctx, nsName, platform, map[string]string{labels.ODH.OwnedNamespace: labels.True})
	}
	return errors.New("application namespace missing required label or label value is incorrect. Please recreate DSCI to set the label, or leave it to default value")
}

func (r *DSCInitializationReconciler) createAppNamespace(ctx context.Context, nsName string, platform cluster.Platform, extraLabel ...map[string]string) error {
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
	foundMonitoringNamespace := &corev1.Namespace{}
	// Create Monitoring Namespace if it is enabled and not exists
	monitoringName := dscInit.Spec.Monitoring.Namespace
	err := r.Get(ctx, client.ObjectKey{Name: monitoringName}, foundMonitoringNamespace)
	if err != nil {
		if k8serr.IsNotFound(err) { //  create monitoring namespace
			log.Info("Not found monitoring namespace", "name", monitoringName)
			desiredMonitoringNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: monitoringName,
					Labels: map[string]string{
						labels.ODH.OwnedNamespace: labels.True,
						labels.SecurityEnforce:    "baseline",
						labels.ClusterMonitoring:  labels.True,
					},
				},
			}
			err = r.Create(ctx, desiredMonitoringNamespace)
			if err != nil && !k8serr.IsAlreadyExists(err) {
				log.Error(err, "Unable to create namespace", "name", monitoringName)
				return err
			}
		} else {
			log.Error(err, "Unable to fetch monitoring namespace", "name", monitoringName)
			return err
		}
	}
	// force to patch monitoring namespace with label for cluster-monitoring
	log.Info("Patching monitoring namespace", "name", monitoringName)
	labelPatch := `{"metadata":{"labels":{"openshift.io/cluster-monitoring":"true", "pod-security.kubernetes.io/enforce":"baseline","opendatahub.io/generated-namespace": "true"}}}`

	err = r.Patch(ctx, foundMonitoringNamespace, client.RawPatch(types.MergePatchType, []byte(labelPatch)))
	if err != nil {
		return err
	}
	return nil
}

func (r *DSCInitializationReconciler) createDefaultRoleBinding(ctx context.Context, dscInit *dsciv1.DSCInitialization) error {
	log := logf.FromContext(ctx)
	name := dscInit.Spec.ApplicationsNamespace
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
	err := r.Client.Get(ctx, client.ObjectKeyFromObject(desiredRoleBinding), foundRoleBinding)
	if err != nil {
		if k8serr.IsNotFound(err) {
			// Set Controller reference
			err = ctrl.SetControllerReference(dscInit, desiredRoleBinding, r.Scheme)
			if err != nil {
				log.Error(err, "Unable to add OwnerReference to the rolebinding")
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

func (r *DSCInitializationReconciler) reconcileDefaultNetworkPolicy(
	ctx context.Context,
	dscInit *dsciv1.DSCInitialization,
	platform cluster.Platform,
) error {
	log := logf.FromContext(ctx)
	name := dscInit.Spec.ApplicationsNamespace
	if platform == cluster.ManagedRhoai || platform == cluster.SelfManagedRhoai {
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
		err = deploy.DeployManifestsFromPath(ctx, r.Client, dscInit, networkpolicyPath+"/monitoring", dscInit.Spec.Monitoring.Namespace, "networkpolicy", true)
		if err != nil {
			log.Error(err, "error to set networkpolicy in monitroing namespace", "path", networkpolicyPath)
			return err
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
	err := r.Client.Get(ctx, client.ObjectKeyFromObject(userGroup), userGroup)
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
