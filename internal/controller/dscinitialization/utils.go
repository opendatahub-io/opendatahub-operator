package dscinitialization

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
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
// - 3. Network Policies 'opendatahub' that allow traffic between the ODH namespaces.
func (r *DSCInitializationReconciler) createOperatorResource(ctx context.Context, dscInit *dsciv1.DSCInitialization, platform common.Platform) error {
	log := logf.FromContext(ctx)

	if err := r.appNamespaceHandler(ctx, dscInit, platform); err != nil {
		log.Error(err, "error handle application namespace")
		return err
	}

	// Patch monitoring namespace: only for Managed cluster
	if platform == cluster.ManagedRhoai {
		if err := PatchMonitoringNS(ctx, r.Client, dscInit); err != nil {
			log.Error(err, "error patch monitoring namespace")
			return err
		}
	}

	// Create default NetworkPolicy for the namespace
	if err := ReconcileDefaultNetworkPolicy(ctx, r.Client, dscInit, platform); err != nil {
		return err
	}

	return nil
}

func (r *DSCInitializationReconciler) appNamespaceHandler(ctx context.Context, dscInit *dsciv1.DSCInitialization, platform common.Platform) error {
	log := logf.FromContext(ctx)

	nsList := &corev1.NamespaceList{}
	ns := &corev1.Namespace{}
	dsciNsName := dscInit.Spec.ApplicationsNamespace

	if err := r.Client.List(ctx, nsList, client.MatchingLabels{
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
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(desiredAppNS), ns); err != nil {
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
		labelList[labels.ClusterMonitoring] = labels.True
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

func PatchMonitoringNS(ctx context.Context, cli client.Client, dscInit *dsciv1.DSCInitialization) error {
	log := logf.FromContext(ctx)
	monitoringName := dscInit.Spec.Monitoring.Namespace
	if dscInit.Spec.Monitoring.ManagementState != operatorv1.Managed || dscInit.Spec.ApplicationsNamespace == monitoringName {
		return nil
	}
	// Create Monitoring Namespace if it is enabled and not exists

	desiredMonitoringNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: monitoringName,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, cli, desiredMonitoringNamespace, func() error {
		resources.SetLabels(desiredMonitoringNamespace, map[string]string{
			labels.ODH.OwnedNamespace: labels.True,
			labels.SecurityEnforce:    "baseline",
			labels.ClusterMonitoring:  labels.True,
		})
		return nil
	})
	if err != nil {
		log.Error(err, "Unable to create or patch monitoring namespace")
	}
	return err
}

func ReconcileDefaultNetworkPolicy(
	ctx context.Context,
	cli client.Client,
	dscInit *dsciv1.DSCInitialization,
	platform common.Platform,
) error {
	if platform == cluster.ManagedRhoai || platform == cluster.SelfManagedRhoai {
		log := logf.FromContext(ctx)

		// Get operator namepsace
		operatorNs, err := cluster.GetOperatorNamespace()
		if err != nil {
			log.Error(err, "error getting operator namespace for networkplicy creation")
			return err
		}
		// Deploy networkpolicy for operator namespace
		err = deploy.DeployManifestsFromPath(ctx, cli, dscInit, networkpolicyPath+"/operator", operatorNs, "networkpolicy", true)
		if err != nil {
			log.Error(err, "error to set networkpolicy in operator namespace", "path", networkpolicyPath)
			return err
		}
		// Deploy networkpolicy for monitoring namespace only when it is managed cluster.
		if platform == cluster.ManagedRhoai {
			err = deploy.DeployManifestsFromPath(ctx, cli, dscInit, networkpolicyPath+"/monitoring", dscInit.Spec.Monitoring.Namespace, "networkpolicy", true)
			if err != nil {
				log.Error(err, "error to set networkpolicy in monitoring namespace", "path", networkpolicyPath)
				return err
			}
		}
		// Deploy networkpolicy for applications namespace
		err = deploy.DeployManifestsFromPath(ctx, cli, dscInit, networkpolicyPath+"/applications", dscInit.Spec.ApplicationsNamespace, "networkpolicy", true)
		if err != nil {
			log.Error(err, "error to set networkpolicy in applications namespace", "path", networkpolicyPath)
			return err
		}
		return nil
	}

	// Expected namespace for the given name in ODH
	np := networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dscInit.Spec.ApplicationsNamespace,
			Namespace: dscInit.Spec.ApplicationsNamespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: createNetworkPolicyPeer(labels.ODH.OwnedNamespace, labels.True)}, {
				From: createNetworkPolicyPeer(labels.CustomizedAppNamespace, labels.True)}, {
				From: createNetworkPolicyPeer("network.openshift.io/policy-group", "ingress")}, {
				From: createNetworkPolicyPeer("kubernetes.io/metadata.name", "openshift-host-network")}, {
				From: createNetworkPolicyPeer("kubernetes.io/metadata.name", "openshift-monitoring")},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
		},
	}

	if err := resources.EnsureGroupVersionKind(cli.Scheme(), &np); err != nil {
		return fmt.Errorf("unable to set GVK to NetworkPolicy: %w", err)
	}

	if err := controllerutil.SetControllerReference(dscInit, &np, cli.Scheme()); err != nil {
		return fmt.Errorf("unable to add OwnerReference to the Network policy: %w", err)
	}

	err := resources.Apply(
		ctx,
		cli,
		&np,
		client.FieldOwner(fieldManager),
		client.ForceOwnership,
	)

	if err != nil {
		return err
	}

	return nil
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

func createNetworkPolicyPeer(key, value string) []networkingv1.NetworkPolicyPeer {
	return []networkingv1.NetworkPolicyPeer{{
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{key: value},
		},
	}}
}
