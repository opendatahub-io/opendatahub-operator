// this uses unexported function for testing purpose
//
//nolint:testpackage
package deploy

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/api/pkg/lib/version"
	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrlCli "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

func TestDeployWithCacheAction(t *testing.T) {
	g := NewWithT(t)
	s := runtime.NewScheme()

	ctx := context.Background()

	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(apiextensionsv1.AddToScheme(s))

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

	envTestClient, err := ctrlCli.New(cfg, ctrlCli.Options{Scheme: s})
	g.Expect(err).NotTo(HaveOccurred())

	cli, err := client.New(ctx, cfg, envTestClient)
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

	t.Run("NonExistingResource(", func(t *testing.T) {
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
}

func testResourceNotReDeployed(t *testing.T, cli *client.Client, obj ctrlCli.Object, create bool) {
	t.Helper()

	g := NewWithT(t)
	ctx := context.Background()

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
		DSC: &dscv1.DataScienceCluster{},
		Instance: &componentsv1.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		},
		Release: cluster.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}}},
	}

	action := New(
		WithCache(),
		WithMode(ModeSSA),
		WithFieldOwner(xid.New().String()),
	)

	// Resource should be created if missing
	ok1, err := action.deploy(ctx, &rr, *in.DeepCopy())
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ok1).Should(BeTrue())

	out1 := unstructured.Unstructured{}
	out1.SetGroupVersionKind(in.GroupVersionKind())

	err = cli.Get(ctx, ctrlCli.ObjectKeyFromObject(in), &out1)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Resource should not be re-deployed
	ok2, err := action.deploy(ctx, &rr, *in.DeepCopy())
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ok2).Should(BeFalse())

	out2 := unstructured.Unstructured{}
	out2.SetGroupVersionKind(in.GroupVersionKind())

	err = cli.Get(ctx, ctrlCli.ObjectKeyFromObject(in), &out2)
	g.Expect(err).ShouldNot(HaveOccurred())

	// check that the resource version has not changed
	g.Expect(out1.GetResourceVersion()).Should(Equal(out2.GetResourceVersion()))
}
