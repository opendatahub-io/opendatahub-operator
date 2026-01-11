package e2e_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/blang/semver/v4"
	"github.com/onsi/gomega/gstruct"
	gTypes "github.com/onsi/gomega/types"
	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type Logger interface {
	Logf(format string, args ...any)
	Helper()
}

// TestContext holds shared context and utilities used during E2E test execution.
type TestContext struct {
	// Embeds the common test context (e.g., cluster clients, config)
	*testf.TestContext

	// Shared Gomega wrapper for making assertions in tests.
	g *testf.WithT

	// Logger for test output.
	logger Logger

	// Test timeouts
	TestTimeouts TestTimeouts

	// Namespace where the operator components are deployed.
	OperatorNamespace string

	// Namespace where application workloads are deployed.
	AppsNamespace string

	// Namespace where the workbenches are deployed.
	WorkbenchesNamespace string

	// Namespace where the monitoring components are deployed.
	MonitoringNamespace string

	// Namespaced name of the DSCInitialization custom resource used for testing.
	DSCInitializationNamespacedName types.NamespacedName

	// Namespaced name of the DataScienceCluster custom resource used for testing.
	DataScienceClusterNamespacedName types.NamespacedName
}

// NewTestContext creates and initializes a new TestContext instance.
//
// It wraps the underlying test framework context (`testf.TestContext`) and sets up
// common testing parameters like default timeouts and polling intervals for Gomega assertions.
// This function is typically used at the beginning of a test to prepare a consistent test environment.
//
// Parameters:
//   - t (*testing.T): The standard Go testing instance for the current test.
//
// Returns:
//   - *TestContext: A fully initialized test context with Gomega and test options pre-configured.
//   - error: An error if the internal test context fails to initialize.
func NewTestContext(t *testing.T) (*TestContext, error) { //nolint:thelper
	tcf, err := testf.NewTestContext(
		testf.WithTOptions(
			testf.WithEventuallyTimeout(testOpts.TestTimeouts.defaultEventuallyTimeout),
			testf.WithEventuallyPollingInterval(testOpts.TestTimeouts.defaultEventuallyPollInterval),
			testf.WithConsistentlyDuration(testOpts.TestTimeouts.defaultConsistentlyTimeout),
			testf.WithConsistentlyPollingInterval(testOpts.TestTimeouts.defaultConsistentlyPollInterval),
		),
	)

	if err != nil {
		return nil, err
	}

	// Set up the global debug client for panic handling
	SetGlobalDebugClient(tcf.Client())

	return &TestContext{
		TestContext:                      tcf,
		g:                                tcf.NewWithT(t),
		logger:                           t,
		DSCInitializationNamespacedName:  types.NamespacedName{Name: dsciInstanceName},
		DataScienceClusterNamespacedName: types.NamespacedName{Name: dscInstanceName},
		OperatorNamespace:                testOpts.operatorNamespace,
		AppsNamespace:                    testOpts.appsNamespace,
		WorkbenchesNamespace:             testOpts.workbenchesNamespace,
		MonitoringNamespace:              testOpts.monitoringNamespace,
		TestTimeouts:                     testOpts.TestTimeouts,
	}, nil
}

// Logf logs a formatted message to the test output.
func (tc *TestContext) Logf(format string, args ...any) {
	tc.logger.Helper()
	tc.logger.Logf(format, args...)
}

// OverrideEventuallyTimeout temporarily changes the Eventually timeout and polling period.
func (tc *TestContext) OverrideEventuallyTimeout(timeout, pollInterval time.Duration) func() {
	// Save current timeout values (you'll need to store these manually)
	previousTimeout := tc.g.DurationBundle.EventuallyTimeout
	previousPollInterval := tc.g.DurationBundle.EventuallyPollingInterval

	// Override with new values
	tc.g.SetDefaultEventuallyTimeout(timeout)
	tc.g.SetDefaultEventuallyPollingInterval(pollInterval)

	// Return a function to reset them back
	return func() {
		// Override with new values
		tc.g.SetDefaultEventuallyTimeout(previousTimeout)
		tc.g.SetDefaultEventuallyPollingInterval(previousPollInterval)
	}
}

// NewResourceOptions creates and returns a ResourceOptions object
// It configures a ResourceOptions object by applying the provided ResourceOpts.
func (tc *TestContext) NewResourceOptions(opts ...ResourceOpts) *ResourceOptions {
	ro := &ResourceOptions{tc: tc}
	for _, opt := range opts {
		opt(ro)
	}

	// Ensure ObjFn is set and fetch the object.
	if ro.Obj == nil && ro.ObjFn != nil {
		// If Obj is not provided, call ObjFn to get the object.
		ro.Obj = ro.ObjFn(tc)
	}

	// Ensure that Obj is not nil before returning the options.
	if ro.Obj == nil {
		panic("Obj must be set in ResourceOptions") // Panics if Obj is nil to enforce validation.
	}

	// Ensure ListOptions is not nil before using it
	if ro.ListOptions == nil {
		ro.ListOptions = &client.ListOptions{}
	}

	// Ensure ClientDeleteOptions is not nil before using it
	if ro.ClientDeleteOptions == nil {
		ro.ClientDeleteOptions = &client.DeleteOptions{}
	}

	// Initialize DeleteAllOfOptions if nil
	if ro.DeleteAllOfOptions == nil {
		ro.DeleteAllOfOptions = make([]client.DeleteAllOfOption, 0)
	}

	// Ensure IgnoreNotFound is true by default
	ro.IgnoreNotFound = true

	return ro
}

// EnsureResourceExists verifies whether a specific Kubernetes resource exists in the cluster and optionally matches a given condition.
// If the resource exists and matches the condition (if provided), it will return the object.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
//
// Returns:
//   - *unstructured.Unstructured: The resource object if it exists and meets the condition (if provided).
func (tc *TestContext) EnsureResourceExists(opts ...ResourceOpts) *unstructured.Unstructured {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	var u *unstructured.Unstructured

	tc.g.Eventually(func(g Gomega) {
		// Use fetchResource to attempt to fetch the resource with retries
		u, _ = fetchResource(ro)

		// Ensure that the resource object is not nil
		g.Expect(u).NotTo(
			BeNil(),
			defaultErrorMessageIfNone(resourceNotFoundErrorMsg, []any{ro.ResourceID, ro.GVK.Kind}, ro.CustomErrorArgs)...,
		)

		// If a condition is provided via WithCondition, apply it inside the Eventually block
		if ro.Condition != nil {
			// Apply the provided condition matcher to the resource.
			applyMatchers(g, ro.ResourceID, ro.GVK, u, nil, ro.Condition, ro.CustomErrorArgs)
		}
	}).Should(Succeed())

	return u
}

// EnsureResourceExistsConsistently verifies that a Kubernetes resource exists and
// consistently matches a specified condition over a period of time.
//
// It repeatedly checks the resource using the provided condition for the specified `timeout` and `polling`
// intervals, ensuring the condition holds true consistently within the given time frame.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
//
// Returns:
//   - *unstructured.Unstructured: The resource that was found and matched.
func (tc *TestContext) EnsureResourceExistsConsistently(opts ...ResourceOpts) *unstructured.Unstructured {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	var u *unstructured.Unstructured

	// Ensure the resource exists and matches the condition consistently over the specified period.
	tc.g.Consistently(func(g Gomega) {
		// Use fetchResource to attempt to fetch the resource with retries
		u, _ = fetchResource(ro)

		// Ensure that the resource object is not nil
		g.Expect(u).NotTo(
			BeNil(),
			defaultErrorMessageIfNone(resourceNotFoundErrorMsg, []any{ro.ResourceID, ro.GVK.Kind}, ro.CustomErrorArgs)...,
		)

		// If a condition is provided via WithCondition, apply it inside the Eventually block
		if ro.Condition != nil {
			// Apply the provided condition matcher to the resource.
			applyMatchers(g, ro.ResourceID, ro.GVK, u, nil, ro.Condition, ro.CustomErrorArgs)
		}
	}).Should(Succeed())

	return u
}

// EventuallyResourceCreated attempts to create a new Kubernetes resource.
// This function calls Create() directly and will fail if the resource already exists.
// Use EventuallyResourceCreatedOrUpdated if you need creation-or-update semantics.
//
// Behavior is controlled by the following optional flags:
//   - WithObjectToCreate: Specifies the resource object to create (required).
//   - WithCondition: Validates the resource state after creation.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
//
// Returns:
//   - *unstructured.Unstructured: The newly created resource object.
func (tc *TestContext) EventuallyResourceCreated(opts ...ResourceOpts) *unstructured.Unstructured {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	// Default the condition to Succeed() if it's not provided.
	if ro.Condition == nil {
		ro.Condition = Succeed()
	}

	// Create adapter function to match the signature
	createFn := func(obj *unstructured.Unstructured, fn ...func(obj *unstructured.Unstructured) error) *testf.EventuallyValue[*unstructured.Unstructured] {
		if len(fn) > 0 && fn[0] != nil {
			if err := fn[0](obj); err != nil {
				tc.g.Expect(err).NotTo(HaveOccurred(), "failed to apply create mutation")
			}
		}
		return tc.g.Create(obj, ro.NN)
	}

	return eventuallyResourceApplied(ro, createFn)
}

