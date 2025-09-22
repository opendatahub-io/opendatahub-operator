package deploy_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/api/pkg/lib/version"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/xid"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

func TestDeployWithCacheAction(t *testing.T) {
	g := NewWithT(t)
	s := runtime.NewScheme()

	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(apiextensionsv1.AddToScheme(s))
	utilruntime.Must(componentApi.AddToScheme(s))

	projectDir, err := envtestutil.FindProjectRoot()
	g.Expect(err).NotTo(HaveOccurred())

	envTest := &envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme: s,
			Paths: []string{
				filepath.Join(projectDir, "config", "crd", "bases"),
			},
			ErrorIfPathMissing: true,
			CleanUpAfterUse:    false,
		},
	}

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	cfg, err := envTest.Start()
	g.Expect(err).NotTo(HaveOccurred())

	cli, err := client.New(cfg, client.Options{Scheme: s})
	g.Expect(err).NotTo(HaveOccurred())

	t.Run("ExistingResource", func(t *testing.T) {
		testResourceNotReDeployed(
			t,
			cli,
			&corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gvk.ConfigMap.GroupVersion().String(),
					Kind:       gvk.ConfigMap.Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      xid.New().String(),
					Namespace: xid.New().String(),
				},
			},
			true)
	})

	t.Run("NonExistingResource", func(t *testing.T) {
		testResourceNotReDeployed(
			t,
			cli,
			&corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gvk.ConfigMap.GroupVersion().String(),
					Kind:       gvk.ConfigMap.Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      xid.New().String(),
					Namespace: xid.New().String(),
				},
			},
			false)
	})

	t.Run("CacheTTL", func(t *testing.T) {
		testCacheTTL(
			t,
			cli,
			&corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gvk.ConfigMap.GroupVersion().String(),
					Kind:       gvk.ConfigMap.Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      xid.New().String(),
					Namespace: xid.New().String(),
				},
			})
	})

	t.Run("DeletionTimestampBypassesCache", func(t *testing.T) {
		testDeletionTimestampBypassesCache(
			t,
			cli,
			&appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      xid.New().String(),
					Namespace: xid.New().String(),
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: func(i int32) *int32 { return &i }(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test-container",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			})
	})

	t.Run("CacheCleanupOnDeletionTimestamp", func(t *testing.T) {
		testCacheCleanupVerification(
			t,
			cli,
			&corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gvk.ConfigMap.GroupVersion().String(),
					Kind:       gvk.ConfigMap.Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      xid.New().String(),
					Namespace: xid.New().String(),
				},
				Data: map[string]string{
					"key": "value",
				},
			})
	})
}

func testResourceNotReDeployed(t *testing.T, cli client.Client, obj client.Object, create bool) {
	t.Helper()

	g := NewWithT(t)
	ctx := t.Context()

	in, err := resources.ToUnstructured(obj)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cli.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: in.GetNamespace(),
		},
	})

	g.Expect(err).ShouldNot(HaveOccurred())

	if create {
		err = cli.Create(ctx, in.DeepCopy())
		g.Expect(err).ShouldNot(HaveOccurred())
	}

	rr := types.ReconciliationRequest{
		Client: cli,
		DSCI: &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: in.GetNamespace()},
		},
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		},
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}}},
		Resources: []unstructured.Unstructured{
			*in.DeepCopy(),
		},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", mock.Anything).Return(false)
		}),
	}

	action := deploy.NewAction(
		deploy.WithCache(),
		deploy.WithMode(deploy.ModeSSA),
		deploy.WithFieldOwner(xid.New().String()),
	)

	deploy.DeployedResourcesTotal.Reset()

	// Resource should be created if missing
	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(testutil.ToFloat64(deploy.DeployedResourcesTotal)).Should(Equal(float64(1)))

	out1 := unstructured.Unstructured{}
	out1.SetGroupVersionKind(in.GroupVersionKind())

	err = cli.Get(ctx, client.ObjectKeyFromObject(in), &out1)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Resource should not be re-deployed
	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(testutil.ToFloat64(deploy.DeployedResourcesTotal)).Should(Equal(float64(1)))

	out2 := unstructured.Unstructured{}
	out2.SetGroupVersionKind(in.GroupVersionKind())

	err = cli.Get(ctx, client.ObjectKeyFromObject(in), &out2)
	g.Expect(err).ShouldNot(HaveOccurred())

	// check that the resource version has not changed
	g.Expect(out1.GetResourceVersion()).Should(Equal(out2.GetResourceVersion()))
}

