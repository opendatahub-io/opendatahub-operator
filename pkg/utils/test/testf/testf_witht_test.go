package testf_test

import (
	"testing"
	"time"

	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

func TestGet(t *testing.T) {
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

	cl, err := fakeclient.New(&cm)
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
}

func TestList(t *testing.T) {
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

	cl, err := fakeclient.New(&cm)
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

func TestUpdate(t *testing.T) {
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

	cl, err := fakeclient.New(&cm)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cl).ShouldNot(BeNil())

	tc, err := testf.NewTestContext(testf.WithClient(cl))
	g.Expect(err).ShouldNot(HaveOccurred())

	matchMetadataAndData := And(
		jq.Match(`.metadata.namespace == "%s"`, cm.Namespace),
		jq.Match(`.metadata.name == "%s"`, cm.Name),
		jq.Match(`.data.foo == "%s"`, cm.Name),
	)

	key := client.ObjectKeyFromObject(&cm)
	transformer := testf.Transform(`.data.foo = "%s"`, cm.Name)

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
