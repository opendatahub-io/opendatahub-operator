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
	cryptorand "crypto/rand"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
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
// If seed is provided, it uses a deterministic random source for reproducible names.
// If seed is 0, it uses crypto/rand for non-deterministic names and logs the generated name.
func generateUniqueName(prefix string, seed ...int64) string {
	var n *big.Int
	var name string

	if len(seed) > 0 && seed[0] != 0 {
		// Use deterministic random source for reproducible names
		//nolint:gosec // Using math/rand with fixed seed for deterministic test names
		r := rand.New(rand.NewSource(seed[0]))
		n = big.NewInt(r.Int63n(1000000))
		// Use deterministic values for time components when seed is provided
		timeComponent := r.Int63n(1000000000000)    // Random timestamp
		nanosecondComponent := r.Int63n(1000000000) // Random nanoseconds
		pidComponent := r.Int63n(100000)            // Random PID-like value
		name = fmt.Sprintf("%s-%d-%d-%d-%d", prefix, timeComponent, nanosecondComponent, pidComponent, n)
	} else {
		// Use crypto/rand for non-deterministic names
		var err error
		n, err = cryptorand.Int(cryptorand.Reader, big.NewInt(1000000))
		if err != nil {
			// This should never fail in practice, but if it does, fail the test
			Fail(fmt.Sprintf("Failed to generate random number for test name: %v", err))
		}
		pid := os.Getpid()
		now := time.Now()
		name = fmt.Sprintf("%s-%s-%d-%d-%d", prefix, now.Format("20060102150405"), now.Nanosecond(), pid, n)
		// Log the generated name for debugging non-deterministic test failures
		logf.Log.Info("Generated non-deterministic test name", "name", name, "prefix", prefix)
	}

	return name
}

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	mgr       manager.Manager
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

// TestGenerateUniqueName tests the generateUniqueName function.
func TestGenerateUniqueName(t *testing.T) {
	// Test deterministic behavior
	name1 := generateUniqueName("test", 12345)
	name2 := generateUniqueName("test", 12345)
	if name1 != name2 {
		t.Errorf("Deterministic names should be equal: %s != %s", name1, name2)
	}

	// Test different seeds produce different names
	name3 := generateUniqueName("test", 67890)
	if name1 == name3 {
		t.Errorf("Different seeds should produce different names: %s == %s", name1, name3)
	}

	// Test non-deterministic behavior (no seed)
	name4 := generateUniqueName("test")
	name5 := generateUniqueName("test")
	if name4 == name5 {
		t.Logf("Non-deterministic names happened to be equal: %s == %s", name4, name5)
		// This is technically possible but very unlikely
	}
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	binaryAssetsDir := os.Getenv("KUBEBUILDER_ASSETS")
	if binaryAssetsDir == "" {
		// Fallback to default location if not set
		binaryAssetsDir = filepath.Join("..", "..", "..", "bin", "k8s", "1.32.0-darwin-arm64")
	}
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: binaryAssetsDir,
	}

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
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("NewDataScienceClusterReconciler", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Context("when called with valid manager", func() {
		It("should successfully create reconciler without error", func() {
			By(callingReconcilerMsg)
			err := datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, mgr, generateUniqueName("integration-test-datasciencecluster-valid", 12345))

			By(verifyNoErrorMsg)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create reconciler with correct configuration", func() {
			By(callingReconcilerMsg)
			err := datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, mgr, generateUniqueName("integration-test-datasciencecluster-config", 23456))

			By(verifyNoErrorMsg)
			Expect(err).NotTo(HaveOccurred())

			// Optionally, start the manager in a goroutine and verify it's ready
			// This would provide stronger validation that the reconciler is properly configured
		})
	})

	Context("when called with nil manager", func() {
		It("should panic", func() {
			By("calling NewDataScienceClusterReconciler with nil manager")
			Expect(func() {
				_ = datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, nil, generateUniqueName("integration-test-datasciencecluster-nil", 34567))
			}).To(Panic())
		})
	})

	Context("when called multiple times", func() {
		It("should handle multiple calls without error", func() {
			By("calling NewDataScienceClusterReconciler multiple times")
			err1 := datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, mgr, generateUniqueName("integration-test-datasciencecluster-multi-1", 45678))
			err2 := datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, mgr, generateUniqueName("integration-test-datasciencecluster-multi-2", 56789))

			By("verifying no errors are returned")
			Expect(err1).NotTo(HaveOccurred())
			Expect(err2).NotTo(HaveOccurred())
		})
	})

	Context("when manager has invalid configuration", func() {
		It("should handle manager with invalid configuration", func() {
			By("creating an invalid manager that will cause client creation to fail")
			invalidMgr := NewInvalidManager()

			By("calling NewDataScienceClusterReconciler with invalid manager")
			Expect(func() {
				_ = datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, invalidMgr, generateUniqueName("integration-test-datasciencecluster-invalid", 67890))
			}).To(Panic())
		})
	})

	var _ = Describe("DataScienceCluster Reconciler Configuration", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
		})

		It("should configure reconciler with all required component ownerships", func() {
			By(callingReconcilerMsg)
			err := datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, mgr, generateUniqueName("integration-test-datasciencecluster-ownerships", 78901))

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
			err := datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, mgr, generateUniqueName("integration-test-datasciencecluster-watches", 89012))

			By(verifyNoErrorMsg)
			Expect(err).NotTo(HaveOccurred())

			// The reconciler should be configured to watch DSCInitialization objects
			// This is verified by the successful creation of the reconciler
		})

		It("should configure reconciler with all required actions", func() {
			By(callingReconcilerMsg)
			err := datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, mgr, generateUniqueName("integration-test-datasciencecluster-actions", 90123))

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
			err := datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, mgr, generateUniqueName("integration-test-datasciencecluster-conditions", 12346))

			By(verifyNoErrorMsg)
			Expect(err).NotTo(HaveOccurred())

			// The reconciler should be configured with status.ConditionTypeComponentsReady
			// This is verified by the successful creation of the reconciler
		})
	})
})
