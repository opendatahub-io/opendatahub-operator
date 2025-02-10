package e2e_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/matchers"
	gTypes "github.com/onsi/gomega/types"
	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	modelregistryctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

var NoOpMutationFn = func(obj *unstructured.Unstructured) error {
	return nil // No operation, just return nil
}

// Namespace and Operator Constants.
const (
	// Namespaces for various components.
	knativeServingNamespace = "knative-serving" // Namespace for Knative Serving components

	// Operators constants.
	serviceMeshOpName            = "servicemeshoperator" // Name of the Service Mesh Operator
	serverlessOpName             = "serverless-operator" // Name of the Serverless Operator
	authorinoOpName              = "authorino-operator"  // Name of the Serverless Operator
	serviceMeshControlPlane      = "data-science-smcp"   // Service Mesh control plane name
	serviceMeshNamespace         = "istio-system"        // Namespace for Istio Service Mesh control plane
	serviceMeshMetricsCollection = "Istio"               // Metrics collection for Service Mesh (e.g., Istio)
	serviceMeshMemberName        = "default"
)

// Timeout & Interval Constants.
const (
	generalRetryInterval = 10 * time.Second // Retry interval for general operations

	defaultEventuallyTimeout        = 5 * time.Minute  // Set default timeout for Eventually (default is 1 second).
	defaultEventuallyPollInterval   = 2 * time.Second  // Set default timeout for Eventually (default is 1 second).
	defaultConsistentlyDuration     = 10 * time.Second // Set default duration for Consistently (default is 2 seconds).
	defaultConsistentlyPollInterval = 2 * time.Second  // Set default polling interval for Consistently (default is 50ms).

	eventuallyTimeoutMedium = 7 * time.Minute  // Medium timeout for readiness checks (e.g., CSV, DSC)
	eventuallyTimeoutLong   = 10 * time.Minute // Longer timeout for more complex readiness (e.g., DSCInitialization, KServe)
)

// Configuration and Miscellaneous Constants.
const (
	ownedNamespaceNumber = 1 // Number of namespaces owned, adjust to 4 for RHOAI deployment

	dsciInstanceName = "e2e-test-dsci" // Instance name for the DSCInitialization
	dscInstanceName  = "e2e-test-dsc"  // Instance name for the DataScienceCluster

	// Standard error messages format.
	resourceNotNilErrorMsg     = "Expected a non-nil resource object but got nil."
	resourceNotFoundErrorMsg   = "Expected resource '%s' of kind '%s' to exist, but it was not found or could not be retrieved."
	resourceFoundErrorMsg      = "Expected resource '%s' of kind '%s' to not exist, but it was found."
	resourceEmptyErrorMsg      = "Expected resource list '%s' of kind '%s' to contain resources, but it was empty."
	resourceNotEmptyErrorMsg   = "Expected resource list '%s' of kind '%s' to be empty, but it contains resources."
	resourceFetchErrorMsg      = "Error occurred while fetching the resource '%s' of kind '%s': %v"
	unexpectedErrorMismatchMsg = "Expected error '%v' to match the actual error '%v' for resource of kind '%s'."
)

// RunTestCases runs a series of test cases, optionally in parallel based on the provided options.
//
// Parameters:
//   - t (*testing.T): The test context passed into the test function.
//   - testCases ([]TestCase): A slice of test cases to execute.
//   - opts (...TestCaseOption): Optional configuration options, like enabling parallel execution.
func (tc *TestContext) RunTestCases(t *testing.T, testCases []TestCase, opts ...TestCaseOption) {
	t.Helper()

	// Apply all provided options (e.g., parallel execution) to each test case.
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// Apply each option to the current test
			for _, opt := range opts {
				opt(t)
			}

			// Run the test function for the current test case
			testCase.testFn(t)
		})
	}
}

// TestCaseOption defines a function type that can be used to modify how individual test cases are executed.
type TestCaseOption func(t *testing.T)

// WithParallel is an option that marks test cases to run in parallel.
func WithParallel() TestCaseOption {
	return func(t *testing.T) {
		t.Helper()

		t.Parallel() // Marks the test case to run in parallel with other tests
	}
}

// ResourceOption defines a function type for constructing or retrieving an unstructured Kubernetes resource.
type ResourceOption func(tc *TestContext) *unstructured.Unstructured

// WithObjectToCreate creates a ResourceOption that converts any object to an unstructured object.
// This is used when the object doesn't exist yet and needs to be created (or updated).
func WithObjectToCreate(obj client.Object) ResourceOption {
	return func(tc *TestContext) *unstructured.Unstructured {
		// Convert the input object to unstructured
		u, err := resources.ObjectToUnstructured(tc.Scheme(), obj)
		tc.g.Expect(err).NotTo(HaveOccurred())

		return u
	}
}

// WithFetchedObject returns a ResourceOption that fetches the resource by GroupVersionKind and NamespacedName.
// It calls `EnsureResourceExists` internally to retrieve the resource for updating or patching.
func WithFetchedObject(gvk schema.GroupVersionKind, nn types.NamespacedName) ResourceOption {
	return func(tc *TestContext) *unstructured.Unstructured {
		return tc.EnsureResourceExists(gvk, nn)
	}
}

// WithMinimalObject creates a ResourceOption that creates an unstructured object with only the specified
// GroupVersionKind, Namespace, and Name. This is useful for cases where only minimal resource details are needed,
// like namespaces or certain simple resource types.
func WithMinimalObject(gvk schema.GroupVersionKind, nn types.NamespacedName) ResourceOption {
	return func(tc *TestContext) *unstructured.Unstructured {
		// Create a new unstructured object and set the necessary fields
		u := resources.GvkToUnstructured(gvk) // Set the GroupVersionKind
		u.SetNamespace(nn.Namespace)          // Set the Namespace
		u.SetName(nn.Name)                    // Set the Name

		// Return the object with only the essential fields set
		return u
	}
}

// WithMinimalObjectFrom creates a ResourceOption that creates an unstructured object with the minimal required fields
// (GroupVersionKind, Namespace, Name) extracted from the provided object.
func WithMinimalObjectFrom(obj client.Object) ResourceOption {
	return func(tc *TestContext) *unstructured.Unstructured {
		// Extract the GroupVersionKind (GVK) from the object
		groupVersionKind := obj.GetObjectKind().GroupVersionKind()

		// Create a new unstructured object with the extracted GVK
		u := resources.GvkToUnstructured(groupVersionKind)

		// Set the Namespace and Name from the provided object
		u.SetNamespace(obj.GetNamespace())
		u.SetName(obj.GetName())

		// Return the object with only the essential fields set (GVK, Namespace, Name)
		return u
	}
}

// OverrideEventuallyTimeout temporarily changes the Eventually timeout and polling period.
func (tc *TestContext) OverrideEventuallyTimeout(timeout, pollInterval time.Duration) func() {
	// Save current timeout values (you'll need to store these manually)
	previousTimeout := tc.g.DurationBundle.EventuallyTimeout
	previousPollInterval := tc.g.DurationBundle.EventuallyPollingInterval

	// Override with new values
	tc.g.SetDefaultEventuallyTimeout(timeout)
	tc.g.SetDefaultConsistentlyPollingInterval(pollInterval)

	// Return a function to reset them back
	return func() {
		// Override with new values
		tc.g.SetDefaultEventuallyTimeout(previousTimeout)
		tc.g.SetDefaultConsistentlyPollingInterval(previousPollInterval)
	}
}

// EnsureResourceExistsOrNil attempts to retrieve a specific Kubernetes resource from the cluster.
// It retries fetching the resource until the retry window expires. If the resource exists, it returns it.
// If the resource does not exist, it returns nil and does not fail the test, which is useful when subsequent actions
// (such as creating the resource) are intended.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource being checked.
//   - nn (types.NamespacedName): The namespace and name of the resource.
//   - args (...interface{}): Optional Gomega assertion message arguments. If none are provided, a default message
//     is used for failure when the resource is expected to exist but cannot be found.
//
// Returns:
//   - *unstructured.Unstructured: The resource object if it exists, or nil if not found.
func (tc *TestContext) EnsureResourceExistsOrNil(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	args ...any,
) (*unstructured.Unstructured, error) {
	// Construct a resource identifier.
	resourceID := resources.FormatNamespacedName(nn)

	// Attempt to get the resource with retries
	var u *unstructured.Unstructured
	var err error
	tc.g.Eventually(func(g Gomega) {
		// Fetch the resource
		u, err = tc.g.Get(gvk, nn).Get()

		// Explicitly set to nil if the resource is not found
		if errors.IsNotFound(err) {
			u = nil
			return
		}

		// Ensure no unexpected errors occurred while fetching the resource
		g.Expect(err).ToNot(HaveOccurred(),
			appendDefaultIfNeeded(resourceFetchErrorMsg, []any{resourceID, gvk.Kind, err}, args)...,
		)
	}).Should(Succeed())

	// Return the resource or nil if it wasn't found
	return u, err
}