// EventuallyResourceUpdated attempts to update an existing Kubernetes resource.
// This function calls Update() directly and will fail if the resource doesn't exist.
// Use EventuallyResourceCreatedOrUpdated if you need creation-or-update semantics.
//
// Behavior is controlled by the following optional flags:
//   - WithMutateFunc: Defines how to modify the resource during update (optional; if omitted, a no-op update is attempted).
//   - WithCondition: Validates the resource state after update.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
//
// Returns:
//   - *unstructured.Unstructured: The updated resource object.
func (tc *TestContext) EventuallyResourceUpdated(opts ...ResourceOpts) *unstructured.Unstructured {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	// Default the condition to Succeed() if it's not provided.
	if ro.Condition == nil {
		ro.Condition = Succeed()
	}

	updateFn := func(obj *unstructured.Unstructured, fn ...func(obj *unstructured.Unstructured) error) *testf.EventuallyValue[*unstructured.Unstructured] {
		// Default to no-op if no function provided
		mutationFn := func(obj *unstructured.Unstructured) error { return nil }
		if len(fn) > 0 && fn[0] != nil {
			mutationFn = fn[0]
		}
		return tc.g.Update(ro.GVK, ro.NN, mutationFn)
	}

	return eventuallyResourceApplied(ro, updateFn)
}

// EventuallyResourcePatched attempts to patch an existing Kubernetes resource.
// This function calls Patch() directly and will fail if the resource doesn't exist.
// Use EventuallyResourceCreatedOrPatched if you need creation-or-patch semantics.
//
// Behavior is controlled by the following optional flags:
//   - WithMutateFunc: Defines how to modify the resource during patch (required).
//   - WithCondition: Validates the resource state after patch.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
//
// Returns:
//   - *unstructured.Unstructured: The patched resource object.
func (tc *TestContext) EventuallyResourcePatched(opts ...ResourceOpts) *unstructured.Unstructured {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	// Default the condition to Succeed() if it's not provided.
	if ro.Condition == nil {
		ro.Condition = Succeed()
	}

	// Create adapter function to match the signature
	patchFn := func(obj *unstructured.Unstructured, fn ...func(obj *unstructured.Unstructured) error) *testf.EventuallyValue[*unstructured.Unstructured] {
		// Default to no-op if no function provided
		mutationFn := func(obj *unstructured.Unstructured) error { return nil }
		if len(fn) > 0 && fn[0] != nil {
			mutationFn = fn[0]
		}

		return tc.g.Patch(ro.GVK, ro.NN, mutationFn)
	}

	// Apply the resource using eventuallyResourceApplied with CreateOrPatch.
	return eventuallyResourceApplied(ro, patchFn)
}

// EventuallyResourceCreatedOrUpdated ensures that a given Kubernetes resource exists.
// If the resource is missing, it will be created; if it already exists, it will be updated
// using the provided mutation function. This function retries until the operation succeeds.
//
// Behavior is controlled by the following optional flags:
//   - WithObjectToCreate: Specifies the resource object to create or update.
//   - WithMutateFunc: Defines how to modify the resource during update operations.
//   - WithCondition: Validates the resource state after creation/update.
//   - WithIgnoreNotFound: Continues operation even if intermediate fetches fail.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
//
// Returns:
//   - *unstructured.Unstructured: The existing or newly created (updated) resource object.
func (tc *TestContext) EventuallyResourceCreatedOrUpdated(opts ...ResourceOpts) *unstructured.Unstructured {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	// Default the condition to Succeed() if it's not provided.
	if ro.Condition == nil {
		ro.Condition = Succeed()
	}

	// Apply the resource using eventuallyResourceApplied.
	return eventuallyResourceApplied(ro, tc.g.CreateOrUpdate)
}

// ConsistentlyResourceCreatedOrUpdated ensures that a given Kubernetes resource exists.
// If the resource is missing, it will be created; if it already exists, it will be updated
// using the provided mutation function. If a mutation function is provided, it applies the mutation
// once using Eventually, then verifies the resource consistently meets the expected condition over time
// using Consistently.
//
// Behavior is controlled by the following optional flags:
//   - WithObjectToCreate: Specifies the resource object to create or update.
//   - WithMutateFunc: Defines how to modify the resource during update operations (applied once using Eventually).
//   - WithCondition: Validates the resource state after creation/update using Consistently.
//   - WithIgnoreNotFound: Continues operation even if intermediate fetches fail.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
//
// Returns:
//   - *unstructured.Unstructured: The existing or newly created (updated) resource object.
func (tc *TestContext) ConsistentlyResourceCreatedOrUpdated(opts ...ResourceOpts) *unstructured.Unstructured {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	// Default the condition to Succeed() if it's not provided.
	if ro.Condition == nil {
		ro.Condition = Succeed()
	}

	// Apply the resource using consistentlyResourceApplied.
	return consistentlyResourceApplied(ro, tc.g.CreateOrUpdate)
}

// EventuallyResourceCreatedOrPatched ensures that a given Kubernetes resource exists.
// If the resource is missing, it will be created; if it already exists, it will be patched.
// If a condition is provided, it will be evaluated; otherwise, Succeed() is used.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
//
// Returns:
//   - *unstructured.Unstructured: The existing or newly created (patched) resource object.
func (tc *TestContext) EventuallyResourceCreatedOrPatched(opts ...ResourceOpts) *unstructured.Unstructured {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	// Default the condition to Succeed() if it's not provided
	if ro.Condition == nil {
		ro.Condition = Succeed()
	}

	// Apply the resource using eventuallyResourceApplied
	return eventuallyResourceApplied(ro, tc.g.CreateOrPatch)
}

// ConsistentlyResourceCreatedOrPatched ensures that a given Kubernetes resource exists.
// If the resource is missing, it will be created; if it already exists, it will be patched.
// If a mutation function is provided, it applies the mutation once using Eventually, then verifies
// the resource consistently meets the expected condition over time using Consistently.
//
// Behavior is controlled by the following optional flags:
//   - WithObjectToCreate: Specifies the resource object to create or patch.
//   - WithMutateFunc: Defines how to modify the resource during patch operations (applied once using Eventually).
//   - WithCondition: Validates the resource state after creation/patch using Consistently.
//   - WithIgnoreNotFound: Continues operation even if intermediate fetches fail.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
//
// Returns:
//   - *unstructured.Unstructured: The existing or newly created (patched) resource object.
func (tc *TestContext) ConsistentlyResourceCreatedOrPatched(opts ...ResourceOpts) *unstructured.Unstructured {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	// Default the condition to Succeed() if it's not provided.
	if ro.Condition == nil {
		ro.Condition = Succeed()
	}

	// Apply the resource using consistentlyResourceApplied.
	return consistentlyResourceApplied(ro, tc.g.CreateOrPatch)
}

// EnsureResourceDoesNotExist performs a one-time check to verify that a resource does not exist in the cluster.
//
// This function fetches the resource once and fails the test immediately if it exists.
// If an acceptable error is provided via WithAcceptableErr, it validates the error.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
//
// This function does not retry; use EnsureResourceGone if you need to wait for deletion.
func (tc *TestContext) EnsureResourceDoesNotExist(opts ...ResourceOpts) {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	err := tc.ensureResourceDoesNotExist(tc.g, ro)

	// Validate the error if an expected error is set
	if ro.AcceptableErrMatcher != nil {
		tc.g.Expect(err).To(ro.AcceptableErrMatcher, unexpectedErrorMismatchMsg, ro.AcceptableErrMatcher, err, ro.GVK.Kind)
	}
}

// EnsureResourceGone retries checking a resource until it is deleted or times out.
// If the resource still exists after the timeout, the test will fail.
//
// Behavior is controlled by the following optional flags:
//   - WithAcceptableErr: If provided, validates that the deletion produces the acceptable error.
//   - WithCustomErrorMsg: Customizes the error message if the resource is not deleted in time.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
func (tc *TestContext) EnsureResourceGone(opts ...ResourceOpts) {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	// For deletion scenarios, treat "not found" as success
	ro.IgnoreNotFound = true

	// Use Eventually to retry checking the resource until it disappears or timeout occurs
	tc.g.Eventually(func(g Gomega) {
		err := tc.ensureResourceDoesNotExist(g, ro)

		// Validate the error if an expected error is set
		if ro.AcceptableErrMatcher != nil {
			g.Expect(err).To(ro.AcceptableErrMatcher, unexpectedErrorMismatchMsg, ro.AcceptableErrMatcher, err, ro.GVK.Kind)
			return
		}

		// For deletion checks, we expect no error (resource should be gone)
		g.Expect(err).NotTo(HaveOccurred())
	}).Should(Succeed())
}

// EnsureResourcesExist ensures that a specific list of Kubernetes resources exists in the cluster.
// It will retry fetching the resources until they are found or the timeout occurs.
// If a condition is provided, it will retry the condition check on the resources until the condition is satisfied.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
//
// Returns:
//   - []unstructured.Unstructured: The list of resources if they exist and meet the condition (if provided).
func (tc *TestContext) EnsureResourcesExist(opts ...ResourceOpts) []unstructured.Unstructured {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	var resourcesList []unstructured.Unstructured

	tc.g.Eventually(func(g Gomega) {
		resourcesList, _ = fetchResourcesSync(ro)

		// If a condition is provided via WithCondition, apply it inside the Eventually block
		if ro.Condition != nil {
			// Apply the condition matcher (e.g., length check, label check, etc.)
			applyMatchers(g, ro.ResourceID, ro.GVK, resourcesList, nil, ro.Condition, ro.CustomErrorArgs)
		}

		// If no condition is provided, simply ensure the list is not empty
		g.Expect(resourcesList).NotTo(BeEmpty(), resourceEmptyErrorMsg, ro.ResourceID, ro.GVK.Kind)
	}).Should(Succeed())

	return resourcesList
}

// EnsureResourcesDoNotExist performs a one-time check to verify that a list of resources does not exist in the cluster.
//
// This function fetches the resources once and fails the test immediately if any exist.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
//
// This function does not retry; use EnsureResourcesGone if you need to wait for deletion.
func (tc *TestContext) EnsureResourcesDoNotExist(opts ...ResourceOpts) {
	// Create a ResourceOptions object based on the provided opts
	ro := tc.NewResourceOptions(opts...)

	_ = tc.ensureResourcesDoNotExist(tc.g, ro)
}

