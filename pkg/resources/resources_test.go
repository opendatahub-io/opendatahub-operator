package resources_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega/gstruct"
	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhCli "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

func TestHasAnnotationAndLabels(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]string
		key      string
		values   []string
		expected bool
	}{
		{"nil object", nil, "key1", []string{"val1"}, false},
		{"no metadata", map[string]string{}, "key1", []string{"val1"}, false},
		{"metadata exists and value matches", map[string]string{"key1": "val1"}, "key1", []string{"val1"}, true},
		{"metadata exists and value doesn't match", map[string]string{"key1": "val2"}, "key1", []string{"val1"}, false},
		{"metadata exists and value in list", map[string]string{"key1": "val2"}, "key1", []string{"val1", "val2"}, true},
		{"metadata exists and key doesn't match", map[string]string{"key2": "val1"}, "key1", []string{"val1"}, false},
		{"multiple values and no match", map[string]string{"key1": "val3"}, "key1", []string{"val1", "val2"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("annotations_"+tt.name, func(t *testing.T) {
				g := NewWithT(t)

				obj := unstructured.Unstructured{}
				if len(tt.data) != 0 {
					obj.SetAnnotations(tt.data)
				}

				result := resources.HasAnnotation(&obj, tt.key, tt.values...)

				g.Expect(result).To(Equal(tt.expected))
			})

			t.Run("labels_"+tt.name, func(t *testing.T) {
				g := NewWithT(t)

				obj := unstructured.Unstructured{}
				if len(tt.data) != 0 {
					obj.SetLabels(tt.data)
				}

				result := resources.HasLabel(&obj, tt.key, tt.values...)

				g.Expect(result).To(Equal(tt.expected))
			})
		})
	}
}

func TestGetGroupVersionKindForObject(t *testing.T) {
	g := NewWithT(t)

	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(appsv1.AddToScheme(scheme)).To(Succeed())

	t.Run("ObjectWithGVK", func(t *testing.T) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk.Deployment)

		gotGVK, err := resources.GetGroupVersionKindForObject(scheme, obj)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(gotGVK).To(Equal(gvk.Deployment))
	})

	t.Run("ObjectWithoutGVK_SuccessfulLookup", func(t *testing.T) {
		obj := &appsv1.Deployment{}

		gotGVK, err := resources.GetGroupVersionKindForObject(scheme, obj)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(gotGVK).To(Equal(gvk.Deployment))
	})

	t.Run("ObjectWithoutGVK_ErrorInLookup", func(t *testing.T) {
		obj := &unstructured.Unstructured{}

		_, err := resources.GetGroupVersionKindForObject(scheme, obj)
		g.Expect(err).To(WithTransform(
			errors.Unwrap,
			MatchError(runtime.IsMissingKind, "IsMissingKind"),
		))
	})

	t.Run("NilObject", func(t *testing.T) {
		_, err := resources.GetGroupVersionKindForObject(scheme, nil)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("nil object"))
	})
}

func TestEnsureGroupVersionKind(t *testing.T) {
	g := NewWithT(t)

	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(appsv1.AddToScheme(scheme)).To(Succeed())

	t.Run("ForObject", func(t *testing.T) {
		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion(gvk.Deployment.GroupVersion().String())
		obj.SetKind(gvk.Deployment.Kind)

		err := resources.EnsureGroupVersionKind(scheme, obj)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(obj.GetObjectKind().GroupVersionKind()).To(Equal(gvk.Deployment))
	})

	t.Run("ErrorOnNilObject", func(t *testing.T) {
		err := resources.EnsureGroupVersionKind(scheme, nil)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("nil object"))
	})

	t.Run("ErrorOnInvalidObject", func(t *testing.T) {
		obj := &unstructured.Unstructured{}
		obj.SetKind("UnknownKind")

		err := resources.EnsureGroupVersionKind(scheme, obj)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("failed to get GVK"))
	})
}

func TestRemoveOwnerRef(t *testing.T) {
	g := NewWithT(t)
	s := runtime.NewScheme()

	ctx := context.Background()
	ns := xid.New().String()

	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(apiextensionsv1.AddToScheme(s))
	utilruntime.Must(componentApi.AddToScheme(s))
	utilruntime.Must(dsciv1.AddToScheme(s))
	utilruntime.Must(dscv1.AddToScheme(s))
	utilruntime.Must(rbacv1.AddToScheme(s))

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

	envTestClient, err := client.New(cfg, client.Options{Scheme: s})
	g.Expect(err).NotTo(HaveOccurred())

	cli, err := odhCli.NewFromConfig(cfg, envTestClient)
	g.Expect(err).NotTo(HaveOccurred())

	err = cli.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	g.Expect(err).ToNot(HaveOccurred())

	cm1 := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm1", Namespace: ns}}
	cm1.SetGroupVersionKind(gvk.ConfigMap)

	err = cli.Create(ctx, cm1)
	g.Expect(err).ToNot(HaveOccurred())

	cm2 := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm2", Namespace: ns}}
	cm2.SetGroupVersionKind(gvk.ConfigMap)

	err = cli.Create(ctx, cm2)
	g.Expect(err).ToNot(HaveOccurred())

	// Create a ConfigMap with OwnerReferences
	configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test-configmap", Namespace: ns}}

	err = controllerutil.SetOwnerReference(cm1, configMap, s)
	g.Expect(err).ToNot(HaveOccurred())
	err = controllerutil.SetOwnerReference(cm2, configMap, s)
	g.Expect(err).ToNot(HaveOccurred())

	err = cli.Create(ctx, configMap)
	g.Expect(err).ToNot(HaveOccurred())

	predicate := func(ref metav1.OwnerReference) bool {
		return ref.Name == cm1.Name
	}

	err = resources.RemoveOwnerReferences(ctx, cli, configMap, predicate)
	g.Expect(err).ToNot(HaveOccurred())

	updatedConfigMap := &corev1.ConfigMap{}
	err = cli.Get(ctx, client.ObjectKeyFromObject(configMap), updatedConfigMap)
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(updatedConfigMap.GetOwnerReferences()).Should(And(
		HaveLen(1),
		HaveEach(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Name":       Equal(cm2.Name),
			"APIVersion": Equal(gvk.ConfigMap.GroupVersion().String()),
			"Kind":       Equal(gvk.ConfigMap.Kind),
			"UID":        Equal(cm2.UID),
		})),
	))
}