// EnsureResourceExists verifies whether a specific Kubernetes resource exists by checking its presence in the cluster.
// If the resource doesn't exist, it will fail the test with an error message.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource being checked.
//   - nn (types.NamespacedName): The namespace and name of the resource.
//   - args (...interface{}): Optional Gomega assertion message arguments. If none are provided, a default message is used.
//
// Returns:
//   - *unstructured.Unstructured: The resource object if it exists.
func (tc *TestContext) EnsureResourceExists(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	args ...any,
) *unstructured.Unstructured {
	// Construct a resource identifier.
	resourceID := resources.FormatNamespacedName(nn)

	// Use EnsureResourceExistsOrNil to attempt to fetch the resource with retries
	u, _ := tc.EnsureResourceExistsOrNil(gvk, nn)

	// Ensure that the resource object is not nil
	tc.g.Expect(u).NotTo(BeNil(),
		appendDefaultIfNeeded(resourceNotFoundErrorMsg, []any{resourceID, gvk.Kind}, args)...)

	return u
}

// EnsureResourceExistsAndMatchesCondition ensures that the resource exists and matches the given condition.
// Callers should explicitly use `Not(matcher)` if they need to assert a negative condition.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource being checked.
//   - nn (types.NamespacedName): The namespace and name of the resource (used for filtering).
//   - condition: A Gomega matcher specifying the expected condition (e.g., BeEmpty(), Not(BeEmpty()), jq.Match()).
//   - args (optional): Gomega assertion message arguments. If not provided, a default message is used.
//
// Returns:
//   - *unstructured.Unstructured: The resource that was found and matched.
func (tc *TestContext) EnsureResourceExistsAndMatchesCondition(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	condition gTypes.GomegaMatcher,
	args ...any,
) *unstructured.Unstructured {
	var u *unstructured.Unstructured

	tc.g.Eventually(func(g Gomega) {
		// Ensure the resource exists by using EnsureResourceExists
		u = tc.EnsureResourceExists(gvk, nn)

		// Get GroupVersionKind from the resource.
		groupVersionKind := u.GetObjectKind().GroupVersionKind()

		// Construct a resource identifier.
		resourceID := resources.FormatUnstructuredName(u)

		// Apply the provided condition matcher to the resource.
		applyMatchers(g, resourceID, groupVersionKind, u, nil, condition, args)
	}).Should(Succeed())

	return u
}

// EnsureResourceExistsAndMatchesConditionConsistently verifies that a Kubernetes resource exists and
// consistently matches a specified condition over a period of time.
//
// It repeatedly checks the resource using the provided condition for the specified `timeout` and `polling`
// intervals, and ensures that the condition holds true consistently within the given time frame.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource to be checked.
//   - nn (types.NamespacedName): The namespaced name (namespace + name) of the resource.
//   - condition: A Gomega matcher specifying the expected condition (e.g., BeEmpty(), Not(BeEmpty()), jq.Match()).
//   - timeout (time.Duration): The maximum time to wait for the condition to be true consistently.
//   - polling (time.Duration): The interval to poll for checking the condition.
//
// Optional Arguments:
//   - If `timeout` and `polling` are not provided, default values will be used.
//     Default timeout: 10 seconds
//     Default polling interval: 2 second
//
// Returns:
//   - *unstructured.Unstructured: The resource that was found and matched.
func (tc *TestContext) EnsureResourceExistsAndMatchesConditionConsistently(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	condition gTypes.GomegaMatcher,
	timeout time.Duration,
	polling time.Duration,
	args ...any,
) {
	// Set default values for timeout and polling if not provided
	if timeout == 0 {
		timeout = defaultConsistentlyDuration // default timeout if not provided
	}
	if polling == 0 {
		polling = defaultConsistentlyPollInterval // default polling interval if not provided
	}

	// Ensure the resource exists and matches the condition consistently over the specified period.
	tc.g.Consistently(func(g Gomega) {
		// Retrieve the resource
		u := tc.EnsureResourceExists(gvk, nn)

		// Get GroupVersionKind from the resource.
		groupVersionKind := u.GetObjectKind().GroupVersionKind()

		// Construct a resource identifier.
		resourceID := resources.FormatUnstructuredName(u)

		// Apply the condition to the resource and assert that it matches
		applyMatchers(g, resourceID, groupVersionKind, u, nil, condition, args)
	}, timeout, polling)
}

// EnsureResourceDoesNotExist verifies whether a specific Kubernetes resource does not exist by checking its presence in the cluster.
// If the resource exists, it will fail the test with an error message.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource being checked.
//   - nn (types.NamespacedName): The namespace and name of the resource.
//   - args (...interface{}): Optional Gomega assertion message arguments. If none are provided, a default message is used.
func (tc *TestContext) EnsureResourceDoesNotExist(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	args ...any,
) {
	_ = tc.ensureResourceDoesNotExist(tc.g, gvk, nn, args)
}

// EnsureResourceDoesNotExistAndErrorMatches ensures that the given resource does not exist
// and that the error encountered while retrieving it matches the expected error.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource to check.
//   - nn (types.NamespacedName): The namespaced name of the resource.
//   - expectedErr (error): The expected error (e.g., &meta.NoKindMatchError{}).
func (tc *TestContext) EnsureResourceDoesNotExistAndErrorMatches(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	expectedErr error,
	args ...any,
) {
	err := tc.ensureResourceDoesNotExist(tc.g, gvk, nn, args)

	// Ensure the error matches the expected condition.
	tc.g.Expect(err).To(MatchError(expectedErr), unexpectedErrorMismatchMsg, expectedErr, err, gvk.Kind)
}

// EnsureResourceGone waits for the specified resource to disappear or until a timeout occurs.
// It retries checking the resource at regular intervals and fails if the resource is still found after the timeout.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource to check.
//   - nn (types.NamespacedName): The namespace and name of the resource.
//   - args (...interface{}): Optional Gomega assertion message arguments.
func (tc *TestContext) EnsureResourceGone(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	args ...any,
) {
	// Use Eventually to retry checking the resource until it disappears or timeout occurs
	tc.g.Eventually(func(g Gomega) {
		_ = tc.ensureResourceDoesNotExist(g, gvk, nn, args)
	}).Should(Succeed())
}

// EnsureResourceIsGoneAndErrorMatches ensures that the given resource is eventually removed from the cluster
// and that the error encountered while retrieving it matches the expected error.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource to check.
//   - nn (types.NamespacedName): The namespaced name of the resource.
//   - expectedErr (error): The expected error (e.g., &meta.NoKindMatchError{}).
func (tc *TestContext) EnsureResourceIsGoneAndErrorMatches(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	expectedErr error,
	args ...any,
) {
	// Use Eventually to retry checking the resource until it disappears or timeout occurs
	tc.g.Eventually(func(g Gomega) {
		err := tc.ensureResourceDoesNotExist(g, gvk, nn, args)

		// Ensure the error matches the expected condition.
		g.Expect(err).To(MatchError(expectedErr), unexpectedErrorMismatchMsg, expectedErr, err, gvk.Kind)
	}).Should(Succeed())
}

// EnsureResourcesExist verifies whether a list of specific Kubernetes resources exists in the cluster.
// It waits for the resources to appear and fails the test with a message if any resource is not found within the timeout.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource being checked.
//   - nn (types.NamespacedName): The namespace and name of the resource.
//   - listOptions (*client.ListOptions): Optional list options like label selectors or other filters.
//   - args (...interface{}): Optional Gomega assertion message arguments. If none are provided, a default message is used.
//
// Returns:
//   - []unstructured.Unstructured: The list of resources if they exist.
func (tc *TestContext) EnsureResourcesExist(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	listOptions *client.ListOptions,
	args ...any,
) []unstructured.Unstructured {
	// Construct a resource identifier
	resourceID := resources.FormatNamespacedName(nn)

	resourcesList := tc.RetrieveResources(gvk, nn, listOptions, args...)

	// Ensure that the resources list is not empty
	tc.g.Expect(resourcesList).NotTo(BeEmpty(), resourceEmptyErrorMsg, resourceID, gvk.Kind)

	return resourcesList
}

