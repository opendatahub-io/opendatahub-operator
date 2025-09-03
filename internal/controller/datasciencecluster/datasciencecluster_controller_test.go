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
	"strings"
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

const (
	verifyingNoErrorReturned = "verifying no error is returned"
	callingWithInvalidName   = "calling NewDataScienceClusterReconcilerWithName with invalid instanceName"
	verifyingErrorReturned   = "verifying error is returned"
	callingWithValidName     = "calling NewDataScienceClusterReconcilerWithName with valid instanceName"
)

// Test contract: NewDataScienceClusterReconcilerWithName accepts empty strings as valid input.
// Empty strings mean "use default instance name" (the lowercase GVK Kind) and bypass DNS-1123 validation.
// This is explicitly allowed to support the default behavior when no custom name is provided.

func TestNewDataScienceClusterReconciler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DataScienceCluster Controller Suite")
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

	// Create a unique manager for each test to avoid controller name conflicts
	mgr, err := manager.New(&rest.Config{
		Host: "https://127.0.0.1:65535",
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
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
			By("calling NewDataScienceClusterReconcilerWithName")
			mgr := createTestManager()
			err := datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, mgr, "test-datasciencecluster")

			By(verifyingNoErrorReturned)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when called with nil manager", func() {
		It("should panic", func() {
			By("calling NewDataScienceClusterReconcilerWithName with nil manager")
			Expect(func() {
				_ = datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, nil, "test-datasciencecluster")
			}).To(Panic())
		})
	})
	Context("when called with invalid instanceName", func() {
		DescribeTable("returns an error",
			func(name string) {
				By(callingWithInvalidName)
				mgr := createTestManager()
				err := datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, mgr, name)
				By(verifyingErrorReturned)
				Expect(err).To(HaveOccurred())
				// Use a more robust assertion that checks for DNS-1123 validation errors
				// The error should contain either "invalid instanceName" or DNS-1123 validation messages
				Expect(err.Error()).To(Or(
					ContainSubstring("invalid instanceName"),
					MatchRegexp(`.*DNS.*1123.*`),
					ContainSubstring("must consist of lower case alphanumeric characters"),
					ContainSubstring("must start with an alphanumeric character"),
					ContainSubstring("must end with an alphanumeric character"),
					ContainSubstring("must be no more than 63 characters"),
				))
			},
			Entry("starts with hyphen", "-invalid-name"),
			Entry("ends with hyphen", "invalid-name-"),
			Entry("contains uppercase", "Invalid-Name"),
			Entry("contains special char @", "invalid@name"),
			Entry("contains underscore", "invalid_name"),
			Entry("contains dot", "invalid.name"),
			Entry("too long (>63)", strings.Repeat("a", 64)),
		)
	})

	Context("when called with valid instanceName", func() {
		DescribeTable("succeeds",
			func(name string) {
				By(callingWithValidName)
				mgr := createTestManager()
				err := datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, mgr, name)
				By(verifyingNoErrorReturned)
				Expect(err).NotTo(HaveOccurred())
			},
			// Empty string is explicitly allowed and means "use default GVK Kind"
			// This bypasses DNS-1123 validation since the default name will be validated separately
			Entry("empty string (uses default GVK Kind)", ""),
			Entry("lowercase alphanumeric", "validname"),
			Entry("lowercase with hyphen", "valid-name"),
			Entry("single character", "a"),
			Entry("numeric", "123"),
			Entry("double hyphen", "a--b"),
			Entry("max length (63)", strings.Repeat("a", 63)),
		)
	})
})
