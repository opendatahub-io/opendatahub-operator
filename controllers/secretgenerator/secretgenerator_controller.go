/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package secretgenerator contains generator logic of secret resources used in Open Data Hub operator
package secretgenerator

import (
	"context"
	"fmt"
	"time"

	ocv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
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

// +kubebuilder:rbac:groups="oauth.openshift.io",resources=oauthclients,verbs=create;delete;get
// +kubebuilder:rbac:groups="core",resources=secrets,verbs=watch;get;create
// +kubebuilder:rbac:groups="route.openshift.io",resources=routes,verbs=get
// +kubebuilder:rbac:groups="core",resources=secrets/finalizers,verbs=*

// SecretGeneratorReconciler holds the controller configuration.
type SecretGeneratorReconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretGeneratorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	secGenLog.Info("Adding controller for Secret Generation.")

	// Watch only new secrets with the corresponding annotation
	predicates := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			if _, found := e.Object.GetAnnotations()[SECRET_NAME_ANNOTATION]; found {
				return true
			}
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			if _, found := e.Object.GetAnnotations()[SECRET_NAME_ANNOTATION]; found {
				return true
			}
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return false
		},
	}

	secretBuilder := ctrl.NewControllerManagedBy(mgr).Named("secret-generator-controller")
	err := secretBuilder.For(&v1.Secret{}).
		Watches(&source.Kind{Type: &v1.Secret{}}, handler.EnqueueRequestsFromMapFunc(
			func(a client.Object) []reconcile.Request {
				namespacedName := types.NamespacedName{Name: a.GetName(), Namespace: a.GetNamespace()}
				return []reconcile.Request{{NamespacedName: namespacedName}}
			}), builder.WithPredicates(predicates)).WithEventFilter(predicates).
		Complete(r)

	return err
}

// Reconcile will generate new secret with random data for the annotated secret
// based on the specified type and complexity. This will avoid possible race
// conditions when a deployment mounts the secret before it is reconciled.
func (r *SecretGeneratorReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	foundSecret := &v1.Secret{}
	err := r.Client.Get(ctx, request.NamespacedName, foundSecret)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// If Secret is deleted, delete OAuthClient if exists
			err = r.deleteOAuthClient(ctx, request.Name)
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
		Name: generatedSecret.Name, Namespace: generatedSecret.Namespace,
	}
	err = r.Client.Get(ctx, generatedSecretKey, generatedSecret)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// Generate secret random value
			secGenLog.Info("Generating a random value for a secret in a namespace",
				"secret", generatedSecret.Name, "namespace", generatedSecret.Namespace)

			secret, err := NewSecretFrom(foundSecret.GetAnnotations())
			if err != nil {
				secGenLog.Error(err, "error creating secret")
				return ctrl.Result{}, err
			}

			generatedSecret.StringData = map[string]string{
				secret.Name: secret.Value,
			}

			err = r.Client.Create(ctx, generatedSecret)
			if err != nil {
				return ctrl.Result{}, err
			}
			if secret.OAuthClientRoute != "" {
				// Get OauthClient Route
				oauthClientRoute, err := r.getRoute(ctx, secret.OAuthClientRoute, request.Namespace)
				if err != nil {
					secGenLog.Error(err, "Unable to retrieve route", "route-name", secret.OAuthClientRoute)
					return ctrl.Result{}, err
				}
				// Generate OAuthClient for the generated secret
				secGenLog.Info("Generating an oauth client resource for route", "route-name", oauthClientRoute.Name)
				err = r.createOAuthClient(ctx, foundSecret.Name, secret.Value, oauthClientRoute.Spec.Host)
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
func (r *SecretGeneratorReconciler) getRoute(ctx context.Context, name string, namespace string) (*routev1.Route, error) {
	route := &routev1.Route{}
	// Get spec.host from route
	err := wait.PollUntilContextTimeout(ctx, resourceRetryInterval, resourceRetryTimeout, false, func(ctx context.Context) (done bool, err error) {
		err = r.Client.Get(ctx, client.ObjectKey{
			Name:      name,
			Namespace: namespace,
		}, route)
		if err != nil {
			if apierrs.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if route.Spec.Host == "" {
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

func (r *SecretGeneratorReconciler) createOAuthClient(ctx context.Context, name string, secretName string, uri string) error {
	// Create OAuthClient resource
	oauthClient := &ocv1.OAuthClient{
		TypeMeta: metav1.TypeMeta{
			Kind:       "OAuthClient",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Secret:       secretName,
		RedirectURIs: []string{"https://" + uri},
		GrantMethod:  ocv1.GrantHandlerAuto,
	}

	err := r.Client.Create(ctx, oauthClient)
	if err != nil {
		if apierrs.IsAlreadyExists(err) {
			secGenLog.Info("OAuth client resource already exists", "name", oauthClient.Name)
			return nil
		}
	}
	return err
}

func (r *SecretGeneratorReconciler) deleteOAuthClient(ctx context.Context, secretName string) error {
	oauthClient := &ocv1.OAuthClient{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: secretName,
	}, oauthClient)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil
		}
		return err
	}

	err = r.Client.Delete(ctx, oauthClient)
	if err != nil {
		return fmt.Errorf("error deleting OAuthClient %v", oauthClient.Name)
	}

	return nil
}
