package secretgenerator

import (
	"context"
	"fmt"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"time"

	ocv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	resourceRetryInterval = 10 * time.Second
	resourceRetryTimeout  = 1 * time.Minute
)

var secGenLog = log.Log.WithName("secret-generator")

// ReconcileSecretGenerator holds the controller configuration
type SecretGeneratorReconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
}

func (r *SecretGeneratorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	secGenLog.Info("Adding controller for Secret Generation.")

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

	secretBuilder := ctrl.NewControllerManagedBy(mgr).Named("secret-generator-controller")
	err := secretBuilder.Watches(&source.Kind{Type: &v1.Secret{}}, handler.EnqueueRequestsFromMapFunc(
		func(a client.Object) []reconcile.Request {
			namespacedName := types.NamespacedName{Name: a.GetName(), Namespace: a.GetNamespace()}
			return []reconcile.Request{{NamespacedName: namespacedName}}
		}), builder.WithPredicates(predicates)).
		Complete(r)

	return err
}

// Reconcile will generate new secret with random data for the annotated secret
// based on the specified type and complexity. This will avoid possible race
// conditions when a deployment mounts the secret before it is reconciled
func (r *SecretGeneratorReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	foundSecret := &v1.Secret{}
	err := r.Client.Get(context.TODO(), request.NamespacedName, foundSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// If Secret is deleted, delete OAuthClient if exists
			err = r.deleteOAuthClient(request.Name)
		}
		return ctrl.Result{}, err
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
	err = r.Client.Get(context.TODO(), generatedSecretKey, generatedSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Generate secret random value
			secGenLog.Info("Generating a random value for a secret in a namespace",
				"secret", generatedSecret.Name, "namespace", generatedSecret.Namespace)

			secret, err := newSecret(foundSecret.GetAnnotations())
			if err != nil {
				secGenLog.Error(err, "error creating secret")
				return ctrl.Result{}, err
			}

			generatedSecret.StringData = map[string]string{
				secret.Name: secret.Value,
			}

			err = r.Client.Create(context.TODO(), generatedSecret)
			if err != nil {
				return ctrl.Result{}, err
			}
			if secret.OAuthClientRoute != "" {
				// Get OauthClient Route
				oauthClientRoute, err := r.getRoute(secret.OAuthClientRoute, request.Namespace)
				if err != nil {
					secGenLog.Error(err, "Unable to retrieve route", "route-name", secret.OAuthClientRoute)
					return ctrl.Result{}, err
				}
				// Generate OAuthClient for the generated secret
				secGenLog.Info("Generating an oauth client resource for route", "route-name", oauthClientRoute.Name)
				err = r.createOAuthClient(foundSecret.Name, secret.Value, oauthClientRoute.Spec.Host)
				if err != nil {
					secGenLog.Error(err, "error creating oauth client resource. Recreate the Secret", "secret-name",
						foundSecret.Name)
					return ctrl.Result{}, err
				}
			}
		} else {
			return ctrl.Result{}, err
		}
	}

	// Don't requeue if secret is created successfully
	return ctrl.Result{}, err
}

// getRoute returns an OpenShift route object. It waits until the .spec.host value exists to avoid possible race conditions, fails otherwise.
func (r *SecretGeneratorReconciler) getRoute(name string, namespace string) (*routev1.Route, error) {
	route := &routev1.Route{}
	// Get spec.host from route
	err := wait.PollImmediate(resourceRetryInterval, resourceRetryTimeout, func() (done bool, err error) {
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: name,
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

func (r *SecretGeneratorReconciler) createOAuthClient(name string, secret string, uri string) error {
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

	err := r.Client.Create(context.TODO(), oauthClient)
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			secGenLog.Info("OAuth client resource already exists", "name", oauthClient.Name)
			return nil
		}
	}
	return err
}

func (r *SecretGeneratorReconciler) deleteOAuthClient(secretName string) error {
	oauthClient := &ocv1.OAuthClient{}

	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: secretName}, oauthClient)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	err = r.Client.Delete(context.TODO(), oauthClient)
	if err != nil {
		return fmt.Errorf("error deleting OAuthClient %v", oauthClient.Name)
	}

	return err
}
