package testf_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/onsi/gomega/types"
	"github.com/rs/xid"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

// Common test helpers to reduce duplication across test functions

// prepareTestConfigMap creates a unique ConfigMap and returns a matcher for it along with the unstructured object and key.
// This is used for tests that create new ConfigMaps (e.g., TestCreate).
//
//nolint:ireturn
func prepareTestConfigMap(g Gomega, template corev1.ConfigMap) (types.GomegaMatcher, *unstructured.Unstructured, client.ObjectKey) {
	cm := template.DeepCopy()
	cm.Name = xid.New().String()
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data["foo"] = "bar"

	matcher := And(
		jq.Match(`.metadata.namespace == "%s"`, cm.Namespace),
		jq.Match(`.metadata.name == "%s"`, cm.Name),
		jq.Match(`.data.foo == "bar"`),
	)

	key := client.ObjectKeyFromObject(cm)
	obj, err := resources.ToUnstructured(cm)
	g.Expect(err).ShouldNot(HaveOccurred())

	return matcher, obj, key
}

// prepareUpdateTestConfigMap creates a ConfigMap from template and returns an unstructured object for update tests.
// The matcher expects the transformer to have been applied.
//
//nolint:ireturn
func prepareUpdateTestConfigMap(g Gomega, template corev1.ConfigMap, expectedMatcher types.GomegaMatcher) (*unstructured.Unstructured, types.GomegaMatcher) {
	obj, err := resources.ToUnstructured(template.DeepCopy())
	g.Expect(err).ShouldNot(HaveOccurred())
	return obj, expectedMatcher
}

// prepareExistingConfigMapTest creates a ConfigMap with pre-existing client and returns test setup for Update/Patch tests.
// The ConfigMap already exists in the fake client, and the test applies a transformer to it.
//
//nolint:ireturn
func prepareExistingConfigMapTest(g Gomega, cmName string) (types.GomegaMatcher, client.ObjectKey, testf.TransformFn, *testf.TestContext) {
	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      cmName,
		},
	}

	cl, err := fakeclient.New(fakeclient.WithObjects(&cm))
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cl).ShouldNot(BeNil())

	tc, err := testf.NewTestContext(testf.WithClient(cl))
	g.Expect(err).ShouldNot(HaveOccurred())

	matcher := And(
		jq.Match(`.metadata.namespace == "%s"`, cm.Namespace),
		jq.Match(`.metadata.name == "%s"`, cm.Name),
		jq.Match(`.data.foo == "%s"`, cm.Name),
	)

	key := client.ObjectKeyFromObject(&cm)
	transformer := testf.Transform(`.data.foo = "%s"`, cm.Name)

	return matcher, key, transformer, tc
}

//nolint:dupl
func TestEventuallyValueTimeout(t *testing.T) {
	g := NewWithT(t)

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cl).ShouldNot(BeNil())

	failureMsg := ""
	timeout := 2 * time.Second

	tc, err := testf.NewTestContext(
		testf.WithClient(cl),
		testf.WithTOptions(testf.WithEventuallyTimeout(timeout)),
		testf.WithTOptions(testf.WithFailHandler(func(message string, callerSkip ...int) {
			failureMsg = message
		})),
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	key := client.ObjectKey{Name: "foo", Namespace: "bar"}

	_ = tc.NewWithT(t).Get(gvk.ConfigMap, key).Eventually().ShouldNot(BeNil())

	assert.Contains(t, failureMsg, fmt.Sprintf("Timed out after %d.", int(timeout.Seconds())))
}

//nolint:dupl
func TestEventuallyErrTimeout(t *testing.T) {
	g := NewWithT(t)

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cl).ShouldNot(BeNil())

	failureMsg := ""
	timeout := 3 * time.Second

	tc, err := testf.NewTestContext(
		testf.WithClient(cl),
		testf.WithTOptions(testf.WithEventuallyTimeout(timeout)),
		testf.WithTOptions(testf.WithFailHandler(func(message string, callerSkip ...int) {
			failureMsg = message
		})),
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	key := client.ObjectKey{Name: "foo", Namespace: "bar"}

	_ = tc.NewWithT(t).Delete(gvk.ConfigMap, key).Eventually().ShouldNot(Succeed())

	assert.Contains(t, failureMsg, fmt.Sprintf("Timed out after %d.", int(timeout.Seconds())))
}

