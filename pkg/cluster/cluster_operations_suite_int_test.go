package cluster_test

import (
	"context"
	"math/rand"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

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

func TestClusterOperationsIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Basic cluster operations through custom funcs")
}

var _ = BeforeSuite(func() {
	rand.Seed(time.Now().UTC().UnixNano())

	ctx, cancel = context.WithCancel(context.TODO())

	opts := zap.Options{Development: true}
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseFlagOptions(&opts)))

	By("Bootstrapping k8s test environment")

	utilruntime.Must(v1.AddToScheme(testScheme))

	envTest = &envtest.Environment{}

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