// EnsureResourcesGone waits for a list of resources to be deleted, retrying the check until they no longer exist.
//
// This function repeatedly checks if the resources are gone, failing the test only if they still exist after the timeout.
//
// Behavior is controlled by the following optional flags:
//   - WithDeleteAllOfOptions: Configures which resources to check for deletion (e.g., label selectors).
//   - WithCustomErrorMsg: Customizes the error message if resources are not deleted in time.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
func (tc *TestContext) EnsureResourcesGone(opts ...ResourceOpts) {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	// For deletion scenarios, treat "not found" as success
	ro.IgnoreNotFound = true

	// Use Eventually to retry checking the resource until it disappears or timeout occurs
	tc.g.Eventually(func(g Gomega) {
		err := tc.ensureResourcesDoNotExist(g, ro)
		g.Expect(err).NotTo(HaveOccurred())
	}).Should(Succeed())
}

// FetchActualSubscription gets a Subscription if it exists.
// It does not use eventually
//
// Parameters:
//   - nn (types.NamespacedName): The namespace and name of the Subscription.
//
// Returns:
//   - *ofapi.Subscription: The existing Subscription or nil if not found.
//   - error: Any error encountered during retrieval.
func (tc *TestContext) FetchActualSubscription(nn types.NamespacedName) (*ofapi.Subscription, error) {
	// Fetch Subscription synchronously to avoid nested Eventually.
	subU, err := fetchResourceSync(tc.NewResourceOptions(
		WithMinimalObject(gvk.Subscription, nn),
		WithCustomErrorMsg("Failed to fetch Subscription %s/%s", nn.Namespace, nn.Name),
	))
	if err != nil {
		return nil, err
	}
	if subU == nil {
		return nil, nil
	}
	sub := &ofapi.Subscription{}
	tc.convertToResource(subU, sub)

	return sub, nil
}

// EnsureSubscriptionExistsOrCreate ensures that the specified Subscription exists.
// If the Subscription is missing, it will be created; if it already exists, no action is taken.
// This function reuses the `EventuallyResourceCreatedOrUpdated` logic to guarantee that the Subscription
// exists or is created.
//
// Parameters:
//   - nn (types.NamespacedName): The namespace and name of the Subscription.
//
// Returns:
//   - *unstructured.Unstructured: The existing or newly created Subscription object.
func (tc *TestContext) EnsureSubscriptionExistsOrCreate(nn types.NamespacedName, channelName string) *unstructured.Unstructured {
	// Construct a resource identifier.
	resourceID := resources.FormatNamespacedName(nn)

	// Create the subscription object using the necessary values (adapt as needed)
	sub := tc.createSubscription(nn, channelName)

	// Ensure the Subscription exists or create it if missing
	return tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(sub),
		WithMutateFunc(testf.TransformSpecToUnstructured(sub.Spec)),
		WithCondition(jq.Match(`.status | has("installPlanRef")`)),
		WithCustomErrorMsg("Failed to ensure Subscription '%s' exists", resourceID),
	)
}

// EnsureResourcesAreEqual asserts that two resource objects are identical.
// Uses Gomega's `BeEquivalentTo` for a flexible deep comparison.
//
// Parameters:
//   - actualResource (interface{}): The resource to be compared.
//   - expectedResource (interface{}): The expected resource.
//   - args (...interface{}): Optional Gomega assertion message arguments.
func (tc *TestContext) EnsureResourcesAreEqual(actualResource, expectedResource interface{}, args ...any) {
	// Use Gomega's BeEquivalentTo for flexible deep comparison
	tc.g.Expect(actualResource).To(
		BeEquivalentTo(expectedResource),
		defaultErrorMessageIfNone(
			"Expected resource to be equal to the actual resource, but they differ.\nActual: %v\nExpected: %v", []any{actualResource, expectedResource},
			args,
		)...,
	)
}

// EnsureResourceNotNil verifies that the given resource is not nil and fails the test if it is.
//
// Parameters:
//   - obj (*unstructured.Unstructured): The resource object to check.
//   - args (...interface{}): Optional Gomega assertion message arguments.
func (tc *TestContext) EnsureResourceNotNil(obj any, args ...any) {
	tc.EnsureResourceConditionMet(obj, Not(BeNil()), args...)
}

// EnsureResourceConditionMet verifies that a given resource satisfies a specified condition.
// Callers should explicitly use `Not(matcher)` if they need to assert a negative condition.
//
// Parameters:
//   - obj (any): The resource object to check.
//   - condition: A Gomega matcher specifying the expected condition (e.g., BeEmpty(), Not(BeEmpty())).
//   - args (...interface{}): Optional Gomega assertion message arguments. If not provided, a default message is used.
func (tc *TestContext) EnsureResourceConditionMet(obj any, condition gTypes.GomegaMatcher, args ...any) {
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
		defaultErrorMessageIfNone(
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
func (tc *TestContext) EnsureDeploymentReady(nn types.NamespacedName, replicas int32) {
	// Construct a resource identifier.
	resourceID := resources.FormatNamespacedName(nn)

	// Ensure the deployment exists and retrieve the object.
	deployment := &appsv1.Deployment{}
	tc.FetchTypedResource(
		deployment,
		WithMinimalObject(gvk.Deployment, nn),
		WithCustomErrorMsg("Deployment %s was expected to exist but was not found", resourceID),
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
		"Expected %d ready replicas for deployment, but got %d", replicas, deployment.Status.ReadyReplicas)
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
func (tc *TestContext) EnsureCRDEstablished(name string) {
	// Ensure the CustomResourceDefinition exists and retrieve the object
	crd := &apiextv1.CustomResourceDefinition{}
	tc.FetchTypedResource(
		crd,
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: name}),
		WithCustomErrorMsg("CRD %s was expected to exist but was not found", name),
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

// UpdateComponentStateInDataScienceClusterWithKind updates the management state of a specified component kind in the DataScienceCluster.
//
// This function updates the component's management state in the DataScienceCluster and validates
// that both the spec and status are updated correctly, including the component's Ready condition.
//
// Parameters:
//   - state (operatorv1.ManagementState): The desired management state (e.g., Managed, Removed).
//   - kind (string): The component kind (e.g., "Dashboard", "Workbenches").
func (tc *TestContext) UpdateComponentStateInDataScienceClusterWithKind(state operatorv1.ManagementState, kind string) {
	componentName := strings.ToLower(kind)

	// Map DataSciencePipelines to aipipelines for v2 API
	componentFieldName := componentName
	conditionKind := kind
	const dataSciencePipelinesKind = "DataSciencePipelines"
	const aiPipelinesFieldName = "aipipelines"
	if kind == dataSciencePipelinesKind {
		componentFieldName = aiPipelinesFieldName
		conditionKind = "AIPipelines"
	}

	readyCondition := metav1.ConditionFalse
	if state == operatorv1.Managed {
		readyCondition = metav1.ConditionTrue
	}

	// Define common conditions to match.
	conditions := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentFieldName, state),

		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, conditionKind, readyCondition),
	}

	// Update the management state of the component in the DataScienceCluster.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentFieldName, state)),
		WithCondition(And(conditions...)),
	)
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
//   - obj (client.Object): The resource object to create, which must be convertible to an unstructured format.
//   - args (...interface{}): Optional Gomega assertion message arguments.
func (tc *TestContext) EnsureResourceIsUnique(obj client.Object, args ...any) {
	// Ensure obj is not nil before proceeding
	tc.g.Expect(obj).NotTo(BeNil(), resourceNotNilErrorMsg)

	// Convert the input object to unstructured
	u, err := resources.ObjectToUnstructured(tc.Scheme(), obj)
	tc.g.Expect(err).NotTo(HaveOccurred(), err)

	// Extract GroupVersionKind from the unstructured object
	groupVersionKind := u.GetObjectKind().GroupVersionKind()

	// Ensure that at least one resource of this kind already exists
	tc.EnsureResourcesExist(
		WithMinimalObject(groupVersionKind, types.NamespacedName{Namespace: u.GetNamespace()}),
		WithListOptions(&client.ListOptions{Namespace: u.GetNamespace()}),
		WithCustomErrorMsg("Failed to verify existence of %s", groupVersionKind.Kind),
	)

	// Attempt to create the duplicate resource, expecting failure
	tc.g.Eventually(func(g Gomega) {
		// Try to create the resource
		_, err := tc.g.Create(u, types.NamespacedName{Namespace: u.GetNamespace(), Name: u.GetName()}).Get()

		// If there's no error, that means the duplicate creation succeeded, which is a failure
		g.Expect(err).To(HaveOccurred(), defaultErrorMessageIfNone(
			"Expected creation of duplicate %s to fail due to uniqueness constraint, but it succeeded.",
			[]any{groupVersionKind.Kind},
			args,
		)...)

		// Check if the error is a Kubernetes StatusError and was denied by an admission webhook
		// Ensure the failure is due to uniqueness constraints (Forbidden error)
		g.Expect(k8serr.IsForbidden(err)).To(BeTrue(),
			defaultErrorMessageIfNone(
				"Expected failure due to uniqueness constraint (Forbidden), but got: %v",
				[]any{err},
				args,
			)...,
		)
	}).Should(Succeed())
}

// EnsureOperatorInstalledWithChannel ensures that an operator is installed via OLM with a specific channel.
//
// This function performs the following steps:
//  1. Creates the operator's namespace if it doesn't exist
//  2. Creates a Subscription for the operator with the specified channel
//  3. Waits for the CSV (ClusterServiceVersion) to reach "Succeeded" phase
//
// Parameters:
//   - nn (types.NamespacedName): The namespace and name of the operator being installed.
//   - channelName (string): The OLM channel to use for the operator installation (e.g., "stable", "alpha").
func (tc *TestContext) EnsureOperatorInstalledWithChannel(nn types.NamespacedName, channelName string) {
	tc.EnsureOperatorHealthy(nn, channelName, nil)
}

