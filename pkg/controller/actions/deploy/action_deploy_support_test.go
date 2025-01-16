//nolint:testpackage
package deploy

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhCli "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

func TestIsLegacyOwnerRef(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	tests := []struct {
		name     string
		ownerRef metav1.OwnerReference
		matcher  types.GomegaMatcher
	}{
		{
			name: "Valid DataScienceCluster owner reference",
			ownerRef: metav1.OwnerReference{
				APIVersion: gvk.DataScienceCluster.GroupVersion().String(),
				Kind:       gvk.DataScienceCluster.Kind,
			},
			matcher: BeTrue(),
		},
		{
			name: "Valid DSCInitialization owner reference",
			ownerRef: metav1.OwnerReference{
				APIVersion: gvk.DSCInitialization.GroupVersion().String(),
				Kind:       gvk.DSCInitialization.Kind,
			},
			matcher: BeTrue(),
		},
		{
			name: "Invalid owner reference (different group)",
			ownerRef: metav1.OwnerReference{
				APIVersion: "othergroup/v1",
				Kind:       gvk.DSCInitialization.Kind,
			},
			matcher: BeFalse(),
		},
		{
			name: "Invalid owner reference (different kind)",
			ownerRef: metav1.OwnerReference{
				APIVersion: gvk.DSCInitialization.GroupVersion().String(),
				Kind:       "OtherKind",
			},
			matcher: BeFalse(),
		},
		{
			name: "Invalid owner reference (different group and kind)",
			ownerRef: metav1.OwnerReference{
				APIVersion: "othergroup/v1",
				Kind:       "OtherKind",
			},
			matcher: BeFalse(),
		},
		{
			name:     "Empty owner reference",
			ownerRef: metav1.OwnerReference{},
			matcher:  BeFalse(),
		},
	}

	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := isLegacyOwnerRef(tt.ownerRef)
			g.Expect(result).To(tt.matcher)
		})
	}
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

	err = removeOwnerReferences(ctx, cli, configMap, predicate)
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
