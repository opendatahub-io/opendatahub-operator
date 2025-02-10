package e2e_test

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	. "github.com/onsi/gomega"
)

// fetchResource attempts to retrieve a single Kubernetes resource, retrying automatically until success or timeout.
//
// It ensures that transient failures or delays in resource creation do not cause test flakiness
// by using Gomega's Eventually mechanism.
//
// Parameters:
//   - ro (*ResourceOptions): Contains details about the resource, including GVK, NamespacedName (NN),
//     expected error conditions, and custom assertion messages.
//
// Returns:
//   - *unstructured.Unstructured: The retrieved resource if found; otherwise, nil.
//   - error: The error encountered during retrieval, if any.
func (tc *TestContext) fetchResource(ro *ResourceOptions) (*unstructured.Unstructured, error) {
	// Retry logic to fetch the resource with appropriate error handling.
	var u *unstructured.Unstructured
	var fetchErr error

	tc.g.Eventually(func(g Gomega) {
		// Fetch the resource
		u, fetchErr = tc.g.Get(ro.GVK, ro.NN).Get()

		// Check if ExpectedErr is provided and match it if encountered
		if ro.ExpectedErr != nil && fetchErr != nil {
			g.Expect(fetchErr).To(MatchError(ro.ExpectedErr), unexpectedErrorMismatchMsg, ro.ExpectedErr, fetchErr, ro.GVK.Kind)
		}

		// If the resource is not found, we set the object to nil
		if errors.IsNotFound(fetchErr) {
			u = nil
		}
	}).Should(Succeed())

	return u, fetchErr
}

// fetchResources retrieves a list of Kubernetes resources, retrying automatically until success or timeout.
//
// It ensures transient issues do not cause test failures by using Gomega's Eventually mechanism.
//
// Parameters:
//   - ro (*ResourceOptions): Contains details about the resources, including GVK, NamespacedName (NN),
//     list filtering options, and custom assertion messages.
//
// Returns:
//   - []unstructured.Unstructured: A list of retrieved resources, which may be empty if none exist.
//   - error: The error encountered during retrieval, if any.
func (tc *TestContext) fetchResources(ro *ResourceOptions) ([]unstructured.Unstructured, error) {
	var resourcesList []unstructured.Unstructured
	var fetchErr error

	tc.g.Eventually(func(g Gomega) {
		// Attempt to retrieve the list of resources
		resourcesList, fetchErr = tc.g.List(ro.GVK, ro.ListOptions).Get()

		// Check if ExpectedErr is provided and match it if encountered
		if ro.ExpectedErr != nil && fetchErr != nil {
			g.Expect(fetchErr).To(MatchError(ro.ExpectedErr), unexpectedErrorMismatchMsg, ro.ExpectedErr, fetchErr, ro.GVK.Kind)
		}

		// Ensure no unexpected errors occurred during retrieval
		g.Expect(fetchErr).NotTo(
			HaveOccurred(),
			defaultErrorMessageIfNone(resourceFetchErrorMsg, []any{ro.ResourceID, ro.GVK.Kind, fetchErr}, ro.CustomErrorArgs)...,
		)
	}).Should(Succeed())

	return resourcesList, fetchErr
}

// ensureResourceExistsOrNil retrieves a Kubernetes resource, retrying until it is found or the timeout expires.
// If the resource exists, it returns the object. If not found, it returns nil without failing the test.
// Unexpected errors will fail the test.
//
// Parameters:
//   - ro (*ResourceOptions): Metadata and retrieval logic for the resource.
//
// Returns:
//   - *unstructured.Unstructured: The resource if found, otherwise nil.
//   - error: Any error encountered during retrieval.
func (tc *TestContext) ensureResourceExistsOrNil(ro *ResourceOptions) (*unstructured.Unstructured, error) {
	// Fetch the resource using fetchResource.
	u, err := tc.fetchResource(ro)

	// Ensure no unexpected errors occurred while fetching the resource
	tc.g.Expect(err).NotTo(
		HaveOccurred(),
		defaultErrorMessageIfNone(resourceFetchErrorMsg, []any{ro.ResourceID, ro.GVK.Kind, err}, ro.CustomErrorArgs)...,
	)

	// Return the resource or nil if it wasn't found
	return u, err
}

// ensureResourceDoesNotExist attempts to retrieve a Kubernetes resource and checks if it does not exist.
// It uses Gomega assertions to ensure the resource is not found and fails the test if it is found.
//
// Parameters:
//   - g (Gomega): The Gomega assertion wrapper.
//   - ro (*ResourceOptions): Metadata and retrieval logic for the resource.
//
// Returns:
//   - error: An error if the resource is found, or nil if the resource does not exist.
func (tc *TestContext) ensureResourceDoesNotExist(g Gomega, ro *ResourceOptions) error {
	// Fetch the resource using fetchResource.
	u, err := tc.fetchResource(ro)

	// Assert that the resource is not found.
	g.Expect(u).To(BeNil(), resourceFoundErrorMsg, ro.ResourceID, ro.GVK.Kind)

	// Return the error encountered during resource retrieval, if any.
	return err
}

// ensureResourcesDoNotExist is a helper function that retrieves a list of Kubernetes resources
// and checks if the resources do not exist (i.e., the list is empty). It performs an assertion
// to ensure that the list of resources is empty. If any resources are found, it fails the test.
//
// Parameters:
//   - g (Gomega): The Gomega assertion wrapper.
//   - ro (*ResourceOptions): Metadata and retrieval logic for the resource.
//
// Returns:
//   - error: An error if the resource is found in the cluster, or nil if the resource does not exist.
func (tc *TestContext) ensureResourcesDoNotExist(g Gomega, ro *ResourceOptions) error {
	resourcesList, err := tc.fetchResources(ro)

	// Ensure that the resources list is empty (resources should not exist)
	g.Expect(resourcesList).To(BeEmpty(), resourceListNotEmptyErrorMsg, ro.ResourceID, ro.GVK.Kind)

	return err
}
