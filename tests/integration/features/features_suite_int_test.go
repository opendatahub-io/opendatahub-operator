package features_test

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	envTestClient client.Client
	envTest       *envtest.Environment
	ctx           context.Context
	cancel        context.CancelFunc
)

var testScheme = runtime.NewScheme()

func TestFeaturesIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Basic Features DSL integration tests")
}

var _ = BeforeSuite(func() {
	rand.Seed(time.Now().UTC().UnixNano())

	ctx, cancel = context.WithCancel(context.TODO())

	opts := zap.Options{Development: true}
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseFlagOptions(&opts)))

	By("Bootstrapping k8s test environment")
	projectDir, err := envtestutil.FindProjectRoot()
	if err != nil {
		fmt.Printf("Error finding project root: %v\n", err)
		return
	}

	utilruntime.Must(v1.AddToScheme(testScheme))

	envTest = &envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme: testScheme,
			Paths: []string{
				filepath.Join(projectDir, "config", "crd", "bases"),
				filepath.Join(projectDir, "config", "crd", "dashboard-crds"),
				filepath.Join(projectDir, "tests", "integration", "features", "crd"),
			},
			ErrorIfPathMissing: true,
			CleanUpAfterUse:    false,
		},
	}

	config, err := envTest.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(config).NotTo(BeNil())

	envTestClient, err = client.New(config, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(envTestClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("Tearing down the test environment")
	cancel()
	Expect(envTest.Stop()).To(Succeed())
})
