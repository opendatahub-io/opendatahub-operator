package secretgenerator

import (
	"context"
	"fmt"
	"time"

	ocv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	resourceRetryInterval = 10 * time.Second
	resourceRetryTimeout  = 1 * time.Minute
	)

// ReconcileSecretGenerator holds the controller configuration
type ReconcileSecretGenerator struct {
	client client.Client
	scheme *runtime.Scheme
}

// newReconciler initializes the ReconcileSecretGenerator object as
// reconcile.Reconciler type
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileSecretGenerator{
		client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
	}
}

// AddToManager creates a new controller and adds it to the manager. The manager
// will set fields on the controller and start it when the manager is started.
func AddToManager(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// add adds a new controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New(
		"secretgenerator-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch only new secrets with the corresponding annotation
	predicates := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			object, _ := meta.Accessor(e.Object)
			if _, found := object.GetAnnotations()[SECRET_NAME_ANNOTATION]; found {
				return true
			}
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			object, _ := meta.Accessor(e.Object)
			if _, found := object.GetAnnotations()[SECRET_NAME_ANNOTATION]; found {
				return true
			}
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return false
		},
	}

	// Watch for changes to Kubernetes secrets
	err = c.Watch(&source.Kind{Type: &v1.Secret{}}, &handler.EnqueueRequestsFromMapFunc{
		ToRequests: handler.ToRequestsFunc(func(a handler.MapObject) []reconcile.Request {
			namespacedName := types.NamespacedName{Name: a.Meta.GetName(), Namespace: a.Meta.GetNamespace()}
			return []reconcile.Request{{NamespacedName: namespacedName}}
		}),
	}, predicates)
	if err != nil {
		return err
	}

	log.Infof("Controller added to watch secrets with known GVK.")
	return nil
}

// Reconcile will generate new secret with random data for the annotated secret
// based on the specified type and complexity. This will avoid possible race
// conditions when a deployment mounts the secret before it is reconciled
func (r *ReconcileSecretGenerator) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	foundSecret := &v1.Secret{}
	err := r.client.Get(context.TODO(), request.NamespacedName, foundSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// If Secret is deleted, delete OAuthClient if exists
			err = r.deleteOAuthClient(request.Name)
		}
		return reconcile.Result{}, err
	}

	owner := []metav1.OwnerReference{
		*metav1.NewControllerRef(foundSecret, foundSecret.GroupVersionKind()),
	}
	// Generate the secret if it does not previously exist
	generatedSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            foundSecret.Name + "-generated",
			Namespace:       foundSecret.Namespace,
			Labels:          foundSecret.Labels,
			OwnerReferences: owner,
		},
	}

	generatedSecretKey := types.NamespacedName{
		Name: generatedSecret.Name, Namespace: generatedSecret.Namespace}
	err = r.client.Get(context.TODO(), generatedSecretKey, generatedSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Generate secret random value
			log.Infof("Generating a random value for %v secret in the %v namespace",
				generatedSecret.Name, generatedSecret.Namespace)

			secret, err := newSecret(foundSecret.GetAnnotations())
			if err != nil {
				log.Errorf("error creating secret: %v", err)
				return reconcile.Result{}, err
			}

			generatedSecret.StringData = map[string]string{
				secret.Name: secret.Value,
			}

			err = r.client.Create(context.TODO(), generatedSecret)
			if err != nil {
				return reconcile.Result{}, err
			}
			if secret.OAuthClientRoute != "" {
				// Get OauthClient Route
				oauthClientRoute, err := r.getRoute(secret.OAuthClientRoute, request.Namespace)
				if err != nil {
					log.Errorf("Unable to retrieve route %v: %v", secret.OAuthClientRoute, err)
					return reconcile.Result{}, err
				}
				// Generate OAuthClient for the generated secret
				log.Infof("Generating an oauth client resource for %v route", oauthClientRoute.Name)
				err = r.createOAuthClient(foundSecret.Name, secret.Value, oauthClientRoute.Spec.Host)
				if err != nil {
					log.Errorf("error creating oauth client resource: %v. Recreate Secret : %v", err,
						foundSecret.Name)
					return reconcile.Result{}, err
				}
			}
		} else {
			return reconcile.Result{}, err
		}
	}

	// Don't requeue if secret is created successfully
	return reconcile.Result{}, err
}

// getRoute returns an OpenShift route object. It waits until the .spec.host value exists to avoid possible race conditions, fails otherwise.
func (r *ReconcileSecretGenerator) getRoute(name string, namespace string) (*routev1.Route, error) {
	route := &routev1.Route{}
	// Get spec.host from route
	err := wait.PollImmediate(resourceRetryInterval, resourceRetryTimeout, func() (done bool, err error) {
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: name,
			Namespace: namespace}, route)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		} else if route.Spec.Host == "" {
			return false, nil
		} else {
			return true, nil
		}
	})

	if err != nil {
		return nil, err
	}
	return route, err
}

func (r *ReconcileSecretGenerator) createOAuthClient(name string, secret string, uri string) error {
	// Create OAuthClient resource
	oauthClient := &ocv1.OAuthClient{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Secret:       secret,
		RedirectURIs: []string{"https://" + uri},
		GrantMethod:  ocv1.GrantHandlerAuto,
	}

	err := r.client.Create(context.TODO(), oauthClient)
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			log.Infof("OAuth client resource %v already exists", oauthClient.Name)
			return nil
		}
	}
	return err
}

func (r *ReconcileSecretGenerator) deleteOAuthClient(secretName string) error {
	oauthClient := &ocv1.OAuthClient{}

	err := r.client.Get(context.TODO(), types.NamespacedName{Name: secretName},oauthClient)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	err = r.client.Delete(context.TODO(), oauthClient)
	if err != nil {
		return fmt.Errorf("error deleting OAuthClient %v", oauthClient.Name)
	}

	return err
}
