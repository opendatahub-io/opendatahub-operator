package certconfigmapgenerator_test

import (
	"context"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func G(t *testing.T) *WithT {
	t.Helper()

	g := NewWithT(t)
	g.DurationBundle.EventuallyTimeout = 30 * time.Second
	g.DurationBundle.ConsistentlyDuration = 15 * time.Second

	return g
}

func TestCertConfigmapGeneratorReconciler(t *testing.T) {
	g := NewWithT(t)
	gctx, cancel := context.WithCancel(t.Context())

	id := xid.New().String()

	env, err := envt.New(envt.WithManager())
	g.Expect(err).ShouldNot(HaveOccurred())

	t.Cleanup(func() {
		cancel()

		err := env.Stop()
		g.Expect(err).NotTo(HaveOccurred())
	})

	go func() {
		err = env.Manager().Start(gctx)
		g.Expect(err).ShouldNot(HaveOccurred())
	}()

	err = certconfigmapgenerator.NewWithManager(gctx, env.Manager())
	g.Expect(err).ToNot(HaveOccurred())

	dsci := dsciv1.DSCInitialization{}
	dsci.Name = id

	env.Manager().GetCache().WaitForCacheSync(gctx)

	ns1 := corev1.Namespace{}
	ns1.Name = xid.New().String()

	err = env.Client().Create(gctx, &ns1)
	g.Expect(err).ShouldNot(HaveOccurred())

	ns2 := corev1.Namespace{}
	ns2.Name = xid.New().String()
	ns2.Annotations = map[string]string{}

	ns3 := corev1.Namespace{}
	ns3.Name = "openshift-" + xid.New().String()
	ns3.Annotations = map[string]string{}

	err = env.Client().Create(gctx, &ns2)
	g.Expect(err).ShouldNot(HaveOccurred())

	t.Run("TrustedCABundle ManagementState set to Managed", func(t *testing.T) {
		ctx := t.Context()
		g := G(t)

		_, err = ctrl.CreateOrUpdate(ctx, env.Client(), &ns1, func() error {
			ns1.Annotations = map[string]string{
				annotation.InjectionOfCABundleAnnotatoion: "false",
			}
			return nil
		})

		g.Expect(err).ShouldNot(HaveOccurred())

		_, err = ctrl.CreateOrUpdate(ctx, env.Client(), &dsci, func() error {
			dsci.Spec.TrustedCABundle = &dsciv1.TrustedCABundleSpec{
				ManagementState: operatorv1.Managed,
			}
			return nil
		})

		g.Expect(err).ShouldNot(HaveOccurred())

		g.Consistently(getCABundlesConfigMap(ctx, env.Client(), ns1.Name)).Should(BeNil())
		g.Eventually(getCABundlesConfigMap(ctx, env.Client(), ns2.Name)).ShouldNot(BeNil())
		g.Consistently(getCABundlesConfigMap(ctx, env.Client(), ns3.Name)).Should(BeNil())
	})

	t.Run("TrustedCABundle ManagementState set to Managed, Namespace opt-in", func(t *testing.T) {
		ctx := t.Context()
		g := G(t)

		_, err = ctrl.CreateOrUpdate(ctx, env.Client(), &ns1, func() error {
			ns1.Annotations = map[string]string{
				annotation.InjectionOfCABundleAnnotatoion: "true",
			}
			return nil
		})
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Eventually(getCABundlesConfigMap(ctx, env.Client(), ns1.Name)).ShouldNot(BeNil())
		g.Eventually(getCABundlesConfigMap(ctx, env.Client(), ns2.Name)).ShouldNot(BeNil())
		g.Consistently(getCABundlesConfigMap(ctx, env.Client(), ns3.Name)).Should(BeNil())
	})

	t.Run("TrustedCABundle ManagementState set to Managed, Namespace opt-out", func(t *testing.T) {
		ctx := t.Context()
		g := G(t)

		_, err = ctrl.CreateOrUpdate(ctx, env.Client(), &ns1, func() error {
			ns1.Annotations = map[string]string{
				annotation.InjectionOfCABundleAnnotatoion: "False",
			}
			return nil
		})
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Eventually(getCABundlesConfigMap(ctx, env.Client(), ns1.Name)).Should(BeNil())
		g.Consistently(getCABundlesConfigMap(ctx, env.Client(), ns1.Name)).Should(BeNil())

		g.Eventually(getCABundlesConfigMap(ctx, env.Client(), ns2.Name)).ShouldNot(BeNil())
		g.Consistently(getCABundlesConfigMap(ctx, env.Client(), ns3.Name)).Should(BeNil())
	})

	t.Run("TrustedCABundle ManagementState set to Unmanaged", func(t *testing.T) {
		ctx := t.Context()
		g := G(t)

		_, err = ctrl.CreateOrUpdate(ctx, env.Client(), &dsci, func() error {
			dsci.Spec.TrustedCABundle = &dsciv1.TrustedCABundleSpec{
				ManagementState: operatorv1.Unmanaged,
			}
			return nil
		})

		g.Expect(err).ShouldNot(HaveOccurred())

		g.Consistently(getCABundlesConfigMap(ctx, env.Client(), ns1.Name)).Should(BeNil())
		g.Consistently(getCABundlesConfigMap(ctx, env.Client(), ns2.Name)).ShouldNot(BeNil())
		g.Consistently(getCABundlesConfigMap(ctx, env.Client(), ns3.Name)).Should(BeNil())
	})

	t.Run("TrustedCABundle ManagementState set to Removed", func(t *testing.T) {
		ctx := t.Context()
		g := G(t)

		_, err = ctrl.CreateOrUpdate(ctx, env.Client(), &dsci, func() error {
			dsci.Spec.TrustedCABundle = &dsciv1.TrustedCABundleSpec{
				ManagementState: operatorv1.Removed,
			}
			return nil
		})

		g.Expect(err).ShouldNot(HaveOccurred())

		g.Eventually(listCABundleConfigMaps(ctx, env.Client())).Should(BeEmpty())
		g.Consistently(listCABundleConfigMaps(ctx, env.Client())).Should(BeEmpty())
	})

	t.Run("TrustedCABundle set to nil", func(t *testing.T) {
		ctx := t.Context()
		g := G(t)

		_, err = ctrl.CreateOrUpdate(ctx, env.Client(), &dsci, func() error {
			dsci.Spec.TrustedCABundle = &dsciv1.TrustedCABundleSpec{
				ManagementState: operatorv1.Managed,
			}
			return nil
		})

		g.Expect(err).ShouldNot(HaveOccurred())

		g.Consistently(getCABundlesConfigMap(ctx, env.Client(), ns1.Name)).Should(BeNil())
		g.Eventually(getCABundlesConfigMap(ctx, env.Client(), ns2.Name)).ShouldNot(BeNil())
		g.Consistently(getCABundlesConfigMap(ctx, env.Client(), ns3.Name)).Should(BeNil())

		_, err = ctrl.CreateOrUpdate(ctx, env.Client(), &dsci, func() error {
			dsci.Spec.TrustedCABundle = nil
			return nil
		})

		g.Expect(err).ShouldNot(HaveOccurred())

		g.Eventually(listCABundleConfigMaps(ctx, env.Client())).Should(BeEmpty())
		g.Consistently(listCABundleConfigMaps(ctx, env.Client())).Should(BeEmpty())
	})
}

func listCABundleConfigMaps(ctx context.Context, cli client.Client) func() ([]corev1.ConfigMap, error) {
	return func() ([]corev1.ConfigMap, error) {
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
}

func getCABundlesConfigMap(ctx context.Context, cli client.Client, ns string) func() (*corev1.ConfigMap, error) {
	return func() (*corev1.ConfigMap, error) {
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
}

func TestInjectTrustedCABundle(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{
			name:        "no annotations",
			annotations: nil,
			want:        true,
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			want:        true,
		},
		{
			name: "annotation not found",
			annotations: map[string]string{
				"other-annotation": "value",
			},
			want: true,
		},
		{
			name: "annotation set to true",
			annotations: map[string]string{
				annotation.InjectionOfCABundleAnnotatoion: "true",
			},
			want: true,
		},
		{
			name: "annotation set to false",
			annotations: map[string]string{
				annotation.InjectionOfCABundleAnnotatoion: "false",
			},
			want: false,
		},
		{
			name: "annotation set to false (mixed case)",
			annotations: map[string]string{
				annotation.InjectionOfCABundleAnnotatoion: "False",
			},
			want: false,
		},
		{
			name: "annotation with invalid value",
			annotations: map[string]string{
				annotation.InjectionOfCABundleAnnotatoion: "invalid",
			},
			want: true,
		},
		{
			name: "annotation with mixed case",
			annotations: map[string]string{
				annotation.InjectionOfCABundleAnnotatoion: "TRUE",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-namespace",
					Annotations: tt.annotations,
				},
			}

			got := certconfigmapgenerator.ShouldInjectTrustedCABundle(ns)
			g.Expect(got).To(Equal(tt.want), "ShouldInjectTrustedCABundle() = %v, want %v", got, tt.want)
		})
	}
}
