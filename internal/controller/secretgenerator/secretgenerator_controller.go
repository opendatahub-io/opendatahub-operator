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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	annotation "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	resourceRetryInterval = 10 * time.Second
	resourceRetryTimeout  = 1 * time.Minute
)

// SecretGeneratorReconciler holds the controller configuration.
type SecretGeneratorReconciler struct {
	*odhClient.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretGeneratorReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	logf.FromContext(ctx).Info("Adding controller for Secret Generation.")

	// Watch only new secrets with the corresponding annotation
	predicates := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			if _, found := e.Object.GetAnnotations()[annotation.SecretNameAnnotation]; found {
				return true
			}

			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
		// this only watch for secret deletion if has with annotation
		// e.g. dashboard-oauth-client but not dashboard-oauth-client-generated
		DeleteFunc: func(e event.DeleteEvent) bool {
			if _, found := e.Object.GetAnnotations()[annotation.SecretNameAnnotation]; found {
				return true
			}

			return false
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
	foundSecret := &corev1.Secret{}
	err := r.Client.Get(ctx, request.NamespacedName, foundSecret)
	if err != nil {
		if !k8serr.IsNotFound(err) {
			return ctrl.Result{}, err
		}

		// If Secret is deleted, delete OAuthClient if exists
		err = r.deleteOAuthClient(ctx, request.NamespacedName)
		if err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// Generate the secret if it does not previously exist
	generatedSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      foundSecret.Name + "-generated",
			Namespace: foundSecret.Namespace,
		},
	}

	err = r.Client.Get(ctx, client.ObjectKeyFromObject(generatedSecret), generatedSecret)
	if err == nil || !k8serr.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	err = r.generateSecret(ctx, foundSecret, generatedSecret)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Don't requeue if secret is created successfully
	return ctrl.Result{}, nil
}

func (r *SecretGeneratorReconciler) generateSecret(ctx context.Context, foundSecret *corev1.Secret, generatedSecret *corev1.Secret) error {
	log := logf.FromContext(ctx).WithName("SecretGenerator")

	// Generate secret random value
	log.Info("Generating a random value for a secret in a namespace",
		"secret", generatedSecret.Name, "namespace", generatedSecret.Namespace)

	generatedSecret.Labels = foundSecret.Labels

	generatedSecret.OwnerReferences = []metav1.OwnerReference{
		*metav1.NewControllerRef(foundSecret, foundSecret.GroupVersionKind()),
	}

	secret, err := NewSecretFrom(foundSecret.GetAnnotations())
	if err != nil {
		log.Error(err, "error creating secret %s in %s", generatedSecret.Name, generatedSecret.Namespace)
		return err
	}

	generatedSecret.StringData = map[string]string{
		secret.Name: secret.Value,
	}

	err = r.Client.Create(ctx, generatedSecret)
	if err != nil {
		return err
	}

	log.Info("Done generating secret in namespace",
		"secret", generatedSecret.Name, "namespace", generatedSecret.Namespace)

	// check if annotation oauth-client-route exists
	if secret.OAuthClientRoute == "" {
		return nil
	}

	// Get OauthClient Route
	oauthClientRoute, err := r.getRoute(ctx, secret.OAuthClientRoute, foundSecret.Namespace)
	if err != nil {
		log.Error(err, "Unable to retrieve route from OAuthClient", "route-name", secret.OAuthClientRoute)
		return err
	}

	// Generate OAuthClient for the generated secret
	log.Info("Generating an OAuthClient CR for route", "route-name", oauthClientRoute.Name)
	err = r.createOAuthClient(ctx, foundSecret.Name, secret.Value, oauthClientRoute.Spec.Host)
	if err != nil {
		log.Error(err, "error creating oauth client resource. Recreate the Secret", "secret-name",
			foundSecret.Name)

		return err
	}

	return nil
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
	log := logf.FromContext(ctx)
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

func (r *SecretGeneratorReconciler) deleteOAuthClient(ctx context.Context, secretNamespacedName types.NamespacedName) error {
	oauthClient := &oauthv1.OAuthClient{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretNamespacedName.Name,
			Namespace: secretNamespacedName.Namespace,
		},
	}

	err := r.Client.Delete(ctx, oauthClient)
	if err != nil && !k8serr.IsNotFound(err) {
		return fmt.Errorf("error deleting OAuthClient %s: %w", oauthClient.Name, err)
	}

	return nil
}
