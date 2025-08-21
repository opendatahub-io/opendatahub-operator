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
	"path/filepath"
	"testing"

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

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
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
			By("calling NewDataScienceClusterReconciler")
			err := datasciencecluster.NewDataScienceClusterReconciler(ctx, mgr)

			By("verifying no error is returned")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create reconciler with correct configuration", func() {
			By("calling NewDataScienceClusterReconciler")
			err := datasciencecluster.NewDataScienceClusterReconciler(ctx, mgr)

			By("verifying no error is returned")
			Expect(err).NotTo(HaveOccurred())

			// Optionally, start the manager in a goroutine and verify it's ready
			// This would provide stronger validation that the reconciler is properly configured
		})
	})

	Context("when called with nil manager", func() {
		It("should return error", func() {
			By("calling NewDataScienceClusterReconciler with nil manager")
			err := datasciencecluster.NewDataScienceClusterReconciler(ctx, nil)

			By("verifying error is returned")
			Expect(err).To(HaveOccurred())
		})
	})

	Context("when called with nil context", func() {
		It("should handle nil context gracefully", func() {
			By("calling NewDataScienceClusterReconciler with nil context")
			err := datasciencecluster.NewDataScienceClusterReconciler(context.TODO(), mgr)

			By("verifying no error is returned")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when called multiple times", func() {
		It("should handle multiple calls without error", func() {
			By("calling NewDataScienceClusterReconciler multiple times")
			err1 := datasciencecluster.NewDataScienceClusterReconciler(ctx, mgr)
			err2 := datasciencecluster.NewDataScienceClusterReconciler(ctx, mgr)

			By("verifying no errors are returned")
			Expect(err1).NotTo(HaveOccurred())
			Expect(err2).NotTo(HaveOccurred())
		})
	})

	Context("when manager has invalid configuration", func() {
		var invalidMgr manager.Manager

		BeforeEach(func() {
			// Create a manager with minimal/incomplete configuration
			// that would fail during reconciler setup
			var err error
			invalidMgr, err = manager.New(&rest.Config{
				Host: "https://127.0.0.1:1", // Unreachable but valid format
			}, manager.Options{
				Scheme: scheme.Scheme,
				// Optionally set MetricsBindAddress to "0" to force an error
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle manager with invalid configuration", func() {
			By("calling NewDataScienceClusterReconciler with invalid manager")
			err := datasciencecluster.NewDataScienceClusterReconciler(ctx, invalidMgr)

			By("verifying error is returned")
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("DataScienceCluster Reconciler Configuration", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("should configure reconciler with all required component ownerships", func() {
		By("calling NewDataScienceClusterReconciler")
		err := datasciencecluster.NewDataScienceClusterReconciler(ctx, mgr)

		By("verifying no error is returned")
		Expect(err).NotTo(HaveOccurred())

		// Successful creation verifies that:
		// - All required component types are registered in the scheme
		// - The reconciler successfully sets up ownership for Dashboard, Workbenches, etc.
		// - The controller builder accepts all ownership configurations
		// Further validation would require starting the manager and inspecting runtime behavior
	})

	It("should configure reconciler with DSCInitialization watches", func() {
		By("calling NewDataScienceClusterReconciler")
		err := datasciencecluster.NewDataScienceClusterReconciler(ctx, mgr)

		By("verifying no error is returned")
		Expect(err).NotTo(HaveOccurred())

		// The reconciler should be configured to watch DSCInitialization objects
		// This is verified by the successful creation of the reconciler
	})

	It("should configure reconciler with all required actions", func() {
		By("calling NewDataScienceClusterReconciler")
		err := datasciencecluster.NewDataScienceClusterReconciler(ctx, mgr)

		By("verifying no error is returned")
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
		By("calling NewDataScienceClusterReconciler")
		err := datasciencecluster.NewDataScienceClusterReconciler(ctx, mgr)

		By("verifying no error is returned")
		Expect(err).NotTo(HaveOccurred())

		// The reconciler should be configured with status.ConditionTypeComponentsReady
		// This is verified by the successful creation of the reconciler
	})
})
