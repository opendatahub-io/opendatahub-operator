package main

import (
	"reflect"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNewCacheOptions_ReaderFailOnMissingInformer(t *testing.T) {
	t.Parallel()

	opts := newCacheOptions(runtime.NewScheme(), nil, nil)
	assert.True(t, opts.ReaderFailOnMissingInformer,
		"ReaderFailOnMissingInformer must be true to prevent silent cluster-wide informer creation")
}

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

func TestNewCacheOptions_ByObjectContainsExpectedTypes(t *testing.T) {
	t.Parallel()

	expected := []client.Object{
		&corev1.Secret{},
		&corev1.ConfigMap{},
		&appsv1.Deployment{},
		&networkingv1.NetworkPolicy{},
		&rbacv1.Role{},
		&rbacv1.RoleBinding{},
		&corev1.ServiceAccount{},
		&corev1.Service{},
		&corev1.PersistentVolumeClaim{},
	}

	opts := newCacheOptions(runtime.NewScheme(), nil, nil)

	for _, obj := range expected {
		typeName := reflect.TypeOf(obj).Elem().Name()
		found := false
		for key := range opts.ByObject {
			if reflect.TypeOf(key) == reflect.TypeOf(obj) {
				found = true
				break
			}
		}
		assert.True(t, found,
			"%s must be in ByObject to prevent cluster-wide informer creation", typeName)
	}
}

func TestCacheDisableFor_ContainsExpectedTypes(t *testing.T) {
	t.Parallel()

	expected := []client.Object{
		&configv1.Infrastructure{},
		&ofapiv1alpha1.Subscription{},
		&authorizationv1.SelfSubjectRulesReview{},
		&corev1.Pod{},
		&corev1.Node{},
		&ofapiv1alpha1.CatalogSource{},
		&ofapiv1alpha1.ClusterServiceVersion{},
	}

	disabled := cacheDisableFor()

	for _, obj := range expected {
		typeName := reflect.TypeOf(obj).Elem().Name()
		found := false
		for _, d := range disabled {
			if reflect.TypeOf(d) == reflect.TypeOf(obj) {
				found = true
				break
			}
		}
		assert.True(t, found,
			"%s must be in DisableFor — it has no informer and would fail with ReaderFailOnMissingInformer", typeName)
	}
}

func TestNewCacheOptions_ByObjectNamespacesNotEmpty(t *testing.T) {
	t.Parallel()

	namespaces := map[string]cache.Config{"test-ns": {}}
	opts := newCacheOptions(runtime.NewScheme(), namespaces, namespaces)

	for key, byObj := range opts.ByObject {
		typeName := reflect.TypeOf(key).Elem().Name()
		require.NotEmpty(t, byObj.Namespaces,
			"%s ByObject entry must have explicit Namespaces to prevent cluster-wide caching", typeName)
	}
}