func TestGet(t *testing.T) {
	g := NewWithT(t)

	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      xid.New().String(),
		},
	}

	cl, err := fakeclient.New(fakeclient.WithObjects(&cm))
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cl).ShouldNot(BeNil())

	tc, err := testf.NewTestContext(testf.WithClient(cl))
	g.Expect(err).ShouldNot(HaveOccurred())

	key := client.ObjectKeyFromObject(&cm)

	matchMetadata := And(
		jq.Match(`.metadata.namespace == "%s"`, cm.Namespace),
		jq.Match(`.metadata.name == "%s"`, cm.Name),
	)

	t.Run("Get", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v, err := wt.Get(gvk.ConfigMap, key).Get()

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(v).Should(matchMetadata)
	})

	t.Run("Eventually", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v := wt.Get(gvk.ConfigMap, key).Eventually().Should(matchMetadata)
		g.Expect(v).ShouldNot(BeNil())
	})

	t.Run("Eventually Succeed", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v := wt.Get(gvk.ConfigMap, key).Eventually().Should(Succeed())
		g.Expect(v).ShouldNot(BeNil())
	})

	t.Run("Consistently", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v := wt.Get(gvk.ConfigMap, key).Consistently().WithTimeout(1 * time.Second).Should(matchMetadata)
		g.Expect(v).ShouldNot(BeNil())
	})

	t.Run("Consistently Succeed", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v := wt.Get(gvk.ConfigMap, key).Consistently().WithTimeout(1 * time.Second).Should(Succeed())
		g.Expect(v).ShouldNot(BeNil())
	})

	t.Run("Get Not Found", func(t *testing.T) {
		wt := tc.NewWithT(t)

		key := client.ObjectKey{Namespace: "ns", Name: "name"}

		v, err := wt.Get(gvk.ConfigMap, key).Get()
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(v).Should(BeNil())
	})

	t.Run("Eventually Not Found", func(t *testing.T) {
		wt := tc.NewWithT(t)

		key := client.ObjectKey{Namespace: "ns", Name: "name"}

		v := wt.Get(gvk.ConfigMap, key).Eventually().WithTimeout(1 * time.Second).ShouldNot(matchMetadata)
		g.Expect(v).Should(BeNil())
	})
}

