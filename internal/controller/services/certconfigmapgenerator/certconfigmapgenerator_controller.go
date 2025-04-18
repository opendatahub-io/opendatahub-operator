// Package certconfigmapgenerator contains generator logic of add cert configmap resource in user namespaces
package certconfigmapgenerator

import (
	"context"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	respredicates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	annotation "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	odhlabels "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// CertConfigmapGeneratorReconciler holds the controller configuration.
type CertConfigmapGeneratorReconciler struct {
	sharedClient client.Client
	certClient   client.Client
}

// NewWithManager sets up the controller with the Manager.
func NewWithManager(_ context.Context, mgr ctrl.Manager) error {
	r := CertConfigmapGeneratorReconciler{}

	targetCache, err := cache.New(mgr.GetConfig(), cache.Options{
		HTTPClient:                  mgr.GetHTTPClient(),
		Scheme:                      mgr.GetScheme(),
		Mapper:                      mgr.GetRESTMapper(),
		ReaderFailOnMissingInformer: true,
		ByObject: map[client.Object]cache.ByObject{
			&corev1.ConfigMap{}: {
				// We don't need to cache all the configmaps, but only those designated to
				// hold Trust CA Bundles, that can be discriminated using a label selector
				// and a field selector (as the name is fixed).
				Label: labels.Set{odhlabels.K8SCommon.PartOf: PartOf}.AsSelector(),
				Field: fields.Set{"metadata.name": CAConfigMapName}.AsSelector(),
			},
		},
		DefaultTransform: func(in any) (any, error) {
			if obj, err := meta.Accessor(in); err == nil && obj.GetManagedFields() != nil {
				obj.SetManagedFields(nil)
			}

			return in, nil
		},
	})

	if err != nil {
		return fmt.Errorf("unable to create cache: %w", err)
	}

	err = mgr.Add(targetCache)
	if err != nil {
		return fmt.Errorf("unable to register target cache to manager: %w", err)
	}

	// create a new client that uses the custom cache
	targetClient, err := client.New(mgr.GetConfig(), client.Options{
		HTTPClient: mgr.GetHTTPClient(),
		Scheme:     mgr.GetScheme(),
		Mapper:     mgr.GetRESTMapper(),
		Cache: &client.CacheOptions{
			Unstructured: true,
			Reader:       targetCache,
			DisableFor: []client.Object{
				// Server side apply eliminates the need for configmap caching as there is no
				// need to access to any ConfigMap field. We only need to watch the configmap
				// to detect and revert any external changes.
				&corev1.ConfigMap{},
			},
		},
	})

	if err != nil {
		return fmt.Errorf("unable to create client: %w", err)
	}

	r.sharedClient = mgr.GetClient()
	r.certClient = targetClient

	b := ctrl.NewControllerManagedBy(mgr).
		Named("cert-configmap-generator-controller")

	//
	// Namespace
	//
	b = b.WatchesRawSource(
		// Namespaces cache is not restricted, hence use the shared cache.
		//
		// We currently cache all Namespaces because the controller needs to operate on any
		// namespace (except reserved ones). We check the namespace phase to avoid processing
		// any terminating namespaces.
		//
		// In the future, we should implement an opt-in mechanism that would allow us to cache
		// namespaces more selectively using label selectors. In such case we can also use a
		// dedicated cache for this controller and a dedicated one for the components/services.
		source.Kind(mgr.GetCache(), &corev1.Namespace{}),
		handlers.RequestFromObject(),
		builder.WithPredicates(
			respredicates.AnnotationChanged(annotation.InjectionOfCABundleAnnotatoion),
		),
	)

	//
	// Configmap
	//
	b = b.WatchesRawSource(
		// Use custom cache so it does not impact the global cache provided by the manager.
		//
		// Server side apply eliminates the need for configmap caching.
		// We only need to watch the configmap to detect and revert any external changes.
		//
		// Using PartialObjectMetadata reduces API server load and network traffic
		// by retrieving only metadata information instead of the complete object.
		source.Kind(targetCache, resources.GvkToPartial(gvk.ConfigMap)),
		handlers.Fn(func(_ context.Context, obj client.Object) []reconcile.Request {
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Name: obj.GetNamespace(),
				},
			}}
		}),
	)

	//
	// DSCInitialization
	//
	b = b.WatchesRawSource(
		// The DSCInitialization singleton is an object that is shared among pretty much
		// all the controllers, then we use the shared cache from the manager to avoid
		// creating redundant informers.
		source.Kind(mgr.GetCache(), &dsciv1.DSCInitialization{}),
		dsciEventHandler(r.sharedClient),
		builder.WithPredicates(
			dsciPredicates(r.sharedClient),
		),
	)

	return b.Complete(
		reconcile.AsReconciler[*corev1.Namespace](r.sharedClient, &r),
	)
}

// Reconcile will generate new configmap, odh-trusted-ca-bundle, that includes cluster-wide
// trusted-ca bundle and custom ca bundle in every new namespace created.
func (r *CertConfigmapGeneratorReconciler) Reconcile(ctx context.Context, ns *corev1.Namespace) (ctrl.Result, error) {
	l := logf.FromContext(ctx)

	if !cluster.IsActiveNamespace(ns) {
		l.V(3).Info("Namespace not active, skip")
		return ctrl.Result{}, nil
	}

	if cluster.IsReservedNamespace(ns) {
		l.V(3).Info("Namespace is reserved, skip")
		return ctrl.Result{}, nil
	}

	dsci, err := cluster.GetDSCI(ctx, r.sharedClient)
	switch {
	case k8serr.IsNotFound(err):
		return ctrl.Result{}, nil
	case err != nil:
		return ctrl.Result{}, fmt.Errorf("failed to retrieve DSCInitializationr: %w", err)
	}

	switch {
	case dsci.Spec.TrustedCABundle == nil || dsci.Spec.TrustedCABundle.ManagementState != operatorv1.Managed:
		l.Info("TrustedCABundle is not set as Managed, skip CA bundle injection and delete existing configmap")

		if err := DeleteOdhTrustedCABundleConfigMap(ctx, r.certClient, ns.Name); err != nil {
			return reconcile.Result{}, fmt.Errorf("error deleting existing configmap: %w", err)
		}

	case resources.HasAnnotation(ns, annotation.InjectionOfCABundleAnnotatoion, "false"):
		l.Info("Namespace has opted-out of CA bundle injection, deleting it")

		if err := DeleteOdhTrustedCABundleConfigMap(ctx, r.certClient, ns.Name); err != nil {
			return reconcile.Result{}, fmt.Errorf("error deleting existing configmap: %w", err)
		}

	default:
		l.Info("Adding CA bundle configmap")

		if err := CreateOdhTrustedCABundleConfigMap(ctx, r.certClient, ns.Name, dsci.Spec.TrustedCABundle.CustomCABundle); err != nil {
			return reconcile.Result{}, fmt.Errorf("error adding configmap to namespace: %w", err)
		}
	}

	return ctrl.Result{}, nil
}
