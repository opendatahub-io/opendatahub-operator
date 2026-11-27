//nolint:testpackage // Reuses the source-specific handler stubs and registry isolation.
package modules

import (
	"context"
	"errors"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/spf13/viper"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func TestResolvePlatformInputs(t *testing.T) {
	const namespaceKey = "rhai-applications-namespace"
	originalNamespace := viper.Get(namespaceKey)
	t.Cleanup(func() { viper.Set(namespaceKey, originalNamespace) })

	t.Run("resolves both namespaces from DSCI", func(t *testing.T) {
		g := NewWithT(t)
		viper.Set(namespaceKey, "")
		dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: testDSCIName}}
		dsci.Spec.ApplicationsNamespace = testApplicationsNamespace
		dsci.Spec.Monitoring.Namespace = "monitoring"
		cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
		g.Expect(err).ShouldNot(HaveOccurred())

		inputs, err := ResolvePlatformInputs(t.Context(), cli)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(inputs).Should(Equal(PlatformInputs{
			ApplicationsNamespace: testApplicationsNamespace,
			MonitoringNamespace:   "monitoring",
		}))
	})

	t.Run("requires applications namespace resolution", func(t *testing.T) {
		g := NewWithT(t)
		viper.Set(namespaceKey, "")
		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		inputs, err := ResolvePlatformInputs(t.Context(), cli)

		g.Expect(err).Should(MatchError(ContainSubstring("resolving applications namespace")))
		g.Expect(k8serr.IsNotFound(err)).Should(BeTrue())
		g.Expect(inputs).Should(BeZero())
	})

	t.Run("monitoring DSCI is absent", func(t *testing.T) {
		g := NewWithT(t)
		viper.Set(namespaceKey, testApplicationsNamespace)
		cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cli client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				return cli.List(ctx, list, opts...)
			},
		}))
		g.Expect(err).ShouldNot(HaveOccurred())

		inputs, err := ResolvePlatformInputs(t.Context(), cli)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(inputs).Should(Equal(PlatformInputs{ApplicationsNamespace: testApplicationsNamespace}))
	})

	t.Run("monitoring CRD is absent on xks", func(t *testing.T) {
		g := NewWithT(t)
		viper.Set(namespaceKey, testApplicationsNamespace)
		cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cli client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*dsciv2.DSCInitializationList); ok {
					return &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "dscinitialization.opendatahub.io", Kind: "DSCInitialization"}}
				}
				return cli.List(ctx, list, opts...)
			},
		}))
		g.Expect(err).ShouldNot(HaveOccurred())

		inputs, err := ResolvePlatformInputs(t.Context(), cli)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(inputs).Should(Equal(PlatformInputs{ApplicationsNamespace: testApplicationsNamespace}))
	})

	t.Run("monitoring lookup fails", func(t *testing.T) {
		g := NewWithT(t)
		viper.Set(namespaceKey, testApplicationsNamespace)
		cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cli client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*dsciv2.DSCInitializationList); ok {
					return errors.New("monitoring unavailable")
				}
				return cli.List(ctx, list, opts...)
			},
		}))
		g.Expect(err).ShouldNot(HaveOccurred())

		inputs, err := ResolvePlatformInputs(t.Context(), cli)

		g.Expect(err).Should(MatchError(ContainSubstring("resolving monitoring namespace")))
		g.Expect(inputs).Should(BeZero())
	})
}