// EnsureOperatorInstalledWithGlobalOperatorGroupAndChannel ensures that an operator is installed via OLM with a specific channel.
//
// This function performs the following steps:
//  1. Creates the operator's namespace if it doesn't exist
//  2. Creates an OperatorGroup with global scope in the namespace
//  3. Creates a Subscription for the operator with the specified channel
//  4. Waits for the CSV (ClusterServiceVersion) to reach "Succeeded" phase
//
// Parameters:
//   - nn (types.NamespacedName): The namespace and name of the operator being installed.
//   - channelName (string): The OLM channel to use for the operator installation (e.g., "stable", "alpha").
func (tc *TestContext) EnsureOperatorInstalledWithGlobalOperatorGroupAndChannel(nn types.NamespacedName, channelName string) {
	tc.EnsureOperatorHealthy(nn, channelName, nil)
}

// EnsureOperatorInstalledWithLocalOperatorGroupAndChannel ensures that an operator is installed via OLM with a specific channel.
//
// This function performs the following steps:
//  1. Creates the operator's namespace if it doesn't exist
//  2. Creates an OperatorGroup targeting operator's namespace
//  3. Creates a Subscription for the operator with the specified channel
//  4. Waits for the CSV (ClusterServiceVersion) to reach "Succeeded" phase
//
// Parameters:
//   - nn (types.NamespacedName): The namespace and name of the operator being installed.
//   - channelName (string): The OLM channel to use for the operator installation (e.g., "stable", "alpha").
func (tc *TestContext) EnsureOperatorInstalledWithLocalOperatorGroupAndChannel(nn types.NamespacedName, channelName string) {
	tc.EnsureOperatorHealthy(nn, channelName, []string{nn.Namespace})
}

// EnsureOperatorHealthy validates that an operator is installed and healthy.
// If the operator is not present or unhealthy, it will be installed.
// This function is idempotent and works regardless of how the operator was initially installed.
func (tc *TestContext) EnsureOperatorHealthy(nn types.NamespacedName, channel string, targetNamespaces []string) {
	// Quick health check - if already healthy, we're done
	if tc.isOperatorHealthy(nn) {
		return
	}

	// Not healthy - ensure installation
	tc.ensureOperatorInstalled(nn, channel, targetNamespaces)

	// Wait for the operator to become healthy
	tc.ensureCSVSucceeded(nn)
}

// isOperatorHealthy checks if operator CSV is in Succeeded phase.
func (tc *TestContext) isOperatorHealthy(nn types.NamespacedName) bool {
	sub, err := tc.FetchActualSubscription(nn)
	if err != nil || sub == nil {
		return false
	}

	// Determine CSV name from subscription status (supports both currentCSV and installedCSV)
	csvName := extractSubscriptionCSVName(sub)
	if csvName == "" {
		return false
	}

	csv, err := fetchResource(
		tc.NewResourceOptions(
			WithMinimalObject(gvk.ClusterServiceVersion, types.NamespacedName{
				Namespace: nn.Namespace,
				Name:      csvName,
			}),
		),
	)

	if err != nil || csv == nil {
		return false
	}

	clusterServiceVersion := &ofapi.ClusterServiceVersion{}
	tc.convertToResource(csv, clusterServiceVersion)

	return clusterServiceVersion.Status.Phase == ofapi.CSVPhaseSucceeded
}

// ensureOperatorInstalled performs operator installation, handling pre-existing resources.
func (tc *TestContext) ensureOperatorInstalled(nn types.NamespacedName, channel string, targetNamespaces []string) {
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: nn.Namespace}),
		WithCustomErrorMsg("Failed to create namespace '%s'", nn.Namespace),
	)

	tc.ensureOperatorGroupExists(nn, targetNamespaces)
	tc.ensureInstallPlan(nn, channel)
}

// ensureOperatorGroupExists reuses existing OperatorGroups or creates one.
func (tc *TestContext) ensureOperatorGroupExists(nn types.NamespacedName, targetNamespaces []string) {
	existingOG := tc.fetchOperatorGroupInNamespace(nn.Namespace)
	if existingOG != nil {
		return // Reuse existing OperatorGroup
	}

	resourceID := resources.FormatNamespacedName(nn)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(tc.createOperatorGroup(nn, targetNamespaces)),
		WithCustomErrorMsg("Failed to create operator group '%s'", resourceID),
	)
}

// fetchOperatorGroupInNamespace discovers any OperatorGroup in the namespace.
func (tc *TestContext) fetchOperatorGroupInNamespace(namespace string) *operatorsv1.OperatorGroup {
	operatorGroups := tc.FetchResources(
		WithMinimalObject(gvk.OperatorGroup, types.NamespacedName{Namespace: namespace}),
		WithListOptions(&client.ListOptions{Namespace: namespace}),
	)

	if len(operatorGroups) == 0 {
		return nil
	}

	og := &operatorsv1.OperatorGroup{}
	tc.convertToResource(&operatorGroups[0], og)

	return og
}

// ensureCSVSucceeded waits for CSV to reach the Succeeded phase.
func (tc *TestContext) ensureCSVSucceeded(nn types.NamespacedName) {
	resourceID := resources.FormatNamespacedName(nn)

	// Fetch Subscription
	subscription := &ofapi.Subscription{}
	tc.FetchTypedResource(subscription,
		WithMinimalObject(gvk.Subscription, nn),
		WithCustomErrorMsg("Failed to fetch Subscription %s", resourceID),
	)

	// Extract CSV name
	csvName := extractSubscriptionCSVName(subscription)
	tc.g.Expect(csvName).NotTo(BeEmpty(), "Subscription %s has no current/installed CSV", resourceID)

	tc.g.Eventually(func(g Gomega) {
		// Fetch CSV
		csv, err := tc.FetchActualClusterServiceVersion(types.NamespacedName{Namespace: nn.Namespace, Name: csvName})
		g.Expect(err).NotTo(HaveOccurred(), "error fetching CSV %s/%s", nn.Namespace, csvName)
		g.Expect(csv).NotTo(BeNil(), "CSV %s/%s not found", nn.Namespace, csvName)
		g.Expect(csv.Status.Phase).To(Equal(ofapi.CSVPhaseSucceeded),
			"CSV %s is not Succeeded: %s", csvName, csv.Status.Phase)
	}).WithTimeout(tc.TestTimeouts.longEventuallyTimeout).
		WithPolling(tc.TestTimeouts.defaultEventuallyPollInterval).
		Should(Succeed())
}

// ensureInstallPlan is a helper function that retrieves and approves an operator's InstallPlan,
// then waits for the associated ClusterServiceVersion (CSV) to reach the 'Succeeded' phase.
//
// This function performs the following steps:
//  1. Creates a Subscription for the operator with the specified channel
//  2. Approves the InstallPlan if it's not already approved (required in CI environments)
//  3. Extracts the CSV name from the InstallPlan
//  4. Waits for the CSV to reach 'Succeeded' phase, indicating successful installation
//
// Parameters:
//   - nn (types.NamespacedName): The namespace and name of the operator subscription.
//   - channelName (string): The OLM channel used for the operator installation.
func (tc *TestContext) ensureInstallPlan(nn types.NamespacedName, channelName string) {
	// Construct a resource identifier.
	resourceID := resources.FormatNamespacedName(nn)

	// Retrieve the InstallPlan
	plan := tc.FetchInstallPlan(nn, channelName)

	// in CI InstallPlan is in Manual mode
	if !plan.Spec.Approved {
		tc.ApproveInstallPlan(plan)
	}

	// Retrieve the CSV name from the InstallPlan and ensure it reaches 'Succeeded' phase.
	tc.g.Expect(plan.Spec.ClusterServiceVersionNames).NotTo(BeEmpty(), "No CSV found in InstallPlan for operator '%s'", resourceID)
	csvName := plan.Spec.ClusterServiceVersionNames[0] // Assuming first in the list

	tc.g.Eventually(func(g Gomega) {
		// Fetch CSV
		csv, err := tc.FetchActualClusterServiceVersion(types.NamespacedName{Namespace: nn.Namespace, Name: csvName})
		g.Expect(err).NotTo(HaveOccurred(), "error fetching CSV %s/%s", nn.Namespace, csvName)
		g.Expect(csv).NotTo(BeNil(), "CSV %s/%s not found", nn.Namespace, csvName)

		g.Expect(csv.Status.Phase).To(
			Equal(ofapi.CSVPhaseSucceeded),
			"CSV %s did not reach 'Succeeded' phase", resourceID,
		)
	}).WithTimeout(tc.TestTimeouts.mediumEventuallyTimeout).
		WithPolling(tc.TestTimeouts.defaultEventuallyPollInterval).
		Should(Succeed())
}