// EnsureResourcesExistAndMatchCondition verifies whether a list of resources of a specific kind exists
// in the cluster, and ensures that the list matches the specified condition (e.g., length, owner references, etc.).
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource being checked.
//   - nn (types.NamespacedName): The namespace and name of the resource.
//   - listOptions (*client.ListOptions): Optional list options like label selectors or other filters.
//   - condition gTypes.GomegaMatcher: The condition to check the list of resources against (e.g., owner references, length).
//   - args (...interface{}): Optional Gomega assertion message arguments.
//
// Returns:
//   - []unstructured.Unstructured: The list of resources that match the condition.
func (tc *TestContext) EnsureResourcesExistAndMatchCondition(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	listOptions *client.ListOptions,
	condition gTypes.GomegaMatcher,
	args ...any,
) []unstructured.Unstructured {
	var resourcesList []unstructured.Unstructured

	tc.g.Eventually(func(g Gomega) {
		// Construct a resource identifier
		resourceID := resources.FormatNamespacedName(nn)

		// First, ensure that the resources exist using EnsureResourcesExist
		resourcesList = tc.EnsureResourcesExist(gvk, nn, listOptions, args...)

		// Apply the provided condition matcher to the resource.
		applyMatchers(g, resourceID, gvk, resourcesList, nil, condition, args)
	}).Should(Succeed())

	return resourcesList
}

// EnsureResourcesDoNotExist ensures that the resources for a given GVK and namespace do not exist in the cluster.
//
// This function uses `Eventually` to ensure that no resources are found matching the given criteria,
// verifying that the resources do not exist or have been removed successfully.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resources to ensure do not exist.
//   - nn (types.NamespacedName): The namespace and name of the resource(s) to check.
//   - listOptions (*client.ListOptions, optional): Optional list options (e.g., label selectors) for filtering the resources.
//   - args (...any, optional): Optional arguments for error messages or logging.
func (tc *TestContext) EnsureResourcesDoNotExist(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	listOptions *client.ListOptions,
	args ...any,
) {
	tc.ensureResourcesDoNotExist(tc.g, gvk, nn, listOptions, args)
}

// EnsureResourcesGone waits for the list of resources to disappear or until a timeout occurs.
// It retries checking the resources at regular intervals and fails if any resource is still found after the timeout.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resources to check.
//   - nn (types.NamespacedName): The namespace and name of the resource(s).
//   - listOptions (*client.ListOptions, optional): Optional list options for filtering the resources (e.g., label selectors).
//   - args (...interface{}): Optional Gomega assertion message arguments.
func (tc *TestContext) EnsureResourcesGone(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	listOptions *client.ListOptions,
	args ...any,
) {
	// Use Eventually to retry checking the resource until it disappears or timeout occurs
	tc.g.Eventually(func(g Gomega) {
		tc.ensureResourcesDoNotExist(g, gvk, nn, listOptions, args)
	}).Should(Succeed())
}

// EnsureExactlyOneResourceExists verifies that exactly one instance of a specific Kubernetes resource
// exists in the cluster. If there are none or more than one, it fails the test.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource being checked.
//   - nn (types.NamespacedName): The namespace and name of the resource (used for filtering).
//   - args (...interface{}): Optional Gomega assertion message arguments. Defaults to a standard error message.
//
// Returns:
//   - *unstructured.Unstructured: The resource object if exactly one instance exists.
func (tc *TestContext) EnsureExactlyOneResourceExists(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	args ...any,
) *unstructured.Unstructured {
	// Construct a resource identifier.
	resourceID := resources.FormatNamespacedName(nn)

	// Use Eventually to retry getting the resources until they appears
	var objList []unstructured.Unstructured
	tc.g.Eventually(func(g Gomega) {
		// Fetch the resources
		var err error
		objList, err = tc.g.List(gvk).Get()

		// Ensure no error occurred when fetching resources
		g.Expect(err).NotTo(HaveOccurred(), appendDefaultIfNeeded(
			resourceFetchErrorMsg,
			[]any{resourceID, gvk.Kind, err},
			args,
		)...)

		// Ensure the resource list is not empty
		g.Expect(objList).NotTo(BeEmpty(), resourceEmptyErrorMsg, resourceID, gvk.Kind)

		// Ensure exactly one resource exists
		g.Expect(objList).To(HaveLen(1), appendDefaultIfNeeded(
			"Expected exactly one resource '%s' of kind '%s', but found %d.",
			[]any{resourceID, gvk.Kind, len(objList)},
			args,
		)...)
	}).Should(Succeed())

	return &objList[0]
}

// EnsureResourcesWithLabelsExist ensures that at least `minCount` resources of a given kind
// exist in the cluster, filtered by the specified labels.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resources being checked.
//   - matchingLabels (client.MatchingLabels): The label selector used to filter the resources.
//   - minCount (int): The minimum number of matching resources expected.
//   - args (...any): Optional additional arguments for assertion messages.
func (tc *TestContext) EnsureResourcesWithLabelsExist(
	gvk schema.GroupVersionKind,
	matchingLabels client.MatchingLabels,
	minCount int,
	args ...any,
) {
	tc.g.Eventually(func(g Gomega) {
		// Fetch resources based on labels
		var err error
		objList, err := tc.g.List(gvk, matchingLabels).Get()

		// Check if an error occurred while listing resources
		g.Expect(err).NotTo(
			HaveOccurred(),
			"Failed to list resources of kind '%s' with labels '%s': %v",
			gvk.Kind, k8slabels.FormatLabels(matchingLabels), err,
		)

		// Ensure at least `minCount` resources exist
		g.Expect(len(objList)).To(
			BeNumerically(">=", minCount),
			appendDefaultIfNeeded(
				"Expected at least %d resources of kind '%s' with labels '%s', but found %d.",
				[]any{minCount, gvk.Kind, k8slabels.FormatLabels(matchingLabels), len(objList)},
				args,
			)...,
		)
	}).Should(Succeed())
}

// EnsureResourceCreatedOrUpdated ensures that a given Kubernetes resource exists.
// If the resource is missing, it will be created; if it already exists, it will be updated
// using the provided mutation function.
//
// Parameters:
//   - option (ResourceOption): A function that provides the resource object (either existing or new).
//   - fn (func(*unstructured.Unstructured) error): A function to modify the resource before applying it.
//   - args (...interface{}): Optional Gomega assertion message arguments. If none are provided, a default message is used.
//
// Returns:
//   - *unstructured.Unstructured: The existing or newly created (updated) resource object.
func (tc *TestContext) EnsureResourceCreatedOrUpdated(
	option ResourceOption,
	fn func(obj *unstructured.Unstructured) error,
	args ...any,
) *unstructured.Unstructured {
	return tc.EnsureResourceCreatedOrUpdatedWithCondition(option, fn, Succeed(), args...)
}

// EnsureResourceCreatedOrUpdatedWithCondition ensures that a Kubernetes resource is either created or updated
// and that it meets a specified condition. It allows resources to be provided either by directly passing an object
// or by fetching one from the cluster.
//
// Parameters:
//   - option: A function that provides the resource (either an existing object or fetched from the cluster).
//   - fn (func(*unstructured.Unstructured) error): A function to modify the resource before applying it.
//   - condition (gTypes.GomegaMatcher): The Gomega matcher condition to assert on the resource after it is created or updated.
//   - args (...interface{}): Optional Gomega assertion message arguments. Defaults to a standard error message.
//
// Returns:
//   - *unstructured.Unstructured: The existing or newly created (updated) resource object.
func (tc *TestContext) EnsureResourceCreatedOrUpdatedWithCondition(
	option ResourceOption,
	fn func(obj *unstructured.Unstructured) error,
	condition gTypes.GomegaMatcher,
	args ...any,
) *unstructured.Unstructured {
	return tc.ensureResourceAppliedWithCondition(option, fn, condition, tc.g.CreateOrUpdate, args...)
}