func TestNewPlatformCR(t *testing.T) {
	withTestRegistry(t)
	DefaultRegistry().Add(&testDSCConfiguredHandler{BaseHandler: BaseHandler{Config: ModuleConfig{Name: "dashboard"}}})
	DefaultRegistry().Add(&testDSCIConfiguredHandler{BaseHandler: BaseHandler{Config: ModuleConfig{Name: "monitoring"}}}, WithConfigSource(ConfigFromDSCI))
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.Dashboard.Standard.ManagementState = operatorv1.Managed
	dsci := &dsciv2.DSCInitialization{}
	dsci.Spec.Monitoring.ManagementState = operatorv1.Managed

	for _, tc := range []struct {
		name    string
		source  ConfigSource
		modules configApi.PlatformModules
	}{
		{name: "DSC", source: ConfigFromDSC, modules: configApi.PlatformModules{Dashboard: common.ManagementSpec{ManagementState: operatorv1.Managed}}},
		{name: "DSCI", source: ConfigFromDSCI, modules: configApi.PlatformModules{Monitoring: common.ManagementSpec{ManagementState: operatorv1.Managed}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			platform := NewPlatformCR(&DSCContext{DSC: dsc, DSCI: dsci}, tc.source)

			g.Expect(platform).Should(Equal(&configApi.Platform{
				TypeMeta: metav1.TypeMeta{
					APIVersion: configApi.GroupVersion.String(),
					Kind:       configApi.PlatformKind,
				},
				ObjectMeta: metav1.ObjectMeta{Name: configApi.PlatformInstanceName},
				Spec:       configApi.PlatformSpec{Modules: tc.modules},
			}))
		})
	}
}

func TestEnsurePlatformOwnerReferenceSkipsUnnecessaryUpdates(t *testing.T) {
	for _, deleting := range []bool{false, true} {
		name := "owner already present"
		if deleting {
			name = "Platform is deleting"
		}
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			s, err := scheme.New()
			g.Expect(err).ShouldNot(HaveOccurred())
			owner := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: testDSCName, UID: types.UID("dsc-uid")}}
			platform := &configApi.Platform{ObjectMeta: metav1.ObjectMeta{Name: configApi.PlatformInstanceName}}
			if deleting {
				now := metav1.Now()
				platform.DeletionTimestamp = &now
				platform.Finalizers = []string{"test.example/finalizer"}
			} else {
				g.Expect(controllerutil.SetOwnerReference(owner, platform, s)).Should(Succeed())
			}
			updates := 0
			cli, err := fakeclient.New(fakeclient.WithScheme(s), fakeclient.WithObjects(platform), fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Update: func(ctx context.Context, cli client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					updates++
					return cli.Update(ctx, obj, opts...)
				},
			}))
			g.Expect(err).ShouldNot(HaveOccurred())
			originalOwners := platform.GetOwnerReferences()

			g.Expect(EnsurePlatformOwnerReference(t.Context(), cli, owner, cli.Scheme())).Should(Succeed())

			g.Expect(updates).Should(BeZero())
			stored := &configApi.Platform{}
			g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(platform), stored)).Should(Succeed())
			g.Expect(stored.GetOwnerReferences()).Should(Equal(originalOwners))
		})
	}
}

func TestEnsurePlatformOwnerReferencePropagatesWriteErrors(t *testing.T) {
	for _, mergeFailure := range []bool{false, true} {
		name := "update fails"
		if mergeFailure {
			name = "owner type is not registered"
		}
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			writeErr := errors.New("update unavailable")
			platform := &configApi.Platform{ObjectMeta: metav1.ObjectMeta{Name: configApi.PlatformInstanceName}}
			cli, err := fakeclient.New(fakeclient.WithObjects(platform), fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
					return writeErr
				},
			}))
			g.Expect(err).ShouldNot(HaveOccurred())
			owner := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: testDSCName, UID: types.UID("dsc-uid")}}
			ownerScheme := cli.Scheme()
			if mergeFailure {
				ownerScheme = runtime.NewScheme()
			}

			err = EnsurePlatformOwnerReference(t.Context(), cli, owner, ownerScheme)

			if mergeFailure {
				g.Expect(err).Should(MatchError(ContainSubstring("failed to merge Platform owner reference")))
			} else {
				g.Expect(err).Should(MatchError(writeErr))
			}
		})
	}
}
