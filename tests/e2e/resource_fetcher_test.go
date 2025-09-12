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
func fetchResource(ro *ResourceOptions) (*unstructured.Unstructured, error) {
	// Retry logic to fetch the resource with appropriate error handling.
	var u *unstructured.Unstructured
	var fetchErr error

	ro.tc.g.Eventually(func(g Gomega) {
		// Fetch the resource
		// For NotFound: u = nil, fetchErr = nil
		u, fetchErr = ro.tc.g.Get(ro.GVK, ro.NN).Get()

		// Check if AcceptableErr is provided and match it if encountered
		if ro.AcceptableErrMatcher != nil && fetchErr != nil {
			g.Expect(fetchErr).To(ro.AcceptableErrMatcher, unexpectedErrorMismatchMsg, ro.AcceptableErrMatcher, fetchErr, ro.GVK.Kind)
			return // Acceptable error matched, exit successfully
		}

		// For any other errors, retry
		g.Expect(fetchErr).NotTo(
			HaveOccurred(),
			defaultErrorMessageIfNone(resourceFetchErrorMsg, []any{ro.ResourceID, ro.GVK.Kind, fetchErr}, ro.CustomErrorArgs)...,
		)
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
func fetchResources(ro *ResourceOptions) ([]unstructured.Unstructured, error) {
	var resourcesList []unstructured.Unstructured
	var fetchErr error

	ro.tc.g.Eventually(func(g Gomega) {
		// Attempt to retrieve the list of resources
		resourcesList, fetchErr = ro.tc.g.List(ro.GVK, ro.ListOptions).Get()

		// Check if AcceptableErr is provided and match it if encountered
		if ro.AcceptableErrMatcher != nil && fetchErr != nil {
			g.Expect(fetchErr).To(ro.AcceptableErrMatcher, unexpectedErrorMismatchMsg, ro.AcceptableErrMatcher, fetchErr, ro.GVK.Kind)
			return // Acceptable error matched, exit successfully
		}

		// For transient errors that aren't "not found", retry
		// "Not found" errors will be handled by the caller based on IgnoreNotFound
		if !errors.IsNotFound(fetchErr) {
			g.Expect(fetchErr).NotTo(
				HaveOccurred(),
				defaultErrorMessageIfNone(resourceFetchErrorMsg, []any{ro.ResourceID, ro.GVK.Kind, fetchErr}, ro.CustomErrorArgs)...,
			)
		}
	}).Should(Succeed())

	return resourcesList, fetchErr
}
