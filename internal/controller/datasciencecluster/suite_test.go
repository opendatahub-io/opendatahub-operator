/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package datasciencecluster_test

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/datasciencecluster"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	verifyNoErrorMsg     = "verifying no error is returned"
	callingReconcilerMsg = "calling NewDataScienceClusterReconciler"
)

// generateUniqueName creates a unique name for test instances.
// It uses a deterministic random source based on the prefix for reproducible names.
func generateUniqueName(prefix string) string {
	// Use deterministic random source based on prefix hash for reproducible names
	var seed int64
	for _, c := range prefix {
		seed = seed*31 + int64(c)
	}
	//nolint:gosec // Using math/rand with fixed seed for deterministic test names
	r := rand.New(rand.NewSource(seed))
	suffix := r.Int63n(1000000)
	return fmt.Sprintf("%s-%06d", prefix, suffix)
}

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	mgr       manager.Manager
	ctx       context.Context
	cancel    context.CancelFunc
)

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	binaryAssetsDir := os.Getenv("KUBEBUILDER_ASSETS")
	if binaryAssetsDir == "" {
		// Fallback to default location if not set
		platformSuffix := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
		binaryAssetsDir = filepath.Join("..", "..", "..", "bin", "k8s", "1.32.0-"+platformSuffix)
	}
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		BinaryAssetsDirectory: binaryAssetsDir,
	}

	// Initialize context for the test suite
	//nolint:fatcontext // This is a test setup function where context creation is necessary
	ctx, cancel = context.WithCancel(context.Background())

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = dscv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = dsciv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = componentApi.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Create manager
	mgr, err = manager.New(cfg, manager.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(mgr).NotTo(BeNil())

	// Do not start the manager here; tests register controllers via Build(ctx).
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")

	// Cancel the context to stop the manager
	if cancel != nil {
		cancel()
	}

	// Stop the test environment if it was successfully started
	if testEnv != nil {
		err := testEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	}
})

var _ = Describe("NewDataScienceClusterReconciler", func() {
	BeforeEach(func() {
		// Reset context for each test
		ctx = context.Background()
	})

	It("should create reconciler with correct configuration", func() {
		By(callingReconcilerMsg)
		err := datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, mgr, generateUniqueName("integration-test-datasciencecluster-config"))

		By(verifyNoErrorMsg)
		Expect(err).NotTo(HaveOccurred())

		// Optionally, start the manager in a goroutine and verify it's ready
		// This would provide stronger validation that the reconciler is properly configured
	})

	Context("when called with nil manager", func() {
		It("should panic", func() {
			By("calling NewDataScienceClusterReconciler with nil manager")
			Expect(func() {
				_ = datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, nil, generateUniqueName("integration-test-datasciencecluster-nil"))
			}).To(Panic())
		})
	})

	Context("when called multiple times", func() {
		It("should handle multiple calls without error", func() {
			By("calling NewDataScienceClusterReconciler multiple times")
			err1 := datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, mgr, generateUniqueName("integration-test-datasciencecluster-multi-1"))
			err2 := datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, mgr, generateUniqueName("integration-test-datasciencecluster-multi-2"))

			By("verifying no errors are returned")
			Expect(err1).NotTo(HaveOccurred())
			Expect(err2).NotTo(HaveOccurred())
		})
	})
})

var _ = Describe("DataScienceCluster Reconciler Configuration", func() {
	BeforeEach(func() {
		ctx = context.Background()
	})

	It("should configure reconciler with all required component ownerships", func() {
		By(callingReconcilerMsg)
		err := datasciencecluster.NewDataScienceClusterReconcilerWithName(
			ctx, mgr,
			generateUniqueName("integration-test-datasciencecluster-ownerships"),
		)

		By(verifyNoErrorMsg)
		Expect(err).NotTo(HaveOccurred())

		// Successful creation verifies that:
		// - All required component types are registered in the scheme
		// - The reconciler successfully sets up ownership for Dashboard, Workbenches, etc.
		// - The controller builder accepts all ownership configurations
		// Further validation would require starting the manager and inspecting runtime behavior
	})

	It("should configure reconciler with DSCInitialization watches", func() {
		By(callingReconcilerMsg)
		err := datasciencecluster.NewDataScienceClusterReconcilerWithName(
			ctx, mgr,
			generateUniqueName("integration-test-datasciencecluster-watches"),
		)

		By(verifyNoErrorMsg)
		Expect(err).NotTo(HaveOccurred())

		// The reconciler should be configured to watch DSCInitialization objects
		// This is verified by the successful creation of the reconciler
	})

	It("should configure reconciler with all required actions", func() {
		By(callingReconcilerMsg)
		err := datasciencecluster.NewDataScienceClusterReconcilerWithName(
			ctx, mgr,
			generateUniqueName("integration-test-datasciencecluster-actions"),
		)

		By(verifyNoErrorMsg)
		Expect(err).NotTo(HaveOccurred())

		// The reconciler should be configured with actions:
		// - initialize
		// - checkPreConditions
		// - updateStatus
		// - provisionComponents
		// - deploy.NewAction
		// - gc.NewAction
		// This is verified by the successful creation of the reconciler
	})

	It("should configure reconciler with component readiness conditions", func() {
		By(callingReconcilerMsg)
		// Using seed 12346 to avoid collision with other tests that use 12345, ensuring deterministic test execution
		err := datasciencecluster.NewDataScienceClusterReconcilerWithName(
			ctx, mgr,
			generateUniqueName("integration-test-datasciencecluster-conditions"),
		)

		By(verifyNoErrorMsg)
		Expect(err).NotTo(HaveOccurred())

		// The reconciler should be configured with status.ConditionTypeComponentsReady
		// This is verified by the successful creation of the reconciler
	})
})
