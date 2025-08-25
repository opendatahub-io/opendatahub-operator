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
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/datasciencecluster"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNewDataScienceClusterReconciler(t *testing.T) {
	RegisterFailHandler(Fail)
}

// createTestManager returns a manager.Manager interface for testing.
// This is acceptable here since we're testing the reconciler's interaction with the manager interface,
// not the concrete implementation details.
//
//nolint:ireturn
func createTestManager() manager.Manager {
	// Create a new scheme for each test to avoid conflicts
	testScheme := runtime.NewScheme()

	// Add all required schemes
	err := dscv1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = dsciv1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = componentApi.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())

	// Create a unique manager for each test to avoid controller name conflicts
	mgr, err := manager.New(&rest.Config{
		Host: "http://127.0.0.1:65535",
	}, manager.Options{
		Scheme:                 testScheme,
		Metrics:                server.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	Expect(err).NotTo(HaveOccurred())
	return mgr
}

var _ = Describe("NewDataScienceClusterReconciler", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Context("when called with valid manager", func() {
		It("should successfully create reconciler without error", func() {
			By("calling NewDataScienceClusterReconciler")
			mgr := createTestManager()
			err := datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, mgr, "test-datasciencecluster")

			By("verifying no error is returned")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when called with nil manager", func() {
		It("should panic", func() {
			By("calling NewDataScienceClusterReconciler with nil manager")
			Expect(func() {
				_ = datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, nil, "test-datasciencecluster")
			}).To(Panic())
		})
	})
})