func TestList(t *testing.T) {
	g := NewWithT(t)

	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      xid.New().String(),
		},
	}

	cl, err := fakeclient.New(fakeclient.WithObjects(&cm))
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cl).ShouldNot(BeNil())

	tc, err := testf.NewTestContext(testf.WithClient(cl))
	g.Expect(err).ShouldNot(HaveOccurred())

	matchMetadata := And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata.namespace == "%s"`, cm.Namespace),
			jq.Match(`.metadata.name == "%s"`, cm.Name),
		)),
	)

	t.Run("Get", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v, err := wt.List(gvk.ConfigMap).Get()

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(v).Should(matchMetadata)
	})

	t.Run("Eventually", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v := wt.List(gvk.ConfigMap).Eventually().Should(matchMetadata)
		g.Expect(v).ShouldNot(BeNil())
	})

	t.Run("Eventually Succeed", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v := wt.List(gvk.ConfigMap).Eventually().Should(Succeed())
		g.Expect(v).ShouldNot(BeNil())
	})

	t.Run("Consistently", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v := wt.List(gvk.ConfigMap).Consistently().WithTimeout(1 * time.Second).Should(matchMetadata)
		g.Expect(v).ShouldNot(BeNil())
	})

	t.Run("Consistently Succeed", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v := wt.List(gvk.ConfigMap).Consistently().WithTimeout(1 * time.Second).Should(Succeed())
		g.Expect(v).ShouldNot(BeNil())
	})
}

//nolint:dupl // TestUpdate and TestPatch have similar structure by design
func TestUpdate(t *testing.T) {
	g := NewWithT(t)

	matchMetadataAndData, key, transformer, tc := prepareExistingConfigMapTest(g, xid.New().String())

	t.Run("Get", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v, err := wt.Update(gvk.ConfigMap, key, transformer).Get()

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(v).Should(matchMetadataAndData)
	})

	t.Run("Eventually", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v := wt.Update(gvk.ConfigMap, key, transformer).Eventually().Should(matchMetadataAndData)
		g.Expect(v).Should(matchMetadataAndData)
	})

	t.Run("Eventually Succeed", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v := wt.Update(gvk.ConfigMap, key, transformer).Eventually().Should(Succeed())
		g.Expect(v).Should(matchMetadataAndData)
	})

	t.Run("Consistently", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v := wt.Update(gvk.ConfigMap, key, transformer).Consistently().WithTimeout(1 * time.Second).Should(matchMetadataAndData)
		g.Expect(v).Should(matchMetadataAndData)
	})

	t.Run("Consistently Succeed", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v := wt.Update(gvk.ConfigMap, key, transformer).Consistently().WithTimeout(1 * time.Second).Should(Succeed())
		g.Expect(v).Should(matchMetadataAndData)
	})
}

//nolint:dupl // TestUpdate and TestPatch have similar structure by design
func TestPatch(t *testing.T) {
	g := NewWithT(t)

	matchMetadataAndData, key, transformer, tc := prepareExistingConfigMapTest(g, xid.New().String())

	t.Run("Get", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v, err := wt.Patch(gvk.ConfigMap, key, transformer).Get()

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(v).Should(matchMetadataAndData)
	})

	t.Run("Eventually", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v := wt.Patch(gvk.ConfigMap, key, transformer).Eventually().Should(matchMetadataAndData)
		g.Expect(v).Should(matchMetadataAndData)
	})

	t.Run("Eventually Succeed", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v := wt.Patch(gvk.ConfigMap, key, transformer).Eventually().Should(Succeed())
		g.Expect(v).Should(matchMetadataAndData)
	})

	t.Run("Consistently", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v := wt.Patch(gvk.ConfigMap, key, transformer).Consistently().WithTimeout(1 * time.Second).Should(matchMetadataAndData)
		g.Expect(v).Should(matchMetadataAndData)
	})

	t.Run("Consistently Succeed", func(t *testing.T) {
		wt := tc.NewWithT(t)

		v := wt.Patch(gvk.ConfigMap, key, transformer).Consistently().WithTimeout(1 * time.Second).Should(Succeed())
		g.Expect(v).Should(matchMetadataAndData)
	})
}

func TestCreate(t *testing.T) {
	g := NewWithT(t)

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cl).ShouldNot(BeNil())

	tc, err := testf.NewTestContext(testf.WithClient(cl))
	g.Expect(err).ShouldNot(HaveOccurred())

	// Shared setup - ConfigMap template for all subtests
	cmTemplate := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gvk.ConfigMap.GroupVersion().String(),
			Kind:       gvk.ConfigMap.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
		},
		Data: map[string]string{
			"foo": "bar",
		},
	}

	t.Run("Get", func(t *testing.T) {
		wt := tc.NewWithT(t)

		matcher, obj, key := prepareTestConfigMap(g, cmTemplate)

		v, err := wt.Create(obj, key).Get()

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(v).Should(matcher)
	})

	t.Run("Eventually", func(t *testing.T) {
		wt := tc.NewWithT(t)

		matcher, obj, key := prepareTestConfigMap(g, cmTemplate)

		v := wt.Create(obj, key).Eventually().Should(matcher)
		g.Expect(v).Should(matcher)
	})

	t.Run("Eventually Succeed", func(t *testing.T) {
		wt := tc.NewWithT(t)

		matcher, obj, key := prepareTestConfigMap(g, cmTemplate)

		v := wt.Create(obj, key).Eventually().Should(Succeed())
		g.Expect(v).Should(matcher)
	})

	t.Run("Consistently", func(t *testing.T) {
		wt := tc.NewWithT(t)

		matcher, obj, key := prepareTestConfigMap(g, cmTemplate)

		v := wt.Create(obj, key).Consistently().WithTimeout(1 * time.Second).Should(matcher)
		g.Expect(v).Should(matcher)
	})

	t.Run("Consistently Succeed", func(t *testing.T) {
		wt := tc.NewWithT(t)

		matcher, obj, key := prepareTestConfigMap(g, cmTemplate)

		v := wt.Create(obj, key).Consistently().WithTimeout(1 * time.Second).Should(Succeed())
		g.Expect(v).Should(matcher)
	})
}

//nolint:dupl // TestCreateOrUpdate and TestCreateOrPatch have similar structure by design
func TestCreateOrUpdate(t *testing.T) {
	g := NewWithT(t)

	cm := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gvk.ConfigMap.GroupVersion().String(),
			Kind:       gvk.ConfigMap.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      xid.New().String(),
		},
		Data: map[string]string{
			"initial": "value",
		},
	}

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cl).ShouldNot(BeNil())

	tc, err := testf.NewTestContext(testf.WithClient(cl))
	g.Expect(err).ShouldNot(HaveOccurred())

	matchMetadataAndData := And(
		jq.Match(`.metadata.namespace == "%s"`, cm.Namespace),
		jq.Match(`.metadata.name == "%s"`, cm.Name),
		jq.Match(`.data.updated == "true"`),
	)

	transformer := testf.Transform(`.data.updated = "true"`)

	t.Run("Get", func(t *testing.T) {
		wt := tc.NewWithT(t)

		obj, matcher := prepareUpdateTestConfigMap(g, cm, matchMetadataAndData)

		v, err := wt.CreateOrUpdate(obj, transformer).Get()

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(v).Should(matcher)
	})

	t.Run("Eventually", func(t *testing.T) {
		wt := tc.NewWithT(t)

		obj, matcher := prepareUpdateTestConfigMap(g, cm, matchMetadataAndData)

		v := wt.CreateOrUpdate(obj, transformer).Eventually().Should(matcher)
		g.Expect(v).Should(matcher)
	})

	t.Run("Eventually Succeed", func(t *testing.T) {
		wt := tc.NewWithT(t)

		obj, matcher := prepareUpdateTestConfigMap(g, cm, matchMetadataAndData)

		v := wt.CreateOrUpdate(obj, transformer).Eventually().Should(Succeed())
		g.Expect(v).Should(matcher)
	})

	t.Run("Consistently", func(t *testing.T) {
		wt := tc.NewWithT(t)

		obj, matcher := prepareUpdateTestConfigMap(g, cm, matchMetadataAndData)

		v := wt.CreateOrUpdate(obj, transformer).Consistently().WithTimeout(1 * time.Second).Should(matcher)
		g.Expect(v).Should(matcher)
	})

	t.Run("Consistently Succeed", func(t *testing.T) {
		wt := tc.NewWithT(t)

		obj, matcher := prepareUpdateTestConfigMap(g, cm, matchMetadataAndData)

		v := wt.CreateOrUpdate(obj, transformer).Consistently().WithTimeout(1 * time.Second).Should(Succeed())
		g.Expect(v).Should(matcher)
	})
}

//nolint:dupl // TestCreateOrUpdate and TestCreateOrPatch have similar structure by design
func TestCreateOrPatch(t *testing.T) {
	g := NewWithT(t)

	cm := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gvk.ConfigMap.GroupVersion().String(),
			Kind:       gvk.ConfigMap.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      xid.New().String(),
		},
		Data: map[string]string{
			"initial": "value",
		},
	}

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cl).ShouldNot(BeNil())

	tc, err := testf.NewTestContext(testf.WithClient(cl))
	g.Expect(err).ShouldNot(HaveOccurred())

	matchMetadataAndData := And(
		jq.Match(`.metadata.namespace == "%s"`, cm.Namespace),
		jq.Match(`.metadata.name == "%s"`, cm.Name),
		jq.Match(`.data.patched == "true"`),
	)

	transformer := testf.Transform(`.data.patched = "true"`)

	t.Run("Get", func(t *testing.T) {
		wt := tc.NewWithT(t)

		obj, matcher := prepareUpdateTestConfigMap(g, cm, matchMetadataAndData)

		v, err := wt.CreateOrPatch(obj, transformer).Get()

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(v).Should(matcher)
	})

	t.Run("Eventually", func(t *testing.T) {
		wt := tc.NewWithT(t)

		obj, matcher := prepareUpdateTestConfigMap(g, cm, matchMetadataAndData)

		v := wt.CreateOrPatch(obj, transformer).Eventually().Should(matcher)
		g.Expect(v).Should(matcher)
	})

	t.Run("Eventually Succeed", func(t *testing.T) {
		wt := tc.NewWithT(t)

		obj, matcher := prepareUpdateTestConfigMap(g, cm, matchMetadataAndData)

		v := wt.CreateOrPatch(obj, transformer).Eventually().Should(Succeed())
		g.Expect(v).Should(matcher)
	})

	t.Run("Consistently", func(t *testing.T) {
		wt := tc.NewWithT(t)

		obj, matcher := prepareUpdateTestConfigMap(g, cm, matchMetadataAndData)

		v := wt.CreateOrPatch(obj, transformer).Consistently().WithTimeout(1 * time.Second).Should(matcher)
		g.Expect(v).Should(matcher)
	})

	t.Run("Consistently Succeed", func(t *testing.T) {
		wt := tc.NewWithT(t)

		obj, matcher := prepareUpdateTestConfigMap(g, cm, matchMetadataAndData)

		v := wt.CreateOrPatch(obj, transformer).Consistently().WithTimeout(1 * time.Second).Should(Succeed())
		g.Expect(v).Should(matcher)
	})
}

func TestDeleteAll(t *testing.T) {
	g := NewWithT(t)

	cm1 := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gvk.ConfigMap.GroupVersion().String(),
			Kind:       gvk.ConfigMap.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      xid.New().String(),
			Labels: map[string]string{
				"test": "deleteall",
			},
		},
	}

	cm2 := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gvk.ConfigMap.GroupVersion().String(),
			Kind:       gvk.ConfigMap.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      xid.New().String(),
			Labels: map[string]string{
				"test": "deleteall",
			},
		},
	}

	cl, err := fakeclient.New(fakeclient.WithObjects(&cm1, &cm2))
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cl).ShouldNot(BeNil())

	tc, err := testf.NewTestContext(testf.WithClient(cl))
	g.Expect(err).ShouldNot(HaveOccurred())

	listOpts := []client.ListOption{
		client.InNamespace("default"),
		client.MatchingLabels(map[string]string{"test": "deleteall"}),
	}

	deleteOpts := []client.DeleteAllOfOption{
		client.InNamespace("default"),
		client.MatchingLabels(map[string]string{"test": "deleteall"}),
	}

	t.Run("Get", func(t *testing.T) {
		wt := tc.NewWithT(t)

		err := wt.DeleteAll(gvk.ConfigMap, deleteOpts...).Get()
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify all matching resources are deleted
		wt.List(gvk.ConfigMap, listOpts...).Eventually().Should(BeEmpty())
	})

	t.Run("Eventually", func(t *testing.T) {
		wt := tc.NewWithT(t)

		// Re-create the resources for this test (clear resourceVersion)
		newCm1 := cm1.DeepCopy()
		newCm1.ResourceVersion = ""
		err := wt.Client().Create(wt.Context(), newCm1)
		g.Expect(err).ShouldNot(HaveOccurred())

		newCm2 := cm2.DeepCopy()
		newCm2.ResourceVersion = ""
		err = wt.Client().Create(wt.Context(), newCm2)
		g.Expect(err).ShouldNot(HaveOccurred())

		wt.DeleteAll(gvk.ConfigMap, deleteOpts...).Eventually().Should(Succeed())

		// Verify all matching resources are deleted
		wt.List(gvk.ConfigMap, listOpts...).Eventually().Should(BeEmpty())
	})

	t.Run("Eventually Succeed", func(t *testing.T) {
		wt := tc.NewWithT(t)

		// Re-create the resources for this test (clear resourceVersion)
		newCm1 := cm1.DeepCopy()
		newCm1.ResourceVersion = ""
		err := wt.Client().Create(wt.Context(), newCm1)
		g.Expect(err).ShouldNot(HaveOccurred())

		newCm2 := cm2.DeepCopy()
		newCm2.ResourceVersion = ""
		err = wt.Client().Create(wt.Context(), newCm2)
		g.Expect(err).ShouldNot(HaveOccurred())

		wt.DeleteAll(gvk.ConfigMap, deleteOpts...).Eventually().Should(Succeed())

		// Verify all matching resources are deleted
		wt.List(gvk.ConfigMap, listOpts...).Eventually().Should(BeEmpty())
	})

	t.Run("Consistently", func(t *testing.T) {
		wt := tc.NewWithT(t)

		// Re-create the resources for this test (clear resourceVersion)
		newCm1 := cm1.DeepCopy()
		newCm1.ResourceVersion = ""
		err := wt.Client().Create(wt.Context(), newCm1)
		g.Expect(err).ShouldNot(HaveOccurred())

		newCm2 := cm2.DeepCopy()
		newCm2.ResourceVersion = ""
		err = wt.Client().Create(wt.Context(), newCm2)
		g.Expect(err).ShouldNot(HaveOccurred())

		wt.DeleteAll(gvk.ConfigMap, deleteOpts...).Consistently().WithTimeout(1 * time.Second).Should(Succeed())

		// Verify all matching resources are deleted
		wt.List(gvk.ConfigMap, listOpts...).Eventually().Should(BeEmpty())
	})

	t.Run("Consistently Succeed", func(t *testing.T) {
		wt := tc.NewWithT(t)

		// Re-create the resources for this test (clear resourceVersion)
		newCm1 := cm1.DeepCopy()
		newCm1.ResourceVersion = ""
		err := wt.Client().Create(wt.Context(), newCm1)
		g.Expect(err).ShouldNot(HaveOccurred())

		newCm2 := cm2.DeepCopy()
		newCm2.ResourceVersion = ""
		err = wt.Client().Create(wt.Context(), newCm2)
		g.Expect(err).ShouldNot(HaveOccurred())

		wt.DeleteAll(gvk.ConfigMap, deleteOpts...).Consistently().WithTimeout(1 * time.Second).Should(Succeed())

		// Verify all matching resources are deleted
		wt.List(gvk.ConfigMap, listOpts...).Eventually().Should(BeEmpty())
	})
}

func TestDelete(t *testing.T) {
	g := NewWithT(t)

	cm := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gvk.ConfigMap.GroupVersion().String(),
			Kind:       gvk.ConfigMap.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      xid.New().String(),
		},
	}

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cl).ShouldNot(BeNil())

	tc, err := testf.NewTestContext(testf.WithClient(cl))
	g.Expect(err).ShouldNot(HaveOccurred())

	key := client.ObjectKeyFromObject(&cm)

	t.Run("Get", func(t *testing.T) {
		wt := tc.NewWithT(t)

		err := wt.Client().Create(wt.Context(), cm.DeepCopy())
		g.Expect(err).ShouldNot(HaveOccurred())

		err = wt.Delete(gvk.ConfigMap, key).Get()
		g.Expect(err).ShouldNot(HaveOccurred())

		wt.List(gvk.ConfigMap).Eventually().Should(BeEmpty())
	})

	t.Run("Eventually", func(t *testing.T) {
		wt := tc.NewWithT(t)

		err := wt.Client().Create(wt.Context(), cm.DeepCopy())
		g.Expect(err).ShouldNot(HaveOccurred())

		ok := wt.Delete(gvk.ConfigMap, key).Eventually().Should(Succeed())
		g.Expect(ok).Should(BeTrue())

		wt.List(gvk.ConfigMap).Eventually().Should(BeEmpty())
	})

	t.Run("Consistently", func(t *testing.T) {
		wt := tc.NewWithT(t)

		err := wt.Client().Create(wt.Context(), cm.DeepCopy())
		g.Expect(err).ShouldNot(HaveOccurred())

		ok := wt.Delete(gvk.ConfigMap, key).Consistently().WithTimeout(1 * time.Second).Should(Succeed())
		g.Expect(ok).Should(BeTrue())

		wt.List(gvk.ConfigMap).Eventually().Should(BeEmpty())
	})
}