// DeleteResource deletes a specific Kubernetes resource by name.
//
// Behavior is controlled by the following optional flags:
//   - WithIgnoreNotFound: If true, skips existence check and ignores NotFound errors during deletion.
//   - WithWaitForDeletion: If true, waits until the resource is fully deleted from the cluster.
//   - WithWaitForRecreation: If true, waits for the resource to be recreated after deletion (useful for managed resources).
//   - WithRemoveFinalizersOnDelete: If true, removes all finalizers before deletion to prevent stuck deletions.
//   - WithClientDeleteOptions: Configures deletion behavior (e.g., propagation policy).
//
// Parameters:
//   - opts(...ResourceOpts): Optional options for configuring the resource and deletion behavior.
func (tc *TestContext) DeleteResource(opts ...ResourceOpts) {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	if !ro.IgnoreNotFound {
		// Ensure the resource exists before attempting deletion
		tc.EnsureResourceExists(
			WithMinimalObject(ro.GVK, ro.NN),
			WithCustomErrorMsg("Expected %s instance %s to exist before attempting deletion", ro.GVK.Kind, ro.ResourceID),
		)
	}

	// Remove finalizers if requested, before attempting deletion
	if ro.RemoveFinalizersOnDelete {
		// Try to remove finalizers in a best-effort manner
		// If this fails (e.g., due to validation errors), we continue with deletion anyway
		tc.tryRemoveFinalizers(ro.GVK, ro.NN)
	}

	// Perform the delete and handle errors appropriately
	err := tc.g.Delete(ro.GVK, ro.NN, ro.ClientDeleteOptions).Get()

	// Handle errors that should cause early return when IgnoreNotFound is true
	if err != nil && ro.IgnoreNotFound {
		if meta.IsNoMatchError(err) || k8serr.IsNotFound(err) {
			return // CRD doesn't exist or resource already gone - nothing to delete or wait for
		}
	}

	// For all remaining cases, expect success
	tc.g.Expect(err).NotTo(HaveOccurred(), "Failed to delete %s instance %s", ro.GVK.Kind, ro.ResourceID)

	if ro.WaitForDeletion {
		opts = append(opts, WithCustomErrorMsg("Resource %s instance %s was not fully deleted", ro.GVK.Kind, ro.ResourceID))
		tc.EnsureResourceGone(opts...)
	}

	if ro.WaitForRecreation {
		// Wait for the resource to be recreated after deletion
		// This helps with controllers that immediately recreate managed resources
		tc.EnsureResourceExists(
			WithMinimalObject(ro.GVK, ro.NN),
			WithCustomErrorMsg("Resource %s instance %s was not recreated after deletion", ro.GVK.Kind, ro.ResourceID),
		)
	}
}

// DeleteResources deletes all Kubernetes resources of a specific type matching the given criteria.
// It uses DeleteAllOf internally for efficient bulk deletion without requiring pre-fetching.
//
// Important: When deleting resources in a specific namespace, use WithNamespaceFilter() instead of setting
// the Namespace field in WithMinimalObject(). The Namespace in NamespacedName is ignored for bulk operations.
//
// Behavior is controlled by the following optional flags:
//   - WithNamespaceFilter: Filters deletion to resources in a specific namespace.
//   - WithDeleteAllOfOptions: Configures the bulk deletion criteria (e.g., label selectors, field selectors).
//   - WithWaitForDeletion: If true, waits until all matching resources are fully deleted from the cluster.
//
// Parameters:
//   - opts(...ResourceOpts): Optional options for configuring the resource type and deletion behavior.
func (tc *TestContext) DeleteResources(opts ...ResourceOpts) {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	// Perform the bulk delete using the configured DeleteAllOfOptions
	tc.g.DeleteAll(
		ro.GVK,
		ro.DeleteAllOfOptions...,
	).Eventually().Should(Succeed(),
		"Failed to delete %s resources", ro.GVK.Kind)

	if ro.WaitForDeletion {
		// Wait for all matching resources to be gone
		tc.EnsureResourcesGone(opts...)
	}
}

// EnsureResourceDeletedThenRecreated provides a robust deletion-recreation test pattern
// that handles the race condition between client deletion and controller recreation.
//
// This function:
//  1. Captures the original resource's UID and ResourceVersion
//  2. Deletes the resource using DeleteResource (respects all deletion options)
//  3. Waits for the controller to recreate it with a new UID/ResourceVersion
//  4. Verifies the recreated resource has different identity metadata
//
// Behavior is controlled by the following optional flags:
//   - All DeleteResource options are supported for the deletion phase.
//
// Parameters:
//   - opts(...ResourceOpts): Optional options for configuring the deletion and recreation behavior.
//
// Returns:
//   - *unstructured.Unstructured: The recreated resource with new UID and ResourceVersion.
func (tc *TestContext) EnsureResourceDeletedThenRecreated(opts ...ResourceOpts) *unstructured.Unstructured {
	ro := tc.NewResourceOptions(opts...)

	// Step 1: Capture original resource metadata
	originalResource := tc.EnsureResourceExists(opts...)
	originalUID := originalResource.GetUID()
	originalResourceVersion := originalResource.GetResourceVersion()

	// Step 2: Delete the resource using standard deletion
	tc.DeleteResource(opts...)

	// Step 2.5: Ensure the resource is actually deleted before looking for recreation
	tc.g.Eventually(func(g Gomega) {
		// Use direct client.Get() instead of fetchResource() to avoid nested Eventually calls
		u, err := fetchResourceSync(ro)
		if err != nil {
			// For NotFound errors, the resource is successfully deleted
			if k8serr.IsNotFound(err) {
				return // Resource is successfully deleted
			}
			g.Expect(err).NotTo(HaveOccurred(), "Failed to fetch %s %s during deletion check", ro.GVK.Kind, ro.ResourceID)
			return // This fails the Eventually iteration, causing retry
		}
		if u == nil {
			return // Resource is successfully deleted or not found
		}
		// If resource still exists, check if it has the same UID (deletion not acknowledged yet)
		g.Expect(u.GetUID()).NotTo(Equal(originalUID),
			"Resource deletion not yet acknowledged: resource still exists with original UID")
		// If it has a different UID, it was already recreated, which is fine
	}).Should(Succeed(), "Resource %s %s deletion was not acknowledged within timeout", ro.GVK.Kind, ro.ResourceID)

	// Step 3: Wait for controller to recreate with new identity
	// (UID-based verification automatically handles deletion acknowledgment)

	// Brief wait to allow controller-runtime cache to update after deletion
	// This prevents cache staleness issues where operator thinks deleted resource still exists
	time.Sleep(controllerCacheRefreshDelay)

	var recreatedResource *unstructured.Unstructured

	tc.g.Eventually(func(g Gomega) {
		// Poll without nesting Eventually to avoid compounded timeouts
		// Use direct client.Get() instead of fetchResource() to avoid nested Eventually calls
		u, err := fetchResourceSync(ro)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to fetch %s %s", ro.GVK.Kind, ro.ResourceID)
		g.Expect(u).NotTo(BeNil(), "Expected %s %s to be recreated", ro.GVK.Kind, ro.ResourceID)
		recreatedResource = u

		// Verify it's actually a new resource (different UID)
		newUID := recreatedResource.GetUID()
		newResourceVersion := recreatedResource.GetResourceVersion()

		// Debug logging to understand what's happening
		if newUID == originalUID {
			// This indicates the resource was never actually deleted, just log and continue polling
			return
		}

		g.Expect(newUID).NotTo(Equal(originalUID),
			"Recreated resource should have different UID. Original: %s, New: %s", originalUID, newUID)
		g.Expect(newResourceVersion).NotTo(Equal(originalResourceVersion),
			"Recreated resource should have different ResourceVersion. Original: %s, New: %s",
			originalResourceVersion, newResourceVersion)
	}).Should(Succeed(),
		"Resource %s %s was not properly recreated with new identity", ro.GVK.Kind, ro.ResourceID)

	return recreatedResource
}

// FetchInstallPlanName retrieves the name of the InstallPlan associated with a subscription.
// It ensures that the subscription exists (or is created) and then retrieves the InstallPlan name.
// This function does not return an error, it will panic if anything goes wrong (such as a missing InstallPlanRef).
//
// Parameters:
//   - name (string): The name of the Subscription to check.
//   - ns (string): The namespace of the Subscription.
//
// Returns:
//   - string: The name of the InstallPlan associated with the Subscription.
func (tc *TestContext) FetchInstallPlanName(nn types.NamespacedName, channelName string) string {
	// Ensure the subscription exists or is created
	u := tc.EnsureSubscriptionExistsOrCreate(nn, channelName)

	// Convert the Unstructured object to Subscription and assert no error
	sub := &ofapi.Subscription{}
	tc.convertToResource(u, sub)

	// Return the name of the InstallPlan
	return sub.Status.InstallPlanRef.Name
}

// FetchInstallPlan retrieves the InstallPlan associated with a Subscription by its name and namespace.
// It ensures the Subscription exists (or is created) and fetches the InstallPlan object by its name and namespace.
//
// Parameters:
//   - name (string): The name of the Subscription to check.
//   - ns (string): The namespace of the Subscription.
//
// Returns:
//   - *ofapi.InstallPlan: The InstallPlan associated with the Subscription.
func (tc *TestContext) FetchInstallPlan(nn types.NamespacedName, channelName string) *ofapi.InstallPlan {
	// Retrieve the InstallPlan name using getInstallPlanName (ensuring Subscription exists if necessary)
	planName := tc.FetchInstallPlanName(nn, channelName)

	// Ensure the InstallPlan exists and retrieve the object.
	installPlan := &ofapi.InstallPlan{}
	tc.FetchTypedResource(
		installPlan,
		WithMinimalObject(gvk.InstallPlan, types.NamespacedName{Namespace: nn.Namespace, Name: planName}),
		WithCustomErrorMsg("InstallPlan %s was expected to exist but was not found", planName),
	)

	// Return the InstallPlan object
	return installPlan
}

// FetchActualClusterServiceVersion retrieves a ClusterServiceVersion (CSV) for an operator by name and namespace.
// If the CSV does not exist, the function will fail the test using Gomega assertions.
// It does not use eventually
//
// Parameters:
//   - nn (types.NamespacedName): The coordinates of the ClusterServiceVersion to retrieve.
//
// Returns:
//   - *ofapi.ClusterServiceVersion: A pointer to the retrieved ClusterServiceVersion object.
func (tc *TestContext) FetchActualClusterServiceVersion(nn types.NamespacedName) (*ofapi.ClusterServiceVersion, error) {
	// Fetch CSV synchronously to avoid nested Eventually.
	csvU, err := fetchResourceSync(tc.NewResourceOptions(
		WithMinimalObject(gvk.ClusterServiceVersion, nn),
		WithCustomErrorMsg("Failed to fetch CSV %s/%s", nn.Namespace, nn.Name),
	))
	if err != nil {
		return nil, err
	}
	if csvU == nil {
		return nil, nil
	}
	csv := &ofapi.ClusterServiceVersion{}
	tc.convertToResource(csvU, csv)

	return csv, nil
}