// EnsureResourceCreatedOrPatched ensures that a given Kubernetes resource exists.
// If the resource is missing, it will be created; if it already exists, it will be patched
// using the provided mutation function.
//
// Parameters:
//   - option (ResourceOption): A function that provides the resource object (either existing or new).
//   - fn (func(*unstructured.Unstructured) error): A function to modify the resource before applying it.
//   - args (...interface{}): Optional Gomega assertion message arguments. Defaults to a standard error message.
//
// Returns:
//   - *unstructured.Unstructured: The existing or newly created (patched) resource object.
func (tc *TestContext) EnsureResourceCreatedOrPatched(
	option ResourceOption,
	fn func(obj *unstructured.Unstructured) error,
	args ...any,
) *unstructured.Unstructured {
	return tc.EnsureResourceCreatedOrPatchedWithCondition(option, fn, Succeed(), args...)
}

// EnsureResourceCreatedOrPatchedWithCondition ensures that a given Kubernetes resource exists
// and that it matches the specified condition. If the resource is missing, it will be created;
// if it already exists, it will be patched using the provided mutation function.
//
// Parameters:
//   - option (ResourceOption): A function that provides the resource (either an existing object or fetched from the cluster).
//   - fn (func(*unstructured.Unstructured) error): A function to modify the resource before applying it.
//   - condition (gTypes.GomegaMatcher): The Gomega matcher condition to assert on the resource after it is created or patched.
//   - args (...interface{}): Optional Gomega assertion message arguments. Defaults to a standard error message.
//
// Returns:
//   - *unstructured.Unstructured: The existing or newly created (patched) resource object.
func (tc *TestContext) EnsureResourceCreatedOrPatchedWithCondition(
	option ResourceOption,
	fn func(obj *unstructured.Unstructured) error,
	condition gTypes.GomegaMatcher,
	args ...any,
) *unstructured.Unstructured {
	return tc.ensureResourceAppliedWithCondition(option, fn, condition, tc.g.CreateOrPatch, args...)
}

// EnsureSubscriptionExistsOrCreate ensures that the specified Subscription exists.
// If the Subscription is missing, it will be created; if it already exists, no action is taken.
// This function reuses the `EnsureResourceCreatedOrUpdated` logic to guarantee that the Subscription
// exists or is created.
//
// Parameters:
//   - nn (types.NamespacedName): The namespace and name of the Subscription.
//
// Returns:
//   - *unstructured.Unstructured: The existing or newly created Subscription object.
func (tc *TestContext) EnsureSubscriptionExistsOrCreate(
	nn types.NamespacedName,
) *unstructured.Unstructured {
	// Construct a resource identifier.
	resourceID := resources.FormatNamespacedName(nn)

	// Create the subscription object using the necessary values (adapt as needed)
	sub := createSubscription(nn)

	// Ensure the Subscription exists or create it if missing
	return tc.EnsureResourceCreatedOrUpdatedWithCondition(
		WithObjectToCreate(sub),
		testf.TransformSpecToUnstructured(sub.Spec),
		jq.Match(`.status | has("installPlanRef")`),
		"Failed to ensure Subscription '%s' exists", resourceID,
	)
}

// EnsureResourcesAreEqual asserts that two resource objects are identical.
// Uses Gomega's `BeEquivalentTo` for a flexible deep comparison.
//
// Parameters:
//   - actualResource (interface{}): The resource to be compared.
//   - expectedResource (interface{}): The expected resource.
//   - args (...interface{}): Optional Gomega assertion message arguments.
func (tc *TestContext) EnsureResourcesAreEqual(
	actualResource, expectedResource interface{},
	args ...any,
) {
	// Use Gomega's BeEquivalentTo for flexible deep comparison
	tc.g.Expect(actualResource).To(
		BeEquivalentTo(expectedResource),
		appendDefaultIfNeeded(
			"Expected resource to be equal to the actual resource, but they differ.\nActual: %v\nExpected: %v", []any{actualResource, expectedResource},
			args,
		)...,
	)
}

// EnsureResourceNotNil verifies that the given resource is not nil and fails the test if it is.
//
// Parameters:
//   - obj (*unstructured.Unstructured): The resource object to check.
//   - resourceID (string): The identifier of the resource (e.g., "namespace/name").
//   - kind (string): The kind of the resource (e.g., "Deployment").
//   - args (...interface{}): Optional Gomega assertion message arguments.
func (tc *TestContext) EnsureResourceNotNil(
	obj any,
	args ...any,
) {
	tc.EnsureResourceConditionMet(obj, Not(BeNil()), args...)
}

// EnsureResourceConditionMet verifies that a given resource satisfies a specified condition.
// Callers should explicitly use `Not(matcher)` if they need to assert a negative condition.
//
// Parameters:
//   - obj (any): The resource object to check.
//   - matcher: A Gomega matcher specifying the expected condition (e.g., BeEmpty(), Not(BeEmpty())).
//   - args (...interface{}): Optional Gomega assertion message arguments. If not provided, a default message is used.
func (tc *TestContext) EnsureResourceConditionMet(
	obj any,
	condition gTypes.GomegaMatcher,
	args ...any,
) {
	// Ensure obj is not nil before proceeding
	tc.g.Expect(obj).NotTo(BeNil(), resourceNotNilErrorMsg)

	// Convert the input object to unstructured
	u, err := resources.ToUnstructured(obj)
	tc.g.Expect(err).NotTo(HaveOccurred())

	// Construct a meaningful resource identifier
	resourceID := resources.FormatUnstructuredName(u)

	// Perform the assertion using the custom condition
	tc.g.Expect(obj).To(
		condition,
		appendDefaultIfNeeded(
			"Expected resource '%s' of kind '%s' to satisfy condition '%v' but did not.",
			[]any{resourceID, u.GetKind()},
			args,
		)...,
	)
}

// EnsureDeploymentReady ensures that the specified Deployment is ready by checking its status and conditions.
//
// This function performs the following steps:
// 1. Ensures that the deployment resource exists using `EnsureResourceExists`.
// 2. Converts the `Unstructured` resource into a `Deployment` object using Kubernetes' runtime conversion.
// 3. Asserts that the `Deployment` condition `DeploymentAvailable` is `True`.
// 4. Verifies that the number of ready replicas in the deployment matches the expected count.
//
// Parameters:
//   - nn (types.NamespacedName): The namespace and name of the deployment to check.
//   - replicas (int32): The expected number of ready replicas for the deployment.
func (tc *TestContext) EnsureDeploymentReady(
	nn types.NamespacedName,
	replicas int32,
) {
	// Construct a resource identifier.
	resourceID := resources.FormatNamespacedName(nn)

	// Ensure the deployment exists and retrieve the object.
	deployment := &appsv1.Deployment{}
	tc.RetrieveResource(
		gvk.Deployment,
		nn,
		deployment,
		"Deployment %s was expected to exist but was not found", resourceID,
	)

	// Assert that the deployment contains the necessary condition (DeploymentAvailable) with status "True"
	tc.g.Expect(deployment.Status.Conditions).To(
		ContainElement(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Type":   Equal(appsv1.DeploymentAvailable),
				"Status": Equal(corev1.ConditionTrue),
			}),
		), "Expected DeploymentAvailable condition to be True for deployment %s", resourceID)

	// Assert the number of ready replicas matches the expected count
	tc.g.Expect(deployment.Status.ReadyReplicas).To(
		Equal(replicas),
		"Expected %d ready replicas for deployment, but got %d", replicas, resourceID, deployment.Status.ReadyReplicas)
}

// EnsureCRDEstablished ensures that the specified CustomResourceDefinition is fully established.
//
// This function performs the following steps:
// 1. Ensures that the CRD resource exists using `EnsureResourceExists`.
// 2. Converts the `Unstructured` resource into a `CustomResourceDefinition` object using Kubernetes' runtime conversion.
// 3. Asserts that the CRD condition `Established` is `True`.
//
// Parameters:
//   - name (string): The name of the CRD to check.
func (tc *TestContext) EnsureCRDEstablished(
	name string,
) {
	// Ensure the CustomResourceDefinition exists and retrieve the object
	crd := &apiextv1.CustomResourceDefinition{}
	tc.RetrieveResource(
		gvk.CustomResourceDefinition,
		types.NamespacedName{Name: name},
		crd,
		"CRD %s was expected to exist but was not found", name,
	)

	// Assert that the CustomResourceDefinition contains the necessary condition (Established) with status "True"
	tc.g.Expect(crd.Status.Conditions).To(
		ContainElement(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Type":   Equal(apiextv1.Established),
				"Status": Equal(apiextv1.ConditionTrue),
			}),
		), "Expected CRD condition 'Established' to be True for CRD %s", name)
}

