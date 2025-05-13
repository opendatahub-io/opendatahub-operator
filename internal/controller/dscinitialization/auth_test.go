package dscinitialization_test

import (
	"context"
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/dscinitialization"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestCreateAuth(t *testing.T) {
	t.Run("should create default Auth when no odh-dashboard-config exists", func(t *testing.T) {
		g := NewWithT(t)
		ctx := context.Background()

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		dsci := &dsciv1.DSCInitialization{
			Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: "test-ns"},
		}

		err = dscinitialization.CreateAuth(ctx, cli, dsci)
		g.Expect(err).ShouldNot(HaveOccurred())

		auth := &serviceApi.Auth{}
		err = cli.Get(ctx, client.ObjectKey{Name: serviceApi.AuthInstanceName}, auth)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(auth.Spec.AdminGroups).To(Equal([]string{dashboard.GetAdminGroup()}))
		g.Expect(auth.Spec.AllowedGroups).To(Equal([]string{"system:authenticated"}))
	})

	t.Run("should create default Auth when no odh-dashboard-config CRD does not exists", func(t *testing.T) {
		g := NewWithT(t)
		ctx := context.Background()

		cli, err := fakeclient.New(
			fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, cli client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if obj.GetObjectKind().GroupVersionKind() == gvk.OdhDashboardConfig {
						return &meta.NoResourceMatchError{}
					}

					return cli.Get(ctx, key, obj, opts...)
				},
			}))

		g.Expect(err).ShouldNot(HaveOccurred())

		dsci := &dsciv1.DSCInitialization{
			Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: "test-ns"},
		}

		err = dscinitialization.CreateAuth(ctx, cli, dsci)
		g.Expect(err).ShouldNot(HaveOccurred())

		auth := &serviceApi.Auth{}
		err = cli.Get(ctx, client.ObjectKey{Name: serviceApi.AuthInstanceName}, auth)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(auth.Spec.AdminGroups).To(Equal([]string{dashboard.GetAdminGroup()}))
		g.Expect(auth.Spec.AllowedGroups).To(Equal([]string{"system:authenticated"}))
	})

	t.Run("should create Auth with groups from odh-dashboard-config", func(t *testing.T) {
		g := NewWithT(t)
		ctx := context.Background()

		odhObj := resources.GvkToUnstructured(gvk.OdhDashboardConfig)
		odhObj.SetName("odh-dashboard-config")
		odhObj.SetNamespace("test-ns")

		err := unstructured.SetNestedStringMap(odhObj.Object, map[string]string{
			"adminGroups":   "admin1",
			"allowedGroups": "allowed1",
		}, "spec", "groupsConfig")

		g.Expect(err).ShouldNot(HaveOccurred())

		cli, err := fakeclient.New(
			fakeclient.WithObjects(odhObj),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		dsci := &dsciv1.DSCInitialization{
			Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: "test-ns"},
		}

		err = dscinitialization.CreateAuth(ctx, cli, dsci)
		g.Expect(err).ShouldNot(HaveOccurred())

		auth := &serviceApi.Auth{}
		err = cli.Get(ctx, client.ObjectKey{Name: serviceApi.AuthInstanceName}, auth)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(auth.Spec.AdminGroups).To(Equal([]string{"admin1"}))
		g.Expect(auth.Spec.AllowedGroups).To(Equal([]string{"allowed1"}))
	})

	t.Run("should create empty Auth when odh-dashboard-config has no groupsConfig", func(t *testing.T) {
		g := NewWithT(t)
		ctx := context.Background()

		odhObj := resources.GvkToUnstructured(gvk.OdhDashboardConfig)
		odhObj.SetName("odh-dashboard-config")
		odhObj.SetNamespace("test-ns")

		cli, err := fakeclient.New(
			fakeclient.WithObjects(odhObj),
		)

		g.Expect(err).ShouldNot(HaveOccurred())

		dsci := &dsciv1.DSCInitialization{
			Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: "test-ns"},
		}

		err = dscinitialization.CreateAuth(ctx, cli, dsci)
		g.Expect(err).ShouldNot(HaveOccurred())

		auth := &serviceApi.Auth{}
		err = cli.Get(ctx, client.ObjectKey{Name: serviceApi.AuthInstanceName}, auth)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(auth.Spec.AdminGroups).To(BeEmpty())
		g.Expect(auth.Spec.AllowedGroups).To(BeEmpty())
	})

	t.Run("should not error if Auth already exists", func(t *testing.T) {
		g := NewWithT(t)
		ctx := context.Background()

		cli, err := fakeclient.New(
			fakeclient.WithObjects(&serviceApi.Auth{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.AuthInstanceName},
			}),
		)

		g.Expect(err).ShouldNot(HaveOccurred())

		dsci := &dsciv1.DSCInitialization{
			ObjectMeta: metav1.ObjectMeta{Name: "test-dsci"},
			Spec:       dsciv1.DSCInitializationSpec{ApplicationsNamespace: "test-ns"},
		}

		err = dscinitialization.CreateAuth(ctx, cli, dsci)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should return error if get odh-dashboard-config fails unexpectedly", func(t *testing.T) {
		g := NewWithT(t)
		ctx := context.Background()

		cli, err := fakeclient.New(
			fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return errors.New("simulated error")
				},
			}))

		g.Expect(err).ShouldNot(HaveOccurred())
		dsci := &dsciv1.DSCInitialization{
			Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: "test-ns"},
		}

		err = dscinitialization.CreateAuth(ctx, cli, dsci)
		g.Expect(err).Should(HaveOccurred())
	})
}
