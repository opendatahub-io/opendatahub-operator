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
				// Server-side apply removes the need to cache the ConfigMap, as we
				// don’t need to access any of its fields. We only watch it to detect
				// and revert any external modifications.
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
		// The Namespaces cache is unrestricted, so we rely on the shared cache.
		//
		// Currently, all Namespaces are cached because the controller must handle any
		// namespace (excluding reserved ones). We filter by namespace phase to skip
		// those in the process of termination.
		//
		// In the future, we should implement an opt-in mechanism to selectively cache
		// namespaces using label selectors. This would allow us to use a dedicated
		// cache for this controller and separate ones for components and services.
		source.TypedKind[client.Object, ctrl.Request](
			mgr.GetCache(),
			&corev1.Namespace{},
			handlers.RequestFromObject(),
			respredicates.AnnotationChanged(annotation.InjectionOfCABundleAnnotatoion),
		),
	)

	//
	// Configmap
	//
	b = b.WatchesRawSource(
		// Use a custom cache to avoid affecting the global cache managed by the controller manager.
		//
		// Server-side apply removes the need to cache the ConfigMap itself.
		// We only watch the ConfigMap to detect and revert any external modifications.
		//
		// Leveraging PartialObjectMetadata minimizes API server load and reduces network traffic
		// by fetching only metadata instead of the full object.
		source.TypedKind[client.Object, ctrl.Request](
			targetCache,
			resources.GvkToPartial(gvk.ConfigMap),
			handlers.Fn(func(_ context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Name: obj.GetNamespace(),
					},
				}}
			}),
		),
	)

	//
	// DSCInitialization
	//
	b = b.WatchesRawSource(
		// The DSCInitialization singleton is shared across nearly all controllers.
		// It uses the manager's shared cache to prevent the creation of redundant informers.
		source.TypedKind[client.Object, ctrl.Request](
			mgr.GetCache(),
			&dsciv1.DSCInitialization{},
			dsciEventHandler(r.sharedClient),
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
		return ctrl.Result{}, fmt.Errorf("failed to retrieve DSCInitialization: %w", err)
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