// EnsureResourceIsUnique ensures that creating a second instance of a given resource fails.
//
// This function performs the following steps:
// 1. Converts the provided resource object into an `Unstructured` format using `ObjectToUnstructured`.
// 2. Extracts the `GroupVersionKind` (GVK) from the object.
// 3. Ensures that at least one resource of the same kind already exists in the cluster using `EnsureResourceExists`.
// 4. Attempts to create a duplicate resource using `CreateUnstructured`.
// 5. Asserts that the creation attempt fails, ensuring uniqueness constraints are enforced.
//
// Parameters:
//   - tc (*TestContext): The test context that provides access to Gomega and the Kubernetes client.
//   - obj (any): The resource object to create, which must be convertible to an unstructured format.
//
// Returns:
//   - error: Returns nil if the duplicate creation fails as expected, otherwise returns an error.
func (tc *TestContext) EnsureResourceIsUnique(
	obj client.Object,
	args ...any,
) {
	// Ensure obj is not nil before proceeding
	tc.g.Expect(obj).NotTo(BeNil(), resourceNotNilErrorMsg)

	// Convert the input object to unstructured
	u, err := resources.ObjectToUnstructured(tc.Scheme(), obj)
	tc.g.Expect(err).NotTo(HaveOccurred(), err)

	// Extract GroupVersionKind from the unstructured object
	groupVersionKind := u.GetObjectKind().GroupVersionKind()

	// Ensure that at least one resource of this kind already exists
	tc.EnsureResourcesExist(
		groupVersionKind,
		types.NamespacedName{Namespace: u.GetNamespace()},
		&client.ListOptions{Namespace: u.GetNamespace()},
		"Failed to verify existence of %s", groupVersionKind.Kind,
	)

	// Attempt to create the duplicate resource, expecting failure
	tc.g.Eventually(func(g Gomega) {
		// Try to create the resource
		_, err := tc.g.Create(u, types.NamespacedName{Namespace: u.GetNamespace(), Name: u.GetName()}).Get()

		// If there's no error, that means the duplicate creation succeeded, which is a failure
		g.Expect(err).To(HaveOccurred(), appendDefaultIfNeeded(
			"Expected creation of duplicate %s to fail due to uniqueness constraint, but it succeeded.",
			[]any{groupVersionKind.Kind},
			args,
		)...)

		// Check if the error is a Kubernetes StatusError and was denied by an admission webhook
		// Ensure the failure is due to uniqueness constraints (Forbidden error)
		g.Expect(errors.IsForbidden(err)).To(BeTrue(),
			appendDefaultIfNeeded(
				"Expected failure due to uniqueness constraint (Forbidden), but got: %v",
				[]any{err},
				args,
			)...,
		)
	}).Should(Succeed())
}

// EnsureOperatorInstalled ensures that the specified operator is installed and the associated
// ClusterServiceVersion (CSV) reaches the 'Succeeded' phase.
//
// This function performs the following tasks:
// 1. Creates or updates the namespace for the operator, if necessary.
// 2. Optionally creates or updates the operator group, depending on the 'skipOperatorGroupCreation' flag.
// 3. Retrieves the InstallPlan for the operator and approves it if not already approved.
// 4. Verifies that the operator's ClusterServiceVersion (CSV) reaches the 'Succeeded' phase.
//
// Parameters:
//   - nn (types.NamespacedName): The namespace and name of the operator being installed.
//   - skipOperatorGroupCreation (bool): If true, skips the creation or update of the operator group.
func (tc *TestContext) EnsureOperatorInstalled(nn types.NamespacedName, skipOperatorGroupCreation bool) {
	// Construct a resource identifier.
	resourceID := resources.FormatNamespacedName(nn)

	// Ensure the operator's namespace is created.
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: nn.Namespace}),
		NoOpMutationFn,
		"Failed to create or update namespace '%s'", nn.Namespace,
	)

	// Ensure the operator group is created or updated only if necessary.
	if !skipOperatorGroupCreation {
		tc.EnsureResourceCreatedOrUpdated(
			WithMinimalObject(gvk.OperatorGroup, nn),
			NoOpMutationFn,
			"Failed to create or update operator group '%s'", resourceID,
		)
	}

	// Retrieve the InstallPlan
	plan := tc.RetrieveInstallPlan(nn)

	// in CI InstallPlan is in Manual mode
	if !plan.Spec.Approved {
		tc.ApproveInstallPlan(plan)
	}

	// Retrieve the CSV name from the InstallPlan and ensure it reaches 'Succeeded' phase.
	tc.g.Expect(plan.Spec.ClusterServiceVersionNames).NotTo(BeEmpty(), "No CSV found in InstallPlan for operator '%s'", resourceID)
	csvName := plan.Spec.ClusterServiceVersionNames[0] // Assuming first in the list

	tc.g.Eventually(func(g Gomega) {
		csv := tc.RetrieveClusterServiceVersion(types.NamespacedName{Namespace: nn.Namespace, Name: csvName})
		g.Expect(csv.Status.Phase).To(
			Equal(ofapi.CSVPhaseSucceeded),
			"CSV %s did not reach 'Succeeded' phase", resourceID,
		)
	}).WithTimeout(eventuallyTimeoutMedium).WithPolling(generalRetryInterval)
}

// DeleteResource verifies whether a specific Kubernetes resource exists and deletes it if found.
// If the resource exists, it is deleted using the provided client options. The test will fail if the resource
// does not exist or if the deletion fails.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource to be deleted.
//   - nn (types.NamespacedName): The namespace and name of the resource to be deleted.
//   - clientOption (...client.DeleteOption): Optional delete options such as cascading or propagation policy.
func (tc *TestContext) DeleteResource(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	clientOption ...client.DeleteOption,
) {
	// Construct a resource identifier.
	resourceID := resources.FormatNamespacedName(nn)

	// Ensure the resource exists before attempting deletion
	tc.EnsureResourceExists(
		gvk,
		nn,
		"Expected %s instance %s to exist before attempting deletion", gvk.Kind, resourceID,
	)

	// Delete the resource if it exists
	tc.g.Delete(
		gvk,
		nn,
		clientOption...,
	).Eventually().Should(Succeed(), "Failed to delete %s instance %s", gvk.Kind, resourceID)
}

// DeleteResourceIfExists verifies whether a specific Kubernetes resource exists and deletes it if found.
// If the resource exists, it is deleted using the provided client options. The test will succeed even if the resource
// does not exist (i.e., the deletion is considered successful if the resource is not found).
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource to be deleted.
//   - nn (types.NamespacedName): The namespace and name of the resource to be deleted.
//   - clientOption (...client.DeleteOption): Optional delete options such as cascading or propagation policy.
func (tc *TestContext) DeleteResourceIfExists(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	clientOption ...client.DeleteOption,
) {
	// Construct a resource identifier.
	resourceID := resources.FormatNamespacedName(nn)

	// Use EnsureResourceExistsOrNil to attempt to fetch the resource with retries
	_, err := tc.EnsureResourceExistsOrNil(gvk, nn)

	// If error is not nil and not IsNotFound, we fail the test.
	if err != nil && !errors.IsNotFound(err) {
		tc.g.Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Unexpected error while checking existence of %s instance %s", gvk.Kind, resourceID))
		return
	}

	// Delete the resource
	tc.g.Delete(
		gvk,
		nn,
		clientOption...,
	).Eventually().Should(Succeed(), "Failed to delete %s instance %s", gvk.Kind, resourceID)
}

