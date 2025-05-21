package certconfigmapgenerator_test

import (
	"context"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/certconfigmapgenerator"
	annotation "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

//nolint:gochecknoinits
func init() {
	log.SetLogger(zap.New(zap.UseDevMode(true)))
}

//nolint:dupl
func TestCertConfigmapGeneratorReconciler(t *testing.T) {
	g := NewWithT(t)
	gctx, cancel := context.WithCancel(context.Background())

	id := xid.New().String()

	env, err := envt.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	t.Cleanup(func() {
		cancel()

		err := env.Stop()
		g.Expect(err).NotTo(HaveOccurred())
	})

	mgr, err := ctrl.NewManager(env.Config(), ctrl.Options{
		Scheme: env.Scheme(),
	})

	go func() {
		err = mgr.Start(gctx)
		g.Expect(err).ShouldNot(HaveOccurred())
	}()

	err = certconfigmapgenerator.NewWithManager(gctx, mgr)
	g.Expect(err).ToNot(HaveOccurred())

	dsci := dsciv1.DSCInitialization{}
	dsci.Name = id

	mgr.GetCache().WaitForCacheSync(gctx)

	ns1 := corev1.Namespace{}
	ns1.Name = xid.New().String()

	err = env.Client().Create(gctx, &ns1)
	g.Expect(err).ShouldNot(HaveOccurred())

	ns2 := corev1.Namespace{}
	ns2.Name = xid.New().String()
	ns2.Annotations = map[string]string{}

	err = env.Client().Create(gctx, &ns2)
	g.Expect(err).ShouldNot(HaveOccurred())

	t.Run("TrustedCABundle missing (nil)", func(t *testing.T) {
		ctx := context.Background()
		g := G(t)

		// Remove TrustedCABundle from the spec (set to nil)
		_, err = ctrl.CreateOrUpdate(ctx, env.Client(), &dsci, func() error {
			dsci.Spec.TrustedCABundle = nil
			return nil
		})

		g.Expect(err).ShouldNot(HaveOccurred())

		g.Consistently(func() ([]corev1.ConfigMap, error) {
			return listCABundleConfigMaps(ctx, env.Client())
		}).Should(
			BeEmpty(),
		)
	})

	t.Run("TrustedCABundle set to Removed", func(t *testing.T) {
		ctx := context.Background()
		g := G(t)

		_, err = ctrl.CreateOrUpdate(ctx, env.Client(), &dsci, func() error {
			dsci.Spec.TrustedCABundle = &dsciv1.TrustedCABundleSpec{
				ManagementState: operatorv1.Removed,
			}

			return nil
		})

		g.Expect(err).ShouldNot(HaveOccurred())

		g.Consistently(func() ([]corev1.ConfigMap, error) {
			return listCABundleConfigMaps(ctx, env.Client())
		}).Should(
			BeEmpty(),
		)
	})

	t.Run("TrustedCABundle set to Managed", func(t *testing.T) {
		ctx := context.Background()
		g := G(t)

		_, err = ctrl.CreateOrUpdate(ctx, env.Client(), &ns1, func() error {
			ns1.Annotations = map[string]string{
				annotation.InjectionOfCABundleAnnotatoion: "false",
			}

			return nil
		})

		g.Expect(err).ShouldNot(HaveOccurred())

		err = env.Client().Update(ctx, &ns1)
		g.Expect(err).ShouldNot(HaveOccurred())

		_, err = ctrl.CreateOrUpdate(ctx, env.Client(), &dsci, func() error {
			dsci.Spec.TrustedCABundle = &dsciv1.TrustedCABundleSpec{
				ManagementState: operatorv1.Managed,
			}

			return nil
		})

		g.Expect(err).ShouldNot(HaveOccurred())

		g.Consistently(func() (*corev1.ConfigMap, error) {
			return getCABundlesConfigMap(ctx, env.Client(), ns1.Name)
		}).Should(
			BeNil(),
		)

		g.Expect(func() (*corev1.ConfigMap, error) {
			return getCABundlesConfigMap(ctx, env.Client(), ns2.Name)
		}).ShouldNot(
			BeNil(),
		)
	})

	t.Run("Namespace opt-in", func(t *testing.T) {
		ctx := context.Background()
		g := G(t)

		_, err = ctrl.CreateOrUpdate(ctx, env.Client(), &ns1, func() error {
			ns1.Annotations = map[string]string{
				annotation.InjectionOfCABundleAnnotatoion: "true",
			}

			return nil
		})

		g.Expect(err).ShouldNot(HaveOccurred())

		err = env.Client().Update(ctx, &ns1)
		g.Expect(err).ShouldNot(HaveOccurred())

		_, err = ctrl.CreateOrUpdate(ctx, env.Client(), &dsci, func() error {
			dsci.Spec.TrustedCABundle = &dsciv1.TrustedCABundleSpec{
				ManagementState: operatorv1.Managed,
			}

			return nil
		})

		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(func() (*corev1.ConfigMap, error) {
			return getCABundlesConfigMap(ctx, env.Client(), ns1.Name)
		}).ShouldNot(
			BeNil(),
		)

		g.Expect(func() (*corev1.ConfigMap, error) {
			return getCABundlesConfigMap(ctx, env.Client(), ns2.Name)
		}).ShouldNot(
			BeNil(),
		)
	})

	t.Run("TrustedCABundle reset to Removed", func(t *testing.T) {
		ctx := context.Background()
		g := G(t)

		_, err = ctrl.CreateOrUpdate(ctx, env.Client(), &dsci, func() error {
			dsci.Spec.TrustedCABundle = &dsciv1.TrustedCABundleSpec{
				ManagementState: operatorv1.Removed,
			}

			return nil
		})

		g.Expect(err).ShouldNot(HaveOccurred())

		g.Eventually(func() ([]corev1.ConfigMap, error) {
			return listCABundleConfigMaps(ctx, env.Client())
		}).Should(
			BeEmpty(),
		)
	})
}

func G(t *testing.T) *WithT {
	t.Helper()

	g := NewWithT(t)
	g.DurationBundle.EventuallyTimeout = 30 * time.Second
	g.DurationBundle.ConsistentlyDuration = 30 * time.Second

	return g
}

func listCABundleConfigMaps(ctx context.Context, cli client.Client) ([]corev1.ConfigMap, error) {
	items := corev1.ConfigMapList{}

	err := cli.List(
		ctx,
		&items,
		client.MatchingFields{
			"metadata.name": certconfigmapgenerator.CAConfigMapName,
		},
		client.MatchingLabels{
			labels.K8SCommon.PartOf: certconfigmapgenerator.PartOf,
		},
	)

	if err != nil {
		return nil, err
	}

	return items.Items, nil
}

func getCABundlesConfigMap(ctx context.Context, cli client.Client, ns string) (*corev1.ConfigMap, error) {
	nn := types.NamespacedName{
		Name:      certconfigmapgenerator.CAConfigMapName,
		Namespace: ns,
	}

	item := corev1.ConfigMap{}

	err := cli.Get(ctx, nn, &item)
	switch {
	case errors.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, err
	default:
		return &item, nil
	}
}