// FetchClusterVersion retrieves the ClusterVersion for the cluster.
// If the ClusterVersion does not exist, the function will fail the test using Gomega assertions.
//
// Returns:
//   - *configv1.ClusterVersion: A pointer to the retrieved ClusterVersion object.
func (tc *TestContext) FetchClusterVersion() *configv1.ClusterVersion {
	// Retrieve the ClusterVersion
	cv := &configv1.ClusterVersion{}
	tc.FetchTypedResource(cv, WithMinimalObject(gvk.ClusterVersion, types.NamespacedName{Name: cluster.OpenShiftVersionObj}))

	// Assert that we found the ClusterVersion
	tc.g.Expect(cv).NotTo(BeNil(), "ClusterVersion not found")

	return cv
}

// FetchPlatformRelease retrieves the platform release name from the DSCInitialization resource.
//
// This function ensures that the DSCInitialization resource and its status exist before accessing
// the release name. If any required field is missing, the function will fail the test using Gomega assertions.
//
// Returns:
//   - common.Platform: The platform release name retrieved from the DSCInitialization resource.
func (tc *TestContext) FetchPlatformRelease() common.Platform {
	// Fetch the DSCInitialization object
	dsci := tc.FetchDSCInitialization()

	// Ensure that the DSCInitialization object has a non-nil release name
	tc.g.Expect(dsci.Status.Release.Name).NotTo(BeEmpty(), "DSCI release name should not be empty")

	return dsci.Status.Release.Name
}

// FetchDSCInitialization retrieves the DSCInitialization resource.
//
// This function ensures that the DSCInitialization resource exists and then retrieves it
// as a strongly typed object.
//
// Returns:
//   - *dsciv2.DSCInitialization: The retrieved DSCInitialization object.
func (tc *TestContext) FetchDSCInitialization() *dsciv2.DSCInitialization {
	// Ensure the DSCInitialization exists and retrieve the object
	dsci := &dsciv2.DSCInitialization{}
	tc.FetchTypedResource(dsci, WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName))

	return dsci
}

// FetchDataScienceCluster retrieves the DataScienceCluster resource.
//
// This function ensures that the DataScienceCluster resource exists and then retrieves it
// as a strongly typed object.
//
// Returns:
//   - *dsciv2.DataScienceCluster: The retrieved DataScienceCluster object.
func (tc *TestContext) FetchDataScienceCluster() *dscv2.DataScienceCluster {
	// Ensure the DataScienceCluster exists and retrieve the object
	dsc := &dscv2.DataScienceCluster{}
	tc.FetchTypedResource(dsc, WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName))

	return dsc
}

// FetchResource ensures a Kubernetes resource exists and retrieves it as an Unstructured object.
//
// Parameters:
//   - opts(...ResourceOpts): Functional options to configure the resource retrieval.
//
// Returns:
//   - *unstructured.Unstructured: The retrieved resource in unstructured format.
func (tc *TestContext) FetchResource(opts ...ResourceOpts) *unstructured.Unstructured {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	// Use fetchResource to attempt to fetch the resources with retries
	r, _ := fetchResource(ro)

	return r
}

// FetchTypedResource ensures a Kubernetes resource exists and retrieves it as a typed object.
//
// This function first ensures the resource exists using `EnsureResourceExists`, then converts
// the Unstructured object into the provided typed object.
//
// Parameters:
//   - obj (client.Object): The target object where the retrieved resource should be stored.
//   - opts(...ResourceOpts): Functional options to configure the resource retrieval.
//
// Panics:
//   - If the resource does not exist.
//   - If conversion from Unstructured to the typed object fails.
func (tc *TestContext) FetchTypedResource(obj client.Object, opts ...ResourceOpts) {
	// Ensure the resource exists and retrieve the object
	u := tc.EnsureResourceExists(opts...)

	// Convert and store it in the provided object
	tc.convertToResource(u, obj)
}

// FetchResources fetches a list of Kubernetes resources from the cluster and fails the test if retrieval fails.
//
// Parameters:
//   - opts(...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
//
// Returns:
//   - []unstructured.Unstructured: A list of resources fetched from the cluster.
func (tc *TestContext) FetchResources(opts ...ResourceOpts) []unstructured.Unstructured {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	// Use fetchResources to attempt to fetch the resources with retries
	resourcesList, _ := fetchResources(ro)

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
	obj := tc.createInstallPlan(plan.Name, plan.Namespace, plan.Spec.ClusterServiceVersionNames)

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
			"Failed to approve InstallPlan %s in namespace %s: %v", obj.Name, obj.Namespace, err,
		)
}

// CheckOperatorExists checks if an operator with name starting with operatorNamePrefix exists.
//
// This function searches for operators (CSVs) in the cluster that match the given name prefix.
// It's commonly used to verify operator installation status before performing operator-dependent tests.
//
// Parameters:
//   - operatorNamePrefix (string): The prefix of the operator name to search for.
//
// Returns:
//   - bool: True if an operator matching the prefix is found, false otherwise.
//   - error: Any error encountered during the search operation.
func (tc *TestContext) CheckOperatorExists(operatorNamePrefix string) (bool, error) {
	operatorInfo, err := cluster.OperatorExists(tc.Context(), tc.Client(), operatorNamePrefix)
	return operatorInfo != nil, err
}

// EnsureWebhookBlocksResourceCreation verifies that webhook validation blocks creation of resources with invalid values.
//
// This function attempts to create a resource and expects the operation to fail with a BadRequest error from the webhook.
// It validates that the error message contains expected content such as field names and invalid values.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
func (tc *TestContext) EnsureWebhookBlocksResourceCreation(opts ...ResourceOpts) {
	tc.EnsureWebhookBlocksOperation(func() error {
		ro := tc.NewResourceOptions(opts...)
		_, err := tc.g.Create(ro.Obj, ro.NN).Get()
		return err
	}, "creation", opts...)
}

// EnsureWebhookBlocksResourceUpdate verifies that webhook validation blocks updates to resources with invalid values.
//
// This function attempts to update a resource using the provided mutation function and expects the operation to fail
// with a Forbidden error from the webhook. It validates that the error message contains expected invalid values.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
func (tc *TestContext) EnsureWebhookBlocksResourceUpdate(opts ...ResourceOpts) {
	tc.EnsureWebhookBlocksOperation(func() error {
		ro := tc.NewResourceOptions(opts...)
		_, err := tc.g.Update(ro.GVK, ro.NN, ro.MutateFunc).Get()
		return err
	}, "update", opts...)
}

// convertToResource converts an Unstructured object to the specified resource type.
// It asserts that no error occurs during the conversion.
// EnsureWebhookBlocksOperation verifies that webhook validation blocks a specific operation.
//
// This is the core generic function that handles webhook validation testing for any operation.
// It expects the operation to fail with a Forbidden error from the webhook and validates
// that the error message contains expected patterns.
//
// Parameters:
//   - operation (func() error): The operation function that should be blocked by the webhook.
//   - operationType (string): A descriptive name for the operation type (e.g., "creation", "update").
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior.
func (tc *TestContext) EnsureWebhookBlocksOperation(operation func() error, operationType string, opts ...ResourceOpts) {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	tc.g.Eventually(func(g Gomega) {
		// Execute the operation that should be blocked
		err := operation()

		// Handle error validation based on WithAcceptableErr or default webhook validation
		if ro.AcceptableErrMatcher != nil {
			// WithAcceptableErr was specified - validate specific error type
			g.Expect(err).To(ro.AcceptableErrMatcher, "Expected specific error type but got: %v", err)
			return
		}

		// Default webhook validation behavior
		// Expect the operation to fail
		g.Expect(err).To(HaveOccurred(),
			defaultErrorMessageIfNone(
				"Expected %s of %s resource to fail due to webhook validation",
				[]any{operationType, ro.GVK.Kind},
				ro.CustomErrorArgs,
			)...)

		// Validate that it's a webhook validation error, not an infrastructure issue
		tc.validateWebhookError(g, err, operationType, ro)
	}).Should(Succeed(), defaultErrorMessageIfNone(
		"Failed to validate webhook blocking behavior for %s of %s resource",
		[]any{operationType, ro.GVK.Kind},
		ro.CustomErrorArgs,
	)...)
}