// RetrieveInstallPlanName retrieves the name of the InstallPlan associated with a subscription.
// It ensures that the subscription exists (or is created) and then retrieves the InstallPlan name.
// This function does not return an error, it will panic if anything goes wrong (such as a missing InstallPlanRef).
//
// Parameters:
//   - name (string): The name of the Subscription to check.
//   - ns (string): The namespace of the Subscription.
//
// Returns:
//   - string: The name of the InstallPlan associated with the Subscription.
func (tc *TestContext) RetrieveInstallPlanName(nn types.NamespacedName) string {
	// Ensure the subscription exists or is created
	u := tc.EnsureSubscriptionExistsOrCreate(nn)

	// Convert the Unstructured object to Subscription and assert no error
	sub := &ofapi.Subscription{}
	tc.ConvertUnstructuredToResource(u, sub)

	// Return the name of the InstallPlan
	return sub.Status.InstallPlanRef.Name
}

// RetrieveInstallPlan retrieves the InstallPlan associated with a Subscription by its name and namespace.
// It ensures the Subscription exists (or is created) and fetches the InstallPlan object by its name and namespace.
//
// Parameters:
//   - name (string): The name of the Subscription to check.
//   - ns (string): The namespace of the Subscription.
//
// Returns:
//   - *ofapi.InstallPlan: The InstallPlan associated with the Subscription.
func (tc *TestContext) RetrieveInstallPlan(nn types.NamespacedName) *ofapi.InstallPlan {
	// Retrieve the InstallPlan name using getInstallPlanName (ensuring Subscription exists if necessary)
	planName := tc.RetrieveInstallPlanName(nn)

	// Ensure the InstallPlan exists and retrieve the object.
	installPlan := &ofapi.InstallPlan{}
	tc.RetrieveResource(
		gvk.InstallPlan,
		types.NamespacedName{Namespace: nn.Namespace, Name: planName},
		installPlan,
		"InstallPlan %s was expected to exist but was not found", planName,
	)

	// Return the InstallPlan object
	return installPlan
}

// RetrieveClusterServiceVersion retrieves a ClusterServiceVersion (CSV) for an operator by name and namespace.
// If the CSV does not exist, the function will fail the test using Gomega assertions.
//
// Parameters:
//   - name (string): The name of the ClusterServiceVersion to retrieve.
//   - ns (string): The namespace where the ClusterServiceVersion is expected to be found.
//
// Returns:
//   - *ofapi.ClusterServiceVersion: A pointer to the retrieved ClusterServiceVersion object.
func (tc *TestContext) RetrieveClusterServiceVersion(nn types.NamespacedName) *ofapi.ClusterServiceVersion {
	// Construct a resource identifier.
	resourceID := resources.FormatNamespacedName(nn)

	// Retrieve the CSV
	csv := &ofapi.ClusterServiceVersion{}
	tc.RetrieveResource(gvk.ClusterServiceVersion, nn, csv)

	// Assert that we found the CSV
	tc.g.Expect(csv).NotTo(BeNil(), "CSV %s not found", resourceID)

	return csv
}

// RetrieveClusterVersion retrieves the ClusterVersion for the cluster.
// If the ClusterVersion does not exist, the function will fail the test using Gomega assertions.
//
// Returns:
//   - *configv1.ClusterVersion: A pointer to the retrieved ClusterVersion object.
func (tc *TestContext) RetrieveClusterVersion() *configv1.ClusterVersion {
	// Retrieve the ClusterVersion
	cv := &configv1.ClusterVersion{}
	tc.RetrieveResource(gvk.ClusterVersion, types.NamespacedName{Name: cluster.OpenShiftVersionObj}, cv)

	// Assert that we found the ClusterVersion
	tc.g.Expect(cv).NotTo(BeNil(), "ClusterVersion not found")

	return cv
}

// RetrieveDSCInitialization retrieves the DSCInitialization resource.
//
// This function ensures that the DSCInitialization resource exists and then retrieves it
// as a strongly typed object.
//
// Parameters:
//   - nn (types.NamespacedName): The namespaced name (namespace + name) of the DSCInitialization.
//
// Returns:
//   - *dsciv1.DSCInitialization: The retrieved DSCInitialization object.
func (tc *TestContext) RetrieveDSCInitialization(nn types.NamespacedName) *dsciv1.DSCInitialization {
	// Ensure the DSCInitialization exists and retrieve the object
	dsci := &dsciv1.DSCInitialization{}
	tc.RetrieveResource(gvk.DSCInitialization, nn, dsci)

	return dsci
}

// RetrieveDataScienceCluster retrieves the DataScienceCluster resource.
//
// This function ensures that the DataScienceCluster resource exists and then retrieves it
// as a strongly typed object.
//
// Parameters:
//   - nn (types.NamespacedName): The namespaced name (namespace + name) of the DataScienceCluster.
//
// Returns:
//   - *dsciv1.DataScienceCluster: The retrieved DataScienceCluster object.
func (tc *TestContext) RetrieveDataScienceCluster(nn types.NamespacedName) *dscv1.DataScienceCluster {
	// Ensure the DataScienceCluster exists and retrieve the object
	dsc := &dscv1.DataScienceCluster{}
	tc.RetrieveResource(gvk.DataScienceCluster, nn, dsc)

	return dsc
}

// RetrieveResource ensures a Kubernetes resource exists and retrieves it into a typed object.
//
// This function first verifies that the resource exists using `EnsureResourceExists` and then
// converts the retrieved Unstructured object into the provided typed object.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource to retrieve.
//   - nn (types.NamespacedName): The namespaced name (namespace + name) of the resource.
//   - obj (any): A pointer to the typed object where the resource data will be stored.
//   - args (any, optional): Additional arguments for error messages or logging.
//
// Panics:
//   - If the resource does not exist.
//   - If conversion from Unstructured to the typed object fails.
func (tc *TestContext) RetrieveResource(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	obj client.Object,
	args ...any,
) {
	// Ensure the resource exists and retrieve the object
	u := tc.EnsureResourceExists(gvk, nn, args...)

	// Convert the Unstructured object to a typed object
	tc.ConvertUnstructuredToResource(u, obj)
}

// RetrieveResources fetches a list of resources from the cluster and waits for their successful retrieval.
//
// This function uses the `Eventually` mechanism to repeatedly attempt to retrieve the resources from
// the cluster until they are available. It ensures no errors occur during the retrieval process and that
// the list of resources is successfully fetched.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind (GVK) of the resources being retrieved.
//   - nn (types.NamespacedName): The namespace and name of the resource(s) to be fetched.
//   - listOptions (*client.ListOptions, optional): Additional options for listing the resources (e.g., label selectors).
//   - args (...any, optional): Optional arguments for custom error messages or logging.
//
// Returns:
//   - []unstructured.Unstructured: A list of resources fetched from the cluster.
func (tc *TestContext) RetrieveResources(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	listOptions *client.ListOptions,
	args ...any,
) []unstructured.Unstructured {
	// Construct a resource identifier
	resourceID := resources.FormatNamespacedName(nn)

	// If no ListOptions are provided, initialize an empty one
	if listOptions == nil {
		listOptions = &client.ListOptions{}
	}

	// Use Eventually to retry getting the resource list until they appear
	var resourcesList []unstructured.Unstructured
	tc.g.Eventually(func(g Gomega) {
		// Fetch the list of resources
		var err error
		resourcesList, _ = tc.g.List(gvk, listOptions).Get()

		// Ensure no unexpected errors occurred while fetching the resources
		g.Expect(err).ToNot(HaveOccurred(),
			appendDefaultIfNeeded(resourceFetchErrorMsg, []any{resourceID, gvk.Kind, err}, args)...,
		)
	}).Should(Succeed())

	return resourcesList
}

// ApproveInstallPlan approves the provided InstallPlan by applying a patch to update its approval status.
//
// This function performs the following steps:
// 1. Prepares the InstallPlan object with the necessary changes to approve it.
// 2. Sets up patch options, including force applying the patch with the specified field manager.
// 3. Applies the patch to update the InstallPlan, marking it as approved automatically.
// 4. Asserts that no error occurs during the patch application process.
//
// Parameters:
//   - plan (*ofapi.InstallPlan): The InstallPlan object that needs to be approved.
func (tc *TestContext) ApproveInstallPlan(plan *ofapi.InstallPlan) {
	// Prepare the InstallPlan object to be approved
	obj := createInstallPlan(plan.ObjectMeta.Name, plan.ObjectMeta.Namespace, plan.Spec.ClusterServiceVersionNames)

	// Set up patch options
	force := true
	opt := &client.PatchOptions{
		FieldManager: dscInstanceName,
		Force:        &force,
	}

	// Apply the patch to approve the InstallPlan
	err := tc.Client().Patch(tc.Context(), obj, client.Apply, opt)
	tc.g.Expect(err).
		NotTo(
			HaveOccurred(),
			"Failed to approve InstallPlan %s in namespace %s: %v", obj.ObjectMeta.Name, obj.ObjectMeta.Namespace, err,
		)
}

