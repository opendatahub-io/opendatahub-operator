package features_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	envTestClient    client.Client
	envTestClientset *kubernetes.Clientset
	envTest          *envtest.Environment
	ctx              context.Context
	cancel           context.CancelFunc
)

var testScheme = runtime.NewScheme()

func TestFeaturesIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Basic Features DSL integration tests")
}

var _ = BeforeSuite(func() {

	ctx, cancel = context.WithCancel(context.TODO())

	opts := zap.Options{Development: true}
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseFlagOptions(&opts)))

	By("Bootstrapping k8s test environment")
	projectDir, err := envtestutil.FindProjectRoot()
	if err != nil {
		fmt.Printf("Error finding project root: %v\n", err)

		return
	}

	utilruntime.Must(corev1.AddToScheme(testScheme))
	utilruntime.Must(featurev1.AddToScheme(testScheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(testScheme))
	utilruntime.Must(ofapiv1alpha1.AddToScheme(testScheme))

	envTest = &envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme: testScheme,
			Paths: []string{
				filepath.Join(projectDir, "config", "crd", "bases"),
				filepath.Join(projectDir, "config", "crd", "dashboard-crds"),
				filepath.Join(projectDir, "tests", "integration", "features", "fixtures", "crd"),
			},
			ErrorIfPathMissing: true,
			CleanUpAfterUse:    false,
		},
	}

	config, err := envTest.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(config).NotTo(BeNil())

	err = featurev1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())

	envTestClient, err = client.New(config, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(envTestClient).NotTo(BeNil())

	envTestClientset, err = kubernetes.NewForConfig(config)
	Expect(err).NotTo(HaveOccurred())
	Expect(envTestClientset).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("Tearing down the test environment")
	cancel()
	Expect(envTest.Stop()).To(Succeed())
})
