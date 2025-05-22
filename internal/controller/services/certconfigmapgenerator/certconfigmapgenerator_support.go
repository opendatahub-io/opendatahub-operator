// Package trustedcabundle provides utility functions to create and check trusted CA bundle configmap from DSCI CRD
package certconfigmapgenerator

import (
	"context"
	"reflect"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	annotation "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	CAConfigMapName           = "odh-trusted-ca-bundle"
	CADataFieldName           = "odh-ca-bundle.crt"
	TrustedCABundleFieldOwner = resources.PlatformFieldOwner + "/trustedcabundle"
	PartOf                    = "opendatahub-operator"
	NSListLimit               = 500
)

// CreateOdhTrustedCABundleConfigMap creates a configMap 'odh-trusted-ca-bundle' in given namespace with labels and data
// or update existing odh-trusted-ca-bundle configmap if already exists with new content of .data.odh-ca-bundle.crt
// this is certificates for the cluster trusted CA Cert Bundle.
func CreateOdhTrustedCABundleConfigMap(ctx context.Context, cli client.Client, namespace string, customCAData string) error {
	// Adding newline breaker if user input does not have it
	customCAData = strings.TrimSpace(customCAData) + "\n"

	// Expected configmap for the given namespace
	desiredConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CAConfigMapName,
			Namespace: namespace,
			Labels: map[string]string{
				labels.K8SCommon.PartOf: PartOf,
				// Label 'config.openshift.io/inject-trusted-cabundle' required for the Cluster Network Operator(CNO)
				// to inject the cluster trusted CA bundle into .data["ca-bundle.crt"]
				labels.InjectTrustCA: labels.True,
			},
		},
		// Add the DSCInitialzation specified TrustedCABundle.CustomCABundle to CM's data.odh-ca-bundle.crt field
		//
		// Additionally, the CNO operator will automatically create and maintain ca-bundle.crt
		// if label 'config.openshift.io/inject-trusted-cabundle' is true
		Data: map[string]string{
			CADataFieldName: customCAData,
		},
	}

	err := resources.Apply(
		ctx,
		cli,
		desiredConfigMap,
		client.FieldOwner(TrustedCABundleFieldOwner),
		client.ForceOwnership,
	)

	if err != nil {
		return err
	}

	return nil
}

func DeleteOdhTrustedCABundleConfigMap(ctx context.Context, cli client.Client, namespace string) error {
	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CAConfigMapName,
			Namespace: namespace},
	}

	err := cli.Delete(ctx, &cm)
	if err != nil && !k8serr.IsNotFound(err) {
		return err
	}

	return nil
}

func ShouldInjectTrustedCABundle(obj client.Object) bool {
	value, found := obj.GetAnnotations()[annotation.InjectionOfCABundleAnnotatoion]
	if !found {
		return true
	}

	shouldInject, err := strconv.ParseBool(value)
	if err != nil {
		return true
	}

	return shouldInject
}

// dsciEventHandler creates an event handler for DSCInitialization events. When a DSCInitialization
// resource changes, this handler enqueues reconciliation requests for all namespaces in the cluster,
// allowing the controller to update CA Bundle configuration across all namespaces.
//
// Parameters:
//   - cli: Kubernetes client used to list namespaces
//
// Returns:
//   - handler.EventHandler: Event handler that maps DSCInitialization events to namespace reconcile requests
func dsciEventHandler(cli client.Client) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		requests := make([]reconcile.Request, 0)

		lo := client.ListOptions{
			Limit: NSListLimit,
		}

		for {
			namespaces := corev1.NamespaceList{}

			if err := cli.List(ctx, &namespaces, &lo); err != nil {
				return []reconcile.Request{}
			}

			for _, ns := range namespaces.Items {
				requests = append(requests, reconcile.Request{
					NamespacedName: resources.NamespacedNameFromObject(&ns),
				})
			}

			if namespaces.Continue == "" {
				break
			}

			lo.Continue = namespaces.Continue
		}

		return requests
	})
}

// dsciPredicates creates predicates for filtering DSCInitialization events. It determines when
// reconciliation should be triggered based on relevant changes to DSCInitialization resources:
// - Always reconcile on resource creation
// - Reconcile on updates only when the TrustedCABundle configuration changes
// - Never reconcile on resource deletion
//
// Parameters:
//   - _: Unused client parameter (kept for interface compatibility)
//
// Returns:
//   - predicate.Funcs: Event filter predicates for DSCInitialization events
func dsciPredicates(_ client.Client) predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},

		UpdateFunc: func(e event.UpdateEvent) bool {
			dsciOld, ok := e.ObjectOld.(*dsciv1.DSCInitialization)
			if !ok {
				return false
			}
			dsciNew, ok := e.ObjectNew.(*dsciv1.DSCInitialization)
			if !ok {
				return false
			}

			return !reflect.DeepEqual(dsciOld.Spec.TrustedCABundle, dsciNew.Spec.TrustedCABundle)
		},

		DeleteFunc: func(deleteEvent event.DeleteEvent) bool {
			return false
		},
	}
}