// ConvertUnstructuredToResource converts the provided Unstructured object to the specified resource type.
// This function performs the conversion and asserts that no error occurs during the conversion process.
//
// The function utilizes Gomega's Expect method to assert that the conversion is successful.
// If the conversion fails, the test will fail.
//
// Parameters:
//   - u (*unstructured.Unstructured): The Unstructured object to be converted.
//   - obj (T): A pointer to the target resource object to which the Unstructured object will be converted. The object must be a pointer to a struct.
func (tc *TestContext) ConvertUnstructuredToResource(u *unstructured.Unstructured, obj client.Object) {
	// Convert Unstructured object to the given resource object
	err := resources.ObjectFromUnstructured(tc.Scheme(), u, obj)
	tc.g.Expect(err).NotTo(HaveOccurred(), "Failed converting %T from Unstructured.Object: %v", obj, u.Object)
}

// ExtractAndExpectValue extracts a value from an input using a jq expression and asserts conditions on it.
//
// This function extracts a value of type T from the given input using the specified jq expression.
// It ensures that extraction succeeds and applies one or more assertions to validate the extracted value.
//
// Parameters:
//   - g (Gomega): The Gomega testing instance used for assertions.
//   - in (any): The input data (e.g., a Kubernetes resource).
//   - expression (string): The jq expression used to extract a value from the input.
//   - matchers (GomegaMatcher): One or more Gomega matchers to validate the extracted value.
func ExtractAndExpectValue[T any](g Gomega, in any, expression string, matchers ...gTypes.GomegaMatcher) T {
	// Extract the value using the jq expression
	value, err := jq.ExtractValue[T](in, expression)

	// Expect no errors during extraction
	g.Expect(err).NotTo(HaveOccurred(), "Failed to extract value using expression: %s", expression)

	// Apply matchers to validate the extracted value
	g.Expect(value).To(And(matchers...), "Extracted value from %s does not match expected conditions", expression)

	return value
}

// CreateDSCI creates a DSCInitialization CR.
func CreateDSCI(name, appNamespace string) *dsciv1.DSCInitialization {
	return &dsciv1.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: dsciv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: appNamespace,
			Monitoring: serviceApi.DSCIMonitoring{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Removed, // keep rhoai branch to Managed so we can test it
				},
				MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
					Namespace: appNamespace,
				},
			},
			TrustedCABundle: &dsciv1.TrustedCABundleSpec{
				ManagementState: operatorv1.Managed,
				CustomCABundle:  "",
			},
			ServiceMesh: &infrav1.ServiceMeshSpec{
				ManagementState: operatorv1.Managed,
				ControlPlane: infrav1.ControlPlaneSpec{
					Name:              serviceMeshControlPlane,
					Namespace:         serviceMeshNamespace,
					MetricsCollection: serviceMeshMetricsCollection,
				},
			},
		},
	}
}

