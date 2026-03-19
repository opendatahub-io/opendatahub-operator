package v1alpha1

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// TestMonitoringCELValidationEnvtest verifies Monitoring CRD CEL rules against a real API server (envtest).
func TestMonitoringCELValidationEnvtest(t *testing.T) {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	g := NewWithT(t)
	ctx := context.Background()

	projectDir, err := envtestutil.FindProjectRoot()
	g.Expect(err).NotTo(HaveOccurred())

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(projectDir, "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(cfg).ToNot(BeNil())
	defer func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	}()

	k8sClient, err := client.New(cfg, client.Options{Scheme: monitoringTestScheme()})
	g.Expect(err).ToNot(HaveOccurred())

	t.Run("invalid name is rejected", func(t *testing.T) {
		g := NewWithT(t)
		m := &Monitoring{
			ObjectMeta: metav1.ObjectMeta{Name: "wrong-name"},
			Spec: MonitoringSpec{
				MonitoringCommonSpec: MonitoringCommonSpec{
					Metrics: &Metrics{
						Storage:  &MetricsStorage{Size: resource.MustParse("5Gi")},
						Replicas: 1,
					},
				},
			},
		}
		err := k8sClient.Create(ctx, m)
		g.Expect(err).To(HaveOccurred())
		g.Expect(k8serrors.IsInvalid(err)).To(BeTrue())
		g.Expect(err.Error()).To(ContainSubstring("default-monitoring"))
	})

	t.Run("metrics replicas without storage is rejected", func(t *testing.T) {
		g := NewWithT(t)
		m := &Monitoring{
			ObjectMeta: metav1.ObjectMeta{Name: MonitoringInstanceName},
			Spec: MonitoringSpec{
				MonitoringCommonSpec: MonitoringCommonSpec{
					Metrics: &Metrics{Replicas: 2},
				},
			},
		}
		err := k8sClient.Create(ctx, m)
		g.Expect(err).To(HaveOccurred())
		g.Expect(k8serrors.IsInvalid(err)).To(BeTrue())
		g.Expect(err.Error()).To(ContainSubstring("metrics.storage"))
	})

	t.Run("reserved exporter name prometheus is rejected", func(t *testing.T) {
		g := NewWithT(t)
		m := &Monitoring{
			ObjectMeta: metav1.ObjectMeta{Name: MonitoringInstanceName},
			Spec: MonitoringSpec{
				MonitoringCommonSpec: MonitoringCommonSpec{
					Metrics: &Metrics{
						Storage:  &MetricsStorage{Size: resource.MustParse("5Gi")},
						Replicas: 1,
						Exporters: map[string]runtime.RawExtension{
							"prometheus": {Raw: []byte(`{}`)},
						},
					},
				},
			},
		}
		err := k8sClient.Create(ctx, m)
		g.Expect(err).To(HaveOccurred())
		g.Expect(k8serrors.IsInvalid(err)).To(BeTrue())
		g.Expect(err.Error()).To(ContainSubstring("prometheus"))
	})

	t.Run("traces s3 backend without secret is rejected", func(t *testing.T) {
		g := NewWithT(t)
		m := &Monitoring{
			ObjectMeta: metav1.ObjectMeta{Name: MonitoringInstanceName},
			Spec: MonitoringSpec{
				MonitoringCommonSpec: MonitoringCommonSpec{
					Traces: &Traces{
						Storage: TracesStorage{Backend: StorageBackendS3},
					},
				},
			},
		}
		err := k8sClient.Create(ctx, m)
		g.Expect(err).To(HaveOccurred())
		g.Expect(k8serrors.IsInvalid(err)).To(BeTrue())
		g.Expect(err.Error()).To(ContainSubstring("secret"))
	})

	t.Run("valid metrics with storage and replicas", func(t *testing.T) {
		g := NewWithT(t)
		m := &Monitoring{
			ObjectMeta: metav1.ObjectMeta{Name: MonitoringInstanceName},
			Spec: MonitoringSpec{
				MonitoringCommonSpec: MonitoringCommonSpec{
					Metrics: &Metrics{
						Storage:  &MetricsStorage{Size: resource.MustParse("5Gi")},
						Replicas: 2,
					},
				},
			},
		}
		g.Expect(k8sClient.Create(ctx, m)).To(Succeed())
		g.Expect(k8sClient.Delete(ctx, m)).To(Succeed())
	})

	t.Run("valid zero replicas without storage", func(t *testing.T) {
		g := NewWithT(t)
		m := &Monitoring{
			ObjectMeta: metav1.ObjectMeta{Name: MonitoringInstanceName},
			Spec: MonitoringSpec{
				MonitoringCommonSpec: MonitoringCommonSpec{
					Metrics: &Metrics{Replicas: 0},
				},
			},
		}
		g.Expect(k8sClient.Create(ctx, m)).To(Succeed())
		g.Expect(k8sClient.Delete(ctx, m)).To(Succeed())
	})

	t.Run("valid traces with pv storage", func(t *testing.T) {
		g := NewWithT(t)
		m := &Monitoring{
			ObjectMeta: metav1.ObjectMeta{Name: MonitoringInstanceName},
			Spec: MonitoringSpec{
				MonitoringCommonSpec: MonitoringCommonSpec{
					Traces: &Traces{
						Storage: TracesStorage{
							Backend: StorageBackendPV,
							Size:    "1Gi",
						},
					},
				},
			},
		}
		g.Expect(k8sClient.Create(ctx, m)).To(Succeed())
		g.Expect(k8sClient.Delete(ctx, m)).To(Succeed())
	})
}

func monitoringTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = SchemeBuilder.AddToScheme(s)
	return s
}
