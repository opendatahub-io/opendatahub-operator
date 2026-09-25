package main

import (
	"reflect"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	userv1 "github.com/openshift/api/user/v1"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

func TestNewCacheOptions_DefaultNamespacesSet(t *testing.T) {
	t.Parallel()

	namespaces := map[string]cache.Config{
		"test-operator":   {},
		"test-apps":       {},
		"test-monitoring": {},
	}

	opts := newCacheOptions(runtime.NewScheme(), namespaces, nil)
	assert.Equal(t, namespaces, opts.DefaultNamespaces,
		"DefaultNamespaces must be set to oDHCache so unscoped types do not watch cluster-wide")
}

func TestNewCacheOptions_ByObjectNamespaceAssignments(t *testing.T) {
	t.Parallel()

	oDHCache := map[string]cache.Config{
		"odh-operator":      {},
		"odh-apps":          {},
		"odh-monitoring":    {},
		"openshift-ingress": {},
	}
	secretCache := map[string]cache.Config{
		"odh-operator":      {},
		"odh-apps":          {},
		"odh-monitoring":    {},
		"openshift-ingress": {},
		"secret-extra-ns":   {},
	}

	opts := newCacheOptions(runtime.NewScheme(), oDHCache, secretCache)

	// Secret is the only type whose namespace scope differs from DefaultNamespaces
	// (secretCache is a strict subset of oDHCache), so it is the only type that needs
	// an explicit ByObject override.
	wantSecretCache := []client.Object{
		&corev1.Secret{},
	}
	// These types are scoped to oDHCache via DefaultNamespaces: controller-runtime
	// defaults ByObject.Namespaces to DefaultNamespaces for every type not listed in
	// ByObject, so listing them explicitly would be redundant. They must therefore be
	// absent from ByObject (and still namespace-scoped through DefaultNamespaces).
	wantDefaultScoped := []client.Object{
		&corev1.ConfigMap{},
		&appsv1.Deployment{},
		&networkingv1.NetworkPolicy{},
		&rbacv1.Role{},
		&rbacv1.RoleBinding{},
		&corev1.ServiceAccount{},
		&corev1.Service{},
		&corev1.PersistentVolumeClaim{},
	}

	for _, obj := range wantSecretCache {
		typeName := reflect.TypeOf(obj).Elem().Name()
		byObj, found := findByObject(opts, obj)
		require.True(t, found, "%s must be in ByObject", typeName)
		assert.Equal(t, secretCache, byObj.Namespaces,
			"%s must use secretCache namespaces, not oDHCache", typeName)
	}

	for _, obj := range wantDefaultScoped {
		typeName := reflect.TypeOf(obj).Elem().Name()
		_, found := findByObject(opts, obj)
		assert.False(t, found,
			"%s must NOT be in ByObject — it is scoped to oDHCache via DefaultNamespaces", typeName)
	}

	// DefaultNamespaces carries the oDHCache scope for all default-scoped types.
	assert.Equal(t, oDHCache, opts.DefaultNamespaces,
		"DefaultNamespaces must be oDHCache so default-scoped types stay namespace-limited")

	assert.Len(t, opts.ByObject, len(wantSecretCache),
		"ByObject should contain exactly Secret (IngressController/Auth/Route added separately by OpenShift setup)")
}

func findByObject(opts cache.Options, target client.Object) (cache.ByObject, bool) {
	targetType := reflect.TypeOf(target)
	for key, byObj := range opts.ByObject {
		if reflect.TypeOf(key) == targetType {
			return byObj, true
		}
	}

	return cache.ByObject{}, false
}

func TestCacheDisableFor_ContainsExpectedTypes(t *testing.T) {
	t.Parallel()

	expectedTyped := []client.Object{
		&configv1.Infrastructure{},
		&ofapiv1alpha1.Subscription{},
		&authorizationv1.SelfSubjectRulesReview{},
		&corev1.Pod{},
		&corev1.Node{},
		&userv1.Group{},
		&ofapiv1alpha1.CatalogSource{},
		&ofapiv1alpha1.ClusterServiceVersion{},
	}

	disabled := cacheDisableFor()

	for _, obj := range expectedTyped {
		typeName := reflect.TypeOf(obj).Elem().Name()
		found := false
		for _, d := range disabled {
			if reflect.TypeOf(d) == reflect.TypeOf(obj) {
				found = true
				break
			}
		}
		assert.True(t, found,
			"%s must be in DisableFor — it has no scoped informer and would otherwise start an unfiltered cluster-wide one", typeName)
	}

	foundIngress := false
	for _, d := range disabled {
		if u, ok := d.(*unstructured.Unstructured); ok && u.GetObjectKind().GroupVersionKind() == gvk.OpenshiftIngress {
			foundIngress = true
			break
		}
	}
	assert.True(t, foundIngress,
		"OpenshiftIngress (unstructured) must be in DisableFor — it has no scoped informer and would otherwise start an unfiltered cluster-wide one")
}

func TestNewCacheOptions_ByObjectNamespacesNotEmpty(t *testing.T) {
	t.Parallel()

	oDH := map[string]cache.Config{"odh-ns": {}}
	secret := map[string]cache.Config{"odh-ns": {}, "ingress-ns": {}}
	opts := newCacheOptions(runtime.NewScheme(), oDH, secret)

	for key, byObj := range opts.ByObject {
		typeName := reflect.TypeOf(key).Elem().Name()
		require.NotEmpty(t, byObj.Namespaces,
			"%s ByObject entry must have explicit Namespaces to prevent cluster-wide caching", typeName)
	}
}