// CreateDSC creates a DataScienceCluster CR.
func CreateDSC(name string) *dscv1.DataScienceCluster {
	return &dscv1.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: dscv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: dscv1.DataScienceClusterSpec{
			Components: dscv1.Components{
				// keep dashboard as enabled, because other test is rely on this
				Dashboard: componentApi.DSCDashboard{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Workbenches: componentApi.DSCWorkbenches{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				ModelMeshServing: componentApi.DSCModelMeshServing{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				DataSciencePipelines: componentApi.DSCDataSciencePipelines{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
						DefaultDeploymentMode: componentApi.Serverless,
						Serving: infrav1.ServingSpec{
							ManagementState: operatorv1.Managed,
							Name:            knativeServingNamespace,
							IngressGateway: infrav1.GatewaySpec{
								Certificate: infrav1.CertificateSpec{
									Type: infrav1.OpenshiftDefaultIngress,
								},
							},
						},
					},
				},
				CodeFlare: componentApi.DSCCodeFlare{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Ray: componentApi.DSCRay{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Kueue: componentApi.DSCKueue{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				TrustyAI: componentApi.DSCTrustyAI{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				ModelRegistry: componentApi.DSCModelRegistry{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					ModelRegistryCommonSpec: componentApi.ModelRegistryCommonSpec{
						RegistriesNamespace: modelregistryctrl.DefaultModelRegistriesNamespace,
					},
				},
				TrainingOperator: componentApi.DSCTrainingOperator{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				FeastOperator: componentApi.DSCFeastOperator{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}
}

// ensureResourceAppliedWithCondition is a common function for handling create/patch and create/update operations.
// It applies a given resource and ensures it meets the expected condition.
//
// Parameters:
//   - gvk: The GroupVersionKind of the resource.
//   - nn: The NamespacedName identifying the resource.
//   - fn: A mutation function to modify the resource before applying.
//   - condition: A Gomega matcher that the resource must satisfy.
//   - applyResourceFn: The function responsible for applying the resource.
//   - args: Additional arguments for error message formatting.
//
// Returns:
//   - *unstructured.Unstructured: The applied resource if it meets the expected condition.
func (tc *TestContext) ensureResourceAppliedWithCondition(
	option ResourceOption,
	fn func(obj *unstructured.Unstructured) error,
	condition gTypes.GomegaMatcher,
	applyResourceFn func(obj *unstructured.Unstructured, fn ...func(obj *unstructured.Unstructured) error) *testf.EventuallyValue[*unstructured.Unstructured],
	args ...any,
) *unstructured.Unstructured {
	// Retrieve the object using the provided option
	obj := option(tc)

	// Get GroupVersionKind from the resource.
	groupVersionKind := obj.GetObjectKind().GroupVersionKind()

	// Construct a resource identifier.
	resourceID := resources.FormatUnstructuredName(obj)

	// Wrap condition if it's not already wrapped correctly
	wrapConditionIfNeeded(&condition)

	// Use Eventually to retry getting the resource until it appears
	var u *unstructured.Unstructured
	tc.g.Eventually(func(g Gomega) {
		// Fetch the resource
		var err error
		u, err = applyResourceFn(obj, fn).Get()

		// Evaluate the condition to check if failure is expected
		expectingFailure := tc.isFailureExpected(condition)

		// Error handling based on failure expectation
		if !expectingFailure {
			// Expect no error if success is expected
			g.Expect(err).NotTo(
				HaveOccurred(),
				"Error occurred while applying the resource '%s' of kind '%s': %v", resourceID, groupVersionKind.Kind, err,
			)

			// Ensure that the resource object is not nil
			g.Expect(u).NotTo(BeNil(), resourceNotFoundErrorMsg, resourceID, groupVersionKind.Kind)
		} else {
			// Expect error if failure is expected
			g.Expect(err).To(HaveOccurred(), "Expected applyResourceFn to fail but it succeeded")
		}

		// Apply the matchers based on the condition
		applyMatchers(g, resourceID, groupVersionKind, u, err, condition, args)
	}).Should(Succeed())

	return u
}

// ensureResourceDoesNotExist is a helper function that attempts to retrieve a Kubernetes resource
// and checks if it does not exist. It returns an error if the resource is found in the cluster.
//
// Parameters:
//   - g (Gomega): The Gomega assertion wrapper.
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource being checked.
//   - nn (types.NamespacedName): The namespaced name of the resource (e.g., "namespace/resource-name").
//   - args ([]any): Optional arguments that can be used for retries or custom messages during resource retrieval.
//
// Returns:
//   - error: An error if the resource is found in the cluster, or nil if the resource does not exist.
func (tc *TestContext) ensureResourceDoesNotExist(
	g Gomega,
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	args []any,
) error {
	// Construct a resource identifier.
	resourceID := resources.FormatNamespacedName(nn)

	// Use EnsureResourceExistsOrNil to attempt to fetch the resource with retries
	u, err := tc.EnsureResourceExistsOrNil(gvk, nn, args...)

	// Ensure that the resource object is not nil
	g.Expect(u).To(BeNil(), resourceFoundErrorMsg, resourceID, gvk.Kind)

	return err
}

// ensureResourcesDoNotExist is a helper function that retrieves a list of Kubernetes resources
// and checks if the resources do not exist (i.e., the list is empty). It performs an assertion
// to ensure that the list of resources is empty. If any resources are found, it fails the test.
//
// Parameters:
//   - g (Gomega): The Gomega assertion wrapper.
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resources to check.
//   - nn (types.NamespacedName): The namespace and name of the resource(s).
//   - listOptions (*client.ListOptions, optional): Optional list options for filtering the resources (e.g., label selectors).
//   - args (...interface{}): Optional Gomega assertion message arguments.
//
// Returns:
//   - error: An error if the resource is found in the cluster, or nil if the resource does not exist.
func (tc *TestContext) ensureResourcesDoNotExist(
	g Gomega,
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	listOptions *client.ListOptions,
	args []any,
) {
	// Construct a resource identifier for logging
	resourceID := resources.FormatNamespacedName(nn)

	// Retrieve the resources list using the common function
	resourcesList := tc.RetrieveResources(gvk, nn, listOptions, args...)

	// Ensure that the resources list is empty (resources should not exist)
	g.Expect(resourcesList).To(BeEmpty(), resourceNotEmptyErrorMsg, resourceID, gvk.Kind)
}

// applyMatchers applies Gomega matchers to a single resource or a list of resources.
//
// Parameters:
//   - g (Gomega): The Gomega assertion wrapper.
//   - resourceID (string): A string identifier for logging purposes.
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource(s).
//   - resource any: Can be a single unstructured.Unstructured or a list ([]unstructured.Unstructured).
//   - err error: The error returned when fetching the resource(s).
//   - condition gTypes.GomegaMatcher: The Gomega matcher to apply.
//   - args []any: Optional additional arguments for assertion messages.
func applyMatchers(
	g Gomega,
	resourceID string,
	gvk schema.GroupVersionKind,
	resource any,
	err error,
	condition gTypes.GomegaMatcher,
	args []any,
) {
	// Check if the condition is an And, Or, or Not and recursively inspect the inner matchers
	switch v := condition.(type) {
	case *matchers.AndMatcher:
		// If the condition is an And, apply each matcher recursively
		for _, inner := range v.Matchers {
			applyMatchers(g, resourceID, gvk, resource, err, inner, args)
		}
	case *matchers.OrMatcher:
		// If the condition is an Or, apply each matcher recursively
		for _, inner := range v.Matchers {
			applyMatchers(g, resourceID, gvk, resource, err, inner, args)
		}
	case *matchers.NotMatcher:
		// If the condition is a Not, apply the negated matcher recursively
		applyMatchers(g, resourceID, gvk, resource, err, v.Matcher, args)
	case gTypes.GomegaMatcher: // Base matcher (for non-Error matchers)
		// If matcher is an error matcher, apply it to the error
		if isErrorMatcher(v) {
			g.Expect(err).To(v, appendDefaultIfNeeded(
				"Expected error when applying resource '%s' of kind '%s'.",
				[]any{resourceID, gvk.Kind}, args,
			)...)
			return
		}

		// If working on a list of resources
		if resourcesList, ok := resource.([]unstructured.Unstructured); ok {
			// Apply the matcher to the list
			g.Expect(resourcesList).To(v, appendDefaultIfNeeded(
				"Expected list of '%s' resources to match condition %v.",
				[]any{gvk.Kind, dereferenceCondition(condition)}, args,
			)...)
			return
		}

		// If working on a single resource
		if u, ok := resource.(*unstructured.Unstructured); ok {
			g.Expect(u.Object).To(v, appendDefaultIfNeeded(
				"Expected resource '%s' of kind '%s' to match condition %v.",
				[]any{resourceID, gvk.Kind, dereferenceCondition(condition)}, args,
			)...)
		}
	}
}

// Helper function to evaluate if failure is expected based on the matcher.
func (tc *TestContext) isFailureExpected(condition gTypes.GomegaMatcher) bool {
	// Check if the condition is an And, Or, or Not and recursively inspect the inner matchers
	switch v := condition.(type) {
	case *matchers.AndMatcher:
		// If the condition is And(), we recursively check the inner matchers
		for _, inner := range v.Matchers {
			if tc.isFailureExpected(inner) {
				return true
			}
		}
	case *matchers.OrMatcher:
		// If the condition is Or(), we recursively check the inner matchers
		for _, inner := range v.Matchers {
			if tc.isFailureExpected(inner) {
				return true
			}
		}
	case *matchers.NotMatcher:
		// If the condition is Not(Succeed()), we expect failure
		if _, ok := v.Matcher.(*matchers.SucceedMatcher); ok {
			return true
		}
	case gTypes.GomegaMatcher:
		// For basic matchers (not wrapped in And/Or/Not), check if it is Not(Succeed()) or any other condition
		expectingFailure, _ := condition.Match(Not(Succeed()))
		return expectingFailure
	}

	// Default case if none of the conditions matched, return false (no failure expected)
	return false
}

// Check if the matcher is an error matcher like Succeed(), HaveOccurred(), etc.
func isErrorMatcher(matcher gTypes.GomegaMatcher) bool {
	switch matcher.(type) {
	case *matchers.SucceedMatcher, *matchers.HaveOccurredMatcher, *matchers.MatchErrorMatcher:
		return true
	default:
		return false
	}
}

// wrapConditionIfNeeded ensures that the condition is wrapped in And(Succeed(), condition)
// if it's not already wrapped in such a way.
func wrapConditionIfNeeded(condition *gTypes.GomegaMatcher) {
	// Check if the condition is already an 'And' that contains Succeed()
	if _, ok := (*condition).(*matchers.SucceedMatcher); ok || isAndContainingSucceed(*condition) {
		return
	}

	// Wrap condition inside And(Succeed(), condition) if it's not already wrapped correctly
	*condition = And(Succeed(), *condition)
}

// isAndContainingSucceed checks if the matcher is an AndMatcher containing Succeed().
func isAndContainingSucceed(matcher gTypes.GomegaMatcher) bool {
	// Check if the condition is already an 'And' that contains Succeed()
	andMatcher, ok := matcher.(*matchers.AndMatcher)
	if !ok {
		return false
	}

	// Check if Succeed is one of the matchers in the And condition
	for _, m := range andMatcher.Matchers {
		if _, ok := m.(*matchers.SucceedMatcher); ok {
			return true
		}
	}

	return false
}

// appendDefaultIfNeeded appends a default message to args if args is empty.
// It formats the default message using the provided format and formatArgs.
func appendDefaultIfNeeded(format string, formatArgs []any, args []any) []any {
	// If no custom args are provided, append the default message
	if len(args) == 0 {
		args = append(args, fmt.Sprintf(format, formatArgs...))
	}
	return args
}

// createSubscription creates a Subscription object.
func createSubscription(nn types.NamespacedName) *ofapi.Subscription {
	return &ofapi.Subscription{
		TypeMeta: metav1.TypeMeta{
			Kind:       ofapi.SubscriptionKind,
			APIVersion: ofapi.SubscriptionCRDAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},
		Spec: &ofapi.SubscriptionSpec{
			CatalogSource:          "redhat-operators",
			CatalogSourceNamespace: "openshift-marketplace",
			Channel:                "stable",
			Package:                nn.Name,
			InstallPlanApproval:    ofapi.ApprovalAutomatic,
		},
	}
}

// createInstallPlan creates an InstallPlan object.
func createInstallPlan(name string, ns string, csvNames []string) *ofapi.InstallPlan {
	return &ofapi.InstallPlan{
		TypeMeta: metav1.TypeMeta{
			Kind:       ofapi.InstallPlanKind,
			APIVersion: ofapi.InstallPlanAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: ofapi.InstallPlanSpec{
			Approved:                   true,
			Approval:                   ofapi.ApprovalAutomatic,
			ClusterServiceVersionNames: csvNames,
		},
	}
}

// Helper function to safely dereference the condition pointer.
func dereferenceCondition(condition gTypes.GomegaMatcher) any {
	// If condition is a pointer, dereference it
	if reflect.TypeOf(condition).Kind() == reflect.Ptr {
		return reflect.ValueOf(condition).Elem().Interface()
	}
	// If it's not a pointer, return the condition as is
	return condition
}
