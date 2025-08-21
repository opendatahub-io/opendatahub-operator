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

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/datasciencecluster"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNewDataScienceClusterReconciler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DataScienceCluster Controller Unit Tests")
}

// createTestManager returns a manager.Manager interface for testing.
// This is acceptable here since we're testing the reconciler's interaction with the manager interface,
// not the concrete implementation details.
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

	mgr, err := manager.New(&rest.Config{}, manager.Options{
		Scheme: testScheme,
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
			err := datasciencecluster.NewDataScienceClusterReconciler(ctx, mgr)

			By("verifying no error is returned")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when called with nil manager", func() {
		It("should panic", func() {
			By("calling NewDataScienceClusterReconciler with nil manager")
			Expect(func() {
				_ = datasciencecluster.NewDataScienceClusterReconciler(ctx, nil)
			}).To(Panic())
		})
	})
})