// UninstallOperator uninstalls an operator by deleting its subscription and related resources.
// This method gracefully handles missing operators and validates resource structure during uninstallation.
//
// The uninstallation process:
//  1. Checks if the operator subscription exists
//  2. Extracts related resources (CSV, InstallPlan) from subscription status
//  3. Deletes the subscription first, then related resources
//  4. Uses WithIgnoreNotFound for resilient cleanup
//
// Parameters:
//   - operatorNamespacedName (types.NamespacedName): The namespace and name of the operator subscription
//   - opts (variadic ResourceOpts): Optional resource options, such as WithWaitForDeletion(true)
func (tc *TestContext) UninstallOperator(operatorNamespacedName types.NamespacedName, opts ...ResourceOpts) {
	// Construct a resource identifier.
	resourceID := resources.FormatNamespacedName(operatorNamespacedName)

	// Create subscription options with default settings
	subscriptionOpts := []ResourceOpts{
		WithMinimalObject(gvk.Subscription, operatorNamespacedName),
		WithIgnoreNotFound(true),
	}
	// Merge with provided options (provided options override defaults)
	subscriptionOpts = append(subscriptionOpts, opts...)

	// Fetch the subscription if it exists.
	sub, err := tc.FetchActualSubscription(operatorNamespacedName)
	if err != nil {
		// Log error but don't fail immediately, as this is often used in cleanup
		tc.Logf("Error fetching Subscription %s: %v", resourceID, err)
		return
	}
	if sub == nil {
		// Subscription doesn't exist, nothing to uninstall
		return
	}

	// Extract CSV and InstallPlan names with proper error handling
	csv := extractSubscriptionCSVName(sub)
	foundCSV := csv != ""

	installPlan := ""
	if sub.Status.InstallPlanRef != nil {
		installPlan = sub.Status.InstallPlanRef.Name
	}

	namespace := sub.Namespace
	tc.g.Expect(namespace).NotTo(BeEmpty(),
		"Subscription %s should have a namespace", operatorNamespacedName.Name)

	// Delete subscription first - this prevents new installations
	tc.DeleteResource(subscriptionOpts...)

	// Delete CSV if found and valid
	if foundCSV {
		csvOpts := []ResourceOpts{WithIgnoreNotFound(true), WithMinimalObject(gvk.ClusterServiceVersion, types.NamespacedName{Name: csv, Namespace: namespace})}
		csvOpts = append(csvOpts, opts...) // Add user-provided options
		tc.DeleteResource(csvOpts...)
	}

	// Delete InstallPlan if found and valid
	if installPlan != "" {
		installPlanOpts := []ResourceOpts{WithIgnoreNotFound(true), WithMinimalObject(gvk.InstallPlan, types.NamespacedName{Name: installPlan, Namespace: namespace})}
		installPlanOpts = append(installPlanOpts, opts...) // Add user-provided options
		tc.DeleteResource(installPlanOpts...)
	}
}

// extractSubscriptionCSVName returns the CSV name referenced by a Subscription status.
func extractSubscriptionCSVName(subscription *ofapi.Subscription) string {
	if subscription == nil {
		return ""
	}
	if subscription.Status.CurrentCSV != "" {
		return subscription.Status.CurrentCSV
	}
	if subscription.Status.InstalledCSV != "" {
		return subscription.Status.InstalledCSV
	}
	return ""
}

func (tc *TestContext) convertToResource(u *unstructured.Unstructured, obj client.Object) {
	// Convert Unstructured object to the given resource object
	err := resources.ObjectFromUnstructured(tc.Scheme(), u, obj)
	tc.g.Expect(err).NotTo(HaveOccurred(), "Failed converting %T from Unstructured.Object: %v", obj, u)
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
	u, err := fetchResource(ro)

	// If we have an error, let the caller handle it
	if err != nil {
		return err
	}

	// Assert that the resource does not exist - if it exists, test fails here
	g.Expect(u).To(BeNil(), resourceFoundErrorMsg, ro.ResourceID, ro.GVK.Kind)

	return nil // Only reached if assertion passes (u == nil)
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
	resourcesList, err := fetchResources(ro)

	// Handle "not found" errors based on IgnoreNotFound setting
	if ro.IgnoreNotFound && k8serr.IsNotFound(err) {
		// For deletion scenarios, "not found" means success - resource is gone
		return nil
	}

	// If we have an error that's not "not found", let the caller handle it
	if err != nil {
		return err
	}

	// Ensure that the resources list is empty (resources should not exist)
	g.Expect(resourcesList).To(BeEmpty(), resourceListNotEmptyErrorMsg, ro.ResourceID, ro.GVK.Kind)

	return nil
}

// createSubscription creates a Subscription object.
func (tc *TestContext) createSubscription(nn types.NamespacedName, channelName string) *ofapi.Subscription {
	subscription := &ofapi.Subscription{
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
			Channel:                channelName,
			Package:                nn.Name,
			InstallPlanApproval:    ofapi.ApprovalAutomatic,
		},
	}

	return subscription
}

// createOperatorGroup creates an OperatorGroup object.
func (tc *TestContext) createOperatorGroup(nn types.NamespacedName, targetNamespaces []string) *operatorsv1.OperatorGroup {
	operatorGroup := &operatorsv1.OperatorGroup{
		TypeMeta: metav1.TypeMeta{
			Kind:       operatorsv1.OperatorGroupKind,
			APIVersion: operatorsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},
	}

	if len(targetNamespaces) > 0 {
		operatorGroup.Spec = operatorsv1.OperatorGroupSpec{
			TargetNamespaces: targetNamespaces,
		}
	}

	return operatorGroup
}

