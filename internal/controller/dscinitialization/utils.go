package dscinitialization

import (
	"context"
	"errors"
	"fmt"
	"maps"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// createOperatorResource include steps:
// - 1. validate customized application namespace || create/update default application namespace
//   - patch application namespaces for managed cluster
//   - Odh specific labels for access
//   - Pod security labels for baseline permissions
//
// - 2. Patch monitoring namespace (create + label for ownership and pod security)
// - 3. Network Policies 'opendatahub' that allow traffic between the ODH namespaces.
func (r *DSCInitializationReconciler) createOperatorResource(ctx context.Context, dscInit *dsciv2.DSCInitialization, platform common.Platform) error {
	log := logf.FromContext(ctx)

	if err := r.appNamespaceHandler(ctx, dscInit, platform); err != nil {
		log.Error(err, "error handle application namespace")
		return err
	}

	if dscInit.Spec.Monitoring.ManagementState == operatorv1.Managed {
		if err := PatchMonitoringNS(ctx, r.Client, dscInit); err != nil {
			log.Error(err, "error patching monitoring namespace")
			return err
		}
	}

	// Create default NetworkPolicy for the namespace
	if err := ReconcileDefaultNetworkPolicy(ctx, r.Client, dscInit, platform); err != nil {
		return err
	}

	return nil
}

func (r *DSCInitializationReconciler) appNamespaceHandler(ctx context.Context, dscInit *dsciv2.DSCInitialization, platform common.Platform) error {
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
		maps.Copy(labelList, l)
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, desiredDefaultNS, func() error {
		// Preserve elevated PSA only when another controller explicitly claims ownership
		// via the PSAElevatedBy annotation (e.g. KServe ModelCache).
		if desiredDefaultNS.Labels[labels.SecurityEnforce] == "privileged" &&
			resources.GetAnnotation(desiredDefaultNS, annotations.PSAElevatedBy) != "" {
			delete(labelList, labels.SecurityEnforce)
		}
		resources.SetLabels(desiredDefaultNS, labelList)
		return nil
	})
	return err
}

// PatchMonitoringNS ensures the monitoring namespace exists and sets labels for
// operator ownership (ODH.OwnedNamespace) and pod security baseline (SecurityEnforce).
func PatchMonitoringNS(ctx context.Context, cli client.Client, dscInit *dsciv2.DSCInitialization) error {
	monitoringName := dscInit.Spec.Monitoring.Namespace
	if dscInit.Spec.ApplicationsNamespace == monitoringName {
		return nil
	}

	desiredMonitoringNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: monitoringName,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, cli, desiredMonitoringNamespace, func() error {
		resources.SetLabels(desiredMonitoringNamespace, map[string]string{
			labels.ODH.OwnedNamespace: labels.True,
			labels.SecurityEnforce:    "baseline",
		})
		return nil
	})

	return err
}

func ReconcileDefaultNetworkPolicy(
	ctx context.Context,
	cli client.Client,
	dscInit *dsciv2.DSCInitialization,
	_ common.Platform,
) error {
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
				From: createNetworkPolicyPeer("kubernetes.io/metadata.name", "openshift-monitoring")}, {
				From: createNetworkPolicyPeer("kubernetes.io/metadata.name", "openshift-cluster-observability-operator")},
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

func createNetworkPolicyPeer(key, value string) []networkingv1.NetworkPolicyPeer {
	return []networkingv1.NetworkPolicyPeer{{
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{key: value},
		},
	}}
}
