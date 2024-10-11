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
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	oauthv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	annotation "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	resourceRetryInterval = 10 * time.Second
	resourceRetryTimeout  = 1 * time.Minute
)

// SecretGeneratorReconciler holds the controller configuration.
type SecretGeneratorReconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretGeneratorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	log := r.Log
	log.Info("Adding controller for Secret Generation.")

	// Watch only new secrets with the corresponding annotation
	// seems we do get multiple events triggered for the same secret creation and even deletion
	predicates := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			_, found := e.Object.GetAnnotations()[annotation.SecretNameAnnotation]
			return found
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
		// this only watch for secret deletion if has with annotation
		// e.g. dashboard-oauth-client but not dashboard-oauth-client-generated
		DeleteFunc: func(e event.DeleteEvent) bool {
			_, found := e.Object.GetAnnotations()[annotation.SecretNameAnnotation]
			return found
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return false
		},
	}

	secretBuilder := ctrl.NewControllerManagedBy(mgr).Named("secret-generator-controller")
	err := secretBuilder.For(&corev1.Secret{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(
				func(_ context.Context, a client.Object) []reconcile.Request {
					namespacedName := types.NamespacedName{Name: a.GetName(), Namespace: a.GetNamespace()}
					return []reconcile.Request{{NamespacedName: namespacedName}}
				},
			), builder.WithPredicates(predicates)).
		WithEventFilter(predicates).
		Complete(r)

	return err
}

// Reconcile will generate new secret with random data for the annotated secret
// based on the specified type and complexity. This will avoid possible race
// conditions when a deployment mounts the secret before it is reconciled.
func (r *SecretGeneratorReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	log := r.Log
	foundSecret := &corev1.Secret{}
	err := r.Client.Get(ctx, request.NamespacedName, foundSecret)

	// deletion case
	if err != nil {
		if k8serr.IsNotFound(err) || foundSecret.GetDeletionTimestamp() != nil {
			log.Info("Reconciling Secret on deletion.", "Request.Name", request.Name)
			// delete OAuthClient if exists
			if err = r.deleteOAuthClient(ctx, request.Name); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log.Info("Reconciling Secret on creation.", "Request.Name", request.Name)
	// creation case
	owner := []metav1.OwnerReference{
		*metav1.NewControllerRef(foundSecret, foundSecret.GroupVersionKind()),
	}
	// Generate the secret if it does not previously exist
	generatedSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            foundSecret.Name + "-generated",
			Namespace:       foundSecret.Namespace,
			Labels:          foundSecret.Labels,
			OwnerReferences: owner,
		},
	}

	err = r.Client.Get(ctx, client.ObjectKey{Name: generatedSecret.Name, Namespace: generatedSecret.Namespace}, generatedSecret)
	if err != nil {
		if k8serr.IsNotFound(err) {
			// create a secret instance with values
			secret, err := NewSecretFrom(foundSecret.GetAnnotations())
			if err != nil {
				log.Error(err, "error setting secret values for %s "+generatedSecret.Name)
				return ctrl.Result{}, err
			}
			generatedSecret.StringData = map[string]string{
				secret.Name: secret.Value,
			}

			err = r.Client.Create(ctx, generatedSecret)
			if err != nil {
				log.Error(err, "error generating secret %s in %s", generatedSecret.Name, generatedSecret.Namespace)
				return ctrl.Result{}, err
			}
			log.Info("Done generating secret", "secret", generatedSecret.Name, "namespace", generatedSecret.Namespace)

			// check if annotation oauth-client-route exists
			// this is for dashboard-oauth-client secret, not dashboard-oauth-config
			if secret.OAuthClientRoute != "" {
				// Get OauthClient Route
				oauthClientRoute, err := r.getRoute(ctx, secret.OAuthClientRoute, request.Namespace)
				if err != nil {
					log.Error(err, "Unable to retrieve route for OAuthClient", "route-name", secret.OAuthClientRoute)
					return ctrl.Result{}, err
				}
				// Generate OAuthClient for the generated secret
				log.Info("Generating an OAuthClient CR for route", "route-name", oauthClientRoute.Name)
				err = r.createOAuthClient(ctx, foundSecret.Name, secret.Value, oauthClientRoute.Spec.Host)
				if err != nil {
					log.Error(err, "error creating AuthClient CR. Recreate the Secret", "secret-name", foundSecret.Name)
					return ctrl.Result{}, err
				}
			}
		} else {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// getRoute returns an OpenShift route object. It waits until the .spec.host value exists to avoid possible race conditions, fails otherwise.
func (r *SecretGeneratorReconciler) getRoute(ctx context.Context, name string, namespace string) (*routev1.Route, error) {
	route := &routev1.Route{}
	// Get spec.host from route
	err := wait.PollUntilContextTimeout(ctx, resourceRetryInterval, resourceRetryTimeout, false, func(ctx context.Context) (bool, error) {
		err := r.Client.Get(ctx, client.ObjectKey{
			Name:      name,
			Namespace: namespace,
		}, route)
		if err != nil {
			return false, client.IgnoreNotFound(err)
		}
		if route.Spec.Host == "" {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}

	return route, err
}

func (r *SecretGeneratorReconciler) createOAuthClient(ctx context.Context, name string, secretName string, uri string) error {
	log := r.Log
	// Create OAuthClient resource
	oauthClient := &oauthv1.OAuthClient{
		TypeMeta: metav1.TypeMeta{
			Kind:       "OAuthClient",
			APIVersion: "oauth.openshift.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Secret:       secretName,
		RedirectURIs: []string{"https://" + uri},
		GrantMethod:  oauthv1.GrantHandlerAuto,
	}

	err := r.Client.Create(ctx, oauthClient)
	if err != nil {
		if k8serr.IsAlreadyExists(err) {
			log.Info("OAuth client resource already exists, patch it", "name", oauthClient.Name)
			data, err := json.Marshal(oauthClient)
			if err != nil {
				return fmt.Errorf("failed to get DataScienceCluster custom resource data: %w", err)
			}
			if err = r.Client.Patch(ctx, oauthClient, client.RawPatch(types.ApplyPatchType, data),
				client.ForceOwnership, client.FieldOwner("rhods-operator")); err != nil {
				return fmt.Errorf("failed to patch existing OAuthClient CR: %w", err)
			}
			return nil
		}
	}

	return err
}

func (r *SecretGeneratorReconciler) deleteOAuthClient(ctx context.Context, secretName string) error {
	oauthClient := &oauthv1.OAuthClient{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: secretName,
	}, oauthClient)
	if err != nil {
		return client.IgnoreNotFound(err)
	}

	if err = r.Client.Delete(ctx, oauthClient); err != nil {
		return fmt.Errorf("error deleting OAuthClient %s: %w", oauthClient.Name, err)
	}

	return nil
}