// createInstallPlan creates an InstallPlan object.
func (tc *TestContext) createInstallPlan(name string, ns string, csvNames []string) *ofapi.InstallPlan {
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

// validateWebhookError validates that an error is a proper webhook validation error.
//
// This helper function checks that the error is a Forbidden (HTTP 403) error from webhook
// validation and validates that the error message contains expected patterns.
//
// Parameters:
//   - g (Gomega): The Gomega assertion wrapper.
//   - err (error): The error to validate.
//   - operationType (string): A descriptive name for the operation type.
//   - ro (*ResourceOptions): Resource options containing validation criteria.
func (tc *TestContext) validateWebhookError(g Gomega, err error, operationType string, ro *ResourceOptions) {
	// admission.Denied() returns HTTP 403 Forbidden, admission.Errored() returns 400/500
	isValidWebhookError := k8serr.IsForbidden(err) || k8serr.IsBadRequest(err) || k8serr.IsInternalError(err)
	g.Expect(isValidWebhookError).To(BeTrue(),
		defaultErrorMessageIfNone(
			"Expected webhook validation error (403 Forbidden, 400 Bad Request, or 500 Internal Server Error) for %s, got: %v",
			[]any{operationType, err},
			ro.CustomErrorArgs,
		)...)

	// Validate error message content
	errorMsg := err.Error()

	// Field name validation if provided
	if ro.FieldName != "" {
		g.Expect(errorMsg).To(Or(
			ContainSubstring(ro.FieldName),
			ContainSubstring(strings.ToLower(ro.FieldName)),
		), defaultErrorMessageIfNone(
			"Expected error message to reference field '%s' for %s, got: %s",
			[]any{ro.FieldName, operationType, errorMsg},
			ro.CustomErrorArgs,
		)...)
	}

	// Invalid value validation if provided
	if ro.InvalidValue != "" {
		g.Expect(errorMsg).To(ContainSubstring(ro.InvalidValue),
			defaultErrorMessageIfNone(
				"Expected error message to contain invalid value '%s' for %s, got: %s",
				[]any{ro.InvalidValue, operationType, errorMsg},
				ro.CustomErrorArgs,
			)...)
	}
}

// tryRemoveFinalizers attempts to remove finalizers from a resource in a best-effort manner.
// If the operation fails (e.g., due to validation errors, resource not found, etc.),
// it logs the failure but does not propagate the error, allowing deletion to proceed.
func (tc *TestContext) tryRemoveFinalizers(gvk schema.GroupVersionKind, nn types.NamespacedName) {
	defer func() {
		if r := recover(); r != nil {
			// Intentionally suppress panics from tryRemoveFinalizers to prevent
			// cleanup failures from breaking test execution
			_ = r // Explicitly ignore the recovered value
		}
	}()

	// Try to remove finalizers by fetching the existing resource first
	// This avoids validation issues with minimal objects that have empty specs
	tc.EventuallyResourcePatched(
		WithFetchedObject(gvk, nn),
		WithMutateFunc(testf.Transform(`.metadata.finalizers = []`)),
		WithIgnoreNotFound(true),
		WithAcceptableErr(func(err error) bool {
			if err == nil {
				return false
			}
			// Accept various cleanup-related errors to make tryRemoveFinalizers more robust
			return meta.IsNoMatchError(err) || // CRD doesn't exist
				k8serr.IsNotFound(err) || // Resource doesn't exist
				k8serr.IsInvalid(err) || // Resource validation errors
				k8serr.IsConflict(err) || // Resource version conflicts
				strings.Contains(err.Error(), "resourceVersion should not be set on objects to be created") // Generic resource version creation errors
		}, "AcceptableCleanupError"),
	)
}

// CheckMinOCPVersion checks if the OpenShift cluster version meets the minimum required version.
//
// This helper function checks if the current OpenShift cluster version is greater than or equal
// to the specified minimum version. It's useful for skipping tests or enabling features based
// on OpenShift version requirements.
//
// Parameters:
//   - minVersion (string): The minimum required version in semver format (e.g., "4.18.0", "4.17.9")
//
// Returns:
//   - bool: true if the cluster version meets the minimum requirement, false otherwise
//   - error: error if version parsing fails
func (tc *TestContext) CheckMinOCPVersion(minVersion string) (bool, error) {
	currentVersion := cluster.GetClusterInfo().Version
	requiredVersion, err := semver.ParseTolerant(minVersion)
	if err != nil {
		// If we can't parse the version, log error and return false for safety
		return false, fmt.Errorf("failed to parse minimum version requirement %s: %w", minVersion, err)
	}

	// Check if current version is greater than or equal to required version
	return currentVersion.GTE(requiredVersion), nil
}

// SkipIfOCPVersionBelow is a test helper that skips the current test if the OpenShift cluster
// version is below the specified minimum version.
//
// This is a convenience wrapper around CheckMinOCPVersion specifically designed for test skipping.
// It automatically calls t.Skipf() with a descriptive message when the version requirement is not met.
//
// Parameters:
//   - t (*testing.T): The test instance to skip
//   - minVersion (string): The minimum required version in semver format (e.g., "4.18.0")
//   - reason (string): Description of why this version is required (appears in skip message)
func (tc *TestContext) SkipIfOCPVersionBelow(t *testing.T, minVersion string, reason string) {
	t.Helper()
	meets, err := tc.CheckMinOCPVersion(minVersion)
	tc.g.Expect(err).ShouldNot(HaveOccurred(), "Failed to check OCP version")
	if !meets {
		t.Skipf("Skipping test: requires OpenShift %s or above for %s, current version: %s",
			minVersion, reason, cluster.GetClusterInfo().Version.String())
	}
}

// FetchSingleResourceOfKind fetches the first resource of a given GVK in a namespace.
// This is useful for external operator CRs where the name may vary.
func (tc *TestContext) FetchSingleResourceOfKind(resourceGVK schema.GroupVersionKind, namespace string) *unstructured.Unstructured {
	tc.Logf("Fetching single %s resource in namespace %q.", resourceGVK.Kind, namespace)
	var result *unstructured.Unstructured

	tc.g.Eventually(func(g Gomega) {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(resourceGVK)

		err := tc.Client().List(tc.Context(), list, client.InNamespace(namespace))
		g.Expect(err).NotTo(HaveOccurred(), "Failed to list %s resources in namespace %s", resourceGVK.Kind, namespace)

		g.Expect(list.Items).To(HaveLen(1), "Expected exactly one %s resource in namespace %s, got %d", resourceGVK.Kind, namespace, len(list.Items))

		result = &list.Items[0]
	}).Should(Succeed(), "Failed to fetch single %s resource in namespace %s", resourceGVK.Kind, namespace)

	return result
}

// InjectConditionIntoResourceStatus injects a condition into a resource's status.conditions field.
// This is useful for testing things such as external operator degradation scenarios.
func (tc *TestContext) InjectConditionIntoResourceStatus(
	resource *unstructured.Unstructured,
	conditionType string,
	conditionStatus metav1.ConditionStatus,
	reason, message string,
) {
	tc.Logf("Injecting condition %s=%s into resource %s/%s.", conditionType, conditionStatus, resource.GetNamespace(), resource.GetName())

	tc.g.Eventually(func(g Gomega) {
		// Refresh, mutate, update, then re-fetch and assert final state in one Eventually
		err := testf.RefreshResource(tc.Context(), tc.Client(), resource)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to refresh resource %s/%s", resource.GetNamespace(), resource.GetName())

		conditions, err := testf.ExtractTypedConditions(resource)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to extract conditions from resource")

		meta.SetStatusCondition(&conditions, metav1.Condition{
			Type:    conditionType,
			Status:  conditionStatus,
			Reason:  reason,
			Message: message,
		})
		err = testf.SetTypedConditions(resource, conditions)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to set conditions on resource")

		err = tc.Client().Status().Update(tc.Context(), resource)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to update resource status with condition %s=%s", conditionType, conditionStatus)

		// Re-fetch and assert the condition is present with expected status/reason/message
		err = testf.RefreshResource(tc.Context(), tc.Client(), resource)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to re-fetch resource %s/%s after update", resource.GetNamespace(), resource.GetName())

		conditions, err = testf.ExtractTypedConditions(resource)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to re-extract conditions from resource")

		found := meta.FindStatusCondition(conditions, conditionType)
		g.Expect(found).NotTo(BeNil(), "Expected condition %s to be present", conditionType)
		g.Expect(found.Status).To(Equal(conditionStatus), "Expected condition %s status %s", conditionType, conditionStatus)
		if reason != "" {
			g.Expect(found.Reason).To(Equal(reason), "Expected condition %s reason %s", conditionType, reason)
		}
		if message != "" {
			g.Expect(found.Message).To(ContainSubstring(message), "Expected condition %s message to contain %q", conditionType, message)
		}
	}).Should(Succeed(), "Failed to inject condition %s=%s into resource %s/%s",
		conditionType, conditionStatus, resource.GetNamespace(), resource.GetName())
}

// RemoveConditionFromResourceStatus removes a condition from a resource's status.conditions field.
// This is useful for testing recovery from external operator degradation scenarios.
func (tc *TestContext) RemoveConditionFromResourceStatus(
	resource *unstructured.Unstructured,
	conditionType string,
) {
	tc.Logf("Removing condition %s from resource %s/%s.", conditionType, resource.GetNamespace(), resource.GetName())

	tc.g.Eventually(func(g Gomega) {
		// Refresh, mutate, update, then re-fetch and assert absence in one Eventually
		err := testf.RefreshResource(tc.Context(), tc.Client(), resource)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to refresh resource %s/%s", resource.GetNamespace(), resource.GetName())

		conditions, err := testf.ExtractTypedConditions(resource)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to extract conditions from resource")

		meta.RemoveStatusCondition(&conditions, conditionType)
		err = testf.SetTypedConditions(resource, conditions)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to set conditions on resource")

		err = tc.Client().Status().Update(tc.Context(), resource)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to update resource status to remove condition %s", conditionType)

		// Re-fetch and assert the condition is absent
		err = testf.RefreshResource(tc.Context(), tc.Client(), resource)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to re-fetch resource %s/%s after update", resource.GetNamespace(), resource.GetName())

		conditions, err = testf.ExtractTypedConditions(resource)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to re-extract conditions from resource")
		found := meta.FindStatusCondition(conditions, conditionType)
		g.Expect(found).To(BeNil(), "Expected condition %s to be absent", conditionType)
	}).Should(Succeed(), "Failed to remove condition %s from resource %s/%s",
		conditionType, resource.GetNamespace(), resource.GetName())
}

// ClearAllConditionsFromResourceStatus removes all conditions from a resource's status.conditions.
// Useful to start tests from a clean slate when the external operator is paused.
func (tc *TestContext) ClearAllConditionsFromResourceStatus(
	resource *unstructured.Unstructured,
) {
	tc.Logf("Clearing all conditions from resource %s/%s.", resource.GetNamespace(), resource.GetName())

	tc.g.Eventually(func(g Gomega) {
		// Refresh, clear, update, then re-fetch and assert empty in one Eventually
		err := testf.RefreshResource(tc.Context(), tc.Client(), resource)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to refresh resource %s/%s", resource.GetNamespace(), resource.GetName())

		conditions := []metav1.Condition{}
		err = testf.SetTypedConditions(resource, conditions)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to clear conditions on resource")

		err = tc.Client().Status().Update(tc.Context(), resource)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to update resource status after clearing conditions")

		// Re-fetch and assert conditions are empty/absent
		err = testf.RefreshResource(tc.Context(), tc.Client(), resource)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to re-fetch resource %s/%s after update", resource.GetNamespace(), resource.GetName())

		conditions, err = testf.ExtractTypedConditions(resource)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to re-extract conditions from resource")
		g.Expect(conditions).To(BeEmpty(), "Expected no conditions after clear on %s/%s", resource.GetNamespace(), resource.GetName())
	}).Should(Succeed(), "Failed to clear all conditions from resource %s/%s",
		resource.GetNamespace(), resource.GetName())
}

// ScaleCSVDeploymentReplicas patches a ClusterServiceVersion to change the replica count
// of a specific deployment. This method blocks until the deployment reaches the target replica count.
//
// This is useful for testing scenarios where we need, for e.g., to scale down an external dependency to 0 replicas, without removing the operator.
//
// Parameters:
//   - namespace: The namespace where the CSV is installed
//   - csvNamePrefix: Prefix to match the CSV name (e.g., "kueue" matches "kueue.v0.10.0")
//   - deploymentName: The name of the deployment within the CSV to scale
//   - replicas: The target replica count
//
// Returns:
//   - int: The replica count of the deployment before scaling
func (tc *TestContext) ScaleCSVDeploymentReplicas(
	namespace string,
	csvNamePrefix string,
	deploymentName string,
	replicas int32,
) int32 {
	tc.Logf("Listing CSVs in namespace %s to find one with prefix %q.", namespace, csvNamePrefix)
	csvList := &ofapi.ClusterServiceVersionList{}
	err := tc.Client().List(tc.Context(), csvList, client.InNamespace(namespace))
	tc.g.Expect(err).NotTo(HaveOccurred(), "Failed to list CSVs in namespace %s", namespace)

	csvIdx := slices.IndexFunc(csvList.Items, func(csv ofapi.ClusterServiceVersion) bool {
		return strings.HasPrefix(csv.Name, csvNamePrefix)
	})
	tc.g.Expect(csvIdx).NotTo(Equal(-1), "No CSV found with prefix %s in namespace %s", csvNamePrefix, namespace)
	targetCSV := &csvList.Items[csvIdx]
	tc.Logf("Found CSV %s.", targetCSV.Name)

	tc.Logf("Looking for deployment %s in CSV spec.", deploymentName)
	deployments := targetCSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs
	depIdx := slices.IndexFunc(deployments, func(d ofapi.StrategyDeploymentSpec) bool {
		return d.Name == deploymentName
	})
	tc.g.Expect(depIdx).NotTo(Equal(-1), "Deployment %s not found in CSV %s", deploymentName, targetCSV.Name)

	spec := &deployments[depIdx].Spec
	originalReplicas := ptr.Deref(spec.Replicas, 1)
	spec.Replicas = ptr.To(replicas)
	tc.Logf("Scaling deployment %s from %d to %d replicas.", deploymentName, originalReplicas, replicas)

	tc.Logf("Updating CSV %s.", targetCSV.Name)
	err = tc.Client().Update(tc.Context(), targetCSV)
	tc.g.Expect(err).NotTo(HaveOccurred(), "Failed to update CSV %s", targetCSV.Name)

	tc.Logf("Waiting for deployment %s to reach %d replicas.", deploymentName, replicas)
	deploymentNN := types.NamespacedName{Name: deploymentName, Namespace: namespace}
	if replicas == 0 {
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.Deployment, deploymentNN),
			WithCondition(jq.Match(`.status.replicas == 0 or .status.replicas == null`)),
			WithCustomErrorMsg("Deployment %s should have 0 replicas", deploymentName),
		)
	} else {
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.Deployment, deploymentNN),
			WithCondition(jq.Match(`.status.readyReplicas == %d`, replicas)),
			WithCustomErrorMsg("Deployment %s should have %d ready replicas", deploymentName, replicas),
		)
	}

	return originalReplicas
}