func testCacheTTL(t *testing.T, cli client.Client, obj client.Object) {
	t.Helper()

	g := NewWithT(t)
	ctx := t.Context()

	in, err := resources.ToUnstructured(obj)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cli.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: in.GetNamespace(),
		},
	})

	g.Expect(err).ShouldNot(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client: cli,
		DSCI: &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: in.GetNamespace()},
		},
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		},
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}}},
		Resources: []unstructured.Unstructured{
			*in.DeepCopy(),
		},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", mock.Anything).Return(false)
		}),
	}

	ttl := 1 * time.Second

	action := deploy.NewAction(
		deploy.WithCache(deploy.WithTTL(ttl)),
		deploy.WithMode(deploy.ModeSSA),
		deploy.WithFieldOwner(xid.New().String()),
	)

	deploy.DeployedResourcesTotal.Reset()

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(testutil.ToFloat64(deploy.DeployedResourcesTotal)).Should(BeNumerically("==", 1))

	g.Eventually(func() (float64, error) {
		if err := action(ctx, &rr); err != nil {
			return 0, err
		}

		return testutil.ToFloat64(deploy.DeployedResourcesTotal), nil
	}).WithTimeout(5 * ttl).WithPolling(2 * ttl).Should(
		BeNumerically("==", 2),
	)
}

func testDeletionTimestampBypassesCache(t *testing.T, cli client.Client, obj client.Object) {
	t.Helper()

	g := NewWithT(t)
	ctx := t.Context()

	in, err := resources.ToUnstructured(obj)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cli.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: in.GetNamespace(),
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client: cli,
		DSCI: &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: in.GetNamespace()},
		},
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		},
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}}},
		Resources: []unstructured.Unstructured{
			*in.DeepCopy(),
		},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", mock.Anything).Return(false)
		}),
	}

	action := deploy.NewAction(
		deploy.WithCache(),
		deploy.WithMode(deploy.ModeSSA),
		deploy.WithFieldOwner(xid.New().String()),
	)

	deploy.DeployedResourcesTotal.Reset()

	// First deployment should create the resource and cache it
	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(testutil.ToFloat64(deploy.DeployedResourcesTotal)).Should(Equal(float64(1)))

	// Second deployment should be skipped due to cache hit
	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(testutil.ToFloat64(deploy.DeployedResourcesTotal)).Should(Equal(float64(1)))

	// Get the current object from the cluster
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(in.GroupVersionKind())
	err = cli.Get(ctx, client.ObjectKeyFromObject(in), current)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Simulate deletion scenario by setting deletionTimestamp
	// Note: This simulates the state when reconciliation is triggered for a deleting object
	now := metav1.NewTime(time.Now())
	current.SetDeletionTimestamp(&now)

	// Update reconciliation request to use the object with deletionTimestamp
	rr.Resources[0] = *current

	// Test the fix: objects with deletionTimestamp should bypass cache
	// Expected behavior: cache is bypassed and deployment proceeds
	// In our test environment, this will result in a validation error because
	// deletionTimestamp is immutable, but this proves cache bypass worked
	err = action(ctx, &rr)

	// Verify that cache bypass occurred by checking for the expected validation error
	g.Expect(err).Should(HaveOccurred())
	g.Expect(k8serr.IsInvalid(err)).Should(BeTrue(), "expected validation error for deletionTimestamp field")
	g.Expect(err.Error()).Should(ContainSubstring("deletionTimestamp"), "error should mention deletionTimestamp field")
}

func testCacheCleanupVerification(t *testing.T, cli client.Client, obj client.Object) {
	t.Helper()
	g := NewWithT(t)
	ctx := t.Context()

	// Convert the test object to unstructured
	in, err := resources.ToUnstructured(obj)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create namespace for the test
	err = cli.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: in.GetNamespace(),
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create the object in the cluster to get proper metadata
	err = cli.Create(ctx, obj)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Get the current object from the cluster to have proper metadata
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(in.GroupVersionKind())
	err = cli.Get(ctx, client.ObjectKeyFromObject(in), current)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create the cache that we'll inspect directly
	cache := deploy.NewCache()

	// Add object to cache to simulate it being cached
	err = cache.Add(current, in)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify object is in cache BEFORE deletion timestamp
	cached, err := cache.Has(current, in)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cached).Should(BeTrue(), "Object should be cached before deletion timestamp")

	// Also verify behavior before deletion: cached object should be skipped
	shouldSkipBefore, err := cache.ProcessCacheEntry(current, in)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(shouldSkipBefore).Should(BeTrue(), "Should skip when object is cached and not being deleted")

	// Set deletion timestamp to simulate stuck deletion
	now := metav1.NewTime(time.Now())
	currentWithDeletion := current.DeepCopy()
	currentWithDeletion.SetDeletionTimestamp(&now)

	// Test CheckAndCleanup with deletion timestamp - this should clean up the cache
	shouldSkip, err := cache.ProcessCacheEntry(currentWithDeletion, in)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(shouldSkip).Should(BeFalse(), "Should not skip for objects with deletionTimestamp")

	// Verify object is NOT in cache AFTER deletion timestamp bypass (cache cleanup occurred)
	cached, err = cache.Has(currentWithDeletion, in)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cached).Should(BeFalse(), "Object should be removed from cache after deletion timestamp bypass")

	// Also verify with original object (without deletion timestamp) - should also be cleaned up
	cached, err = cache.Has(current, in)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cached).Should(BeFalse(), "Cache entry should be cleaned up completely")
}
