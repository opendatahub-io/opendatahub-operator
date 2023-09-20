package servicemesh_test

import (
	"context"
	"fmt"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
	v1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"math/rand"
	"path/filepath"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var (
	envTestClient client.Client
	envTest       *envtest.Environment
	ctx           context.Context
	cancel        context.CancelFunc
)

var testScheme = runtime.NewScheme()

func TestServiceMeshSetupIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Openshift Service Mesh infra setup integration")
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
				filepath.Join(projectDir, "tests", "integration", "servicemesh", "crd"),
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
