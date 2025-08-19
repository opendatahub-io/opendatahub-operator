package e2e_test

import (
	"strings"
	"testing"
	"time"

	"github.com/onsi/gomega/gstruct"
	gTypes "github.com/onsi/gomega/types"
	configv1 "github.com/openshift/api/config/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

// TestContext holds shared context and utilities used during E2E test execution.
type TestContext struct {
	// Embeds the common test context (e.g., cluster clients, config)
	*testf.TestContext

	// Shared Gomega wrapper for making assertions in tests.
	g *testf.WithT

	// Test timeouts
	TestTimeouts TestTimeouts

	// Namespace where the operator components are deployed.
	OperatorNamespace string

	// Namespace where application workloads are deployed.
	AppsNamespace string

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

	return &TestContext{
		TestContext:                      tcf,
		g:                                tcf.NewWithT(t),
		DSCInitializationNamespacedName:  types.NamespacedName{Name: dsciInstanceName},
		DataScienceClusterNamespacedName: types.NamespacedName{Name: dscInstanceName},
		OperatorNamespace:                testOpts.operatorNamespace,
		AppsNamespace:                    testOpts.appsNamespace,
		MonitoringNamespace:              testOpts.monitoringNamespace,
		TestTimeouts:                     testOpts.TestTimeouts,
	}, nil
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
		// Use ensureResourceExistsOrNil to attempt to fetch the resource with retries
		u, _ = tc.ensureResourceExistsOrNil(ro)

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
		// Use ensureResourceExistsOrNil to attempt to fetch the resource with retries
		u, _ = tc.ensureResourceExistsOrNil(ro)

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

// EventuallyResourceCreatedOrUpdated ensures that a given Kubernetes resource exists.
// If the resource is missing, it will be created; if it already exists, it will be updated
// using the provided mutation function. Conditions in ResourceOpts are evaluated with eventually.
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

// EventuallyResourceCreatedOrUpdated ensures that a given Kubernetes resource exists.
// If the resource is missing, it will be created; if it already exists, it will be updated
// using the provided mutation function. Conditions in ResourceOpts are evaluated with consistently.
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

	// Apply the resource using eventuallyResourceApplied.
	return consistentlyResourceApplied(ro, tc.g.CreateOrUpdate)
}

// EnsureResourceCreatedOrPatched ensures that a given Kubernetes resource exists.
// If the resource is missing, it will be created; if it already exists, it will be patched.
// If a condition is provided, it will be evaluated; otherwise, Succeed() is used.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
//
// Returns:
//   - *unstructured.Unstructured: The existing or newly created (patched) resource object.
func (tc *TestContext) EnsureResourceCreatedOrPatched(opts ...ResourceOpts) *unstructured.Unstructured {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	// Default the condition to Succeed() if it's not provided
	if ro.Condition == nil {
		ro.Condition = Succeed()
	}

	// Apply the resource using eventuallyResourceApplied
	return eventuallyResourceApplied(ro, tc.g.CreateOrPatch)
}

// EnsureResourceDoesNotExist performs a one-time check to verify that a resource does not exist in the cluster.
//
// This function fetches the resource once and fails the test immediately if it exists.
// If an expected error is provided via WithExpectedErr, it validates the error.
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
	if ro.ExpectedErr != nil {
		tc.g.Expect(err).To(MatchError(ro.ExpectedErr), unexpectedErrorMismatchMsg, ro.ExpectedErr, err, ro.GVK.Kind)
	}
}

// EnsureResourceGone retries checking a resource until it is deleted or times out.
// If the resource still exists after the timeout, the test will fail.
//
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
func (tc *TestContext) EnsureResourceGone(opts ...ResourceOpts) {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	// Use Eventually to retry checking the resource until it disappears or timeout occurs
	tc.g.Eventually(func(g Gomega) {
		err := tc.ensureResourceDoesNotExist(g, ro)

		// Validate the error if an expected error is set
		if ro.ExpectedErr != nil {
			g.Expect(err).To(MatchError(ro.ExpectedErr), unexpectedErrorMismatchMsg, ro.ExpectedErr, err, ro.GVK.Kind)
		}
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
		resourcesList, _ := fetchResources(ro)

		// If no condition is provided, simply ensure the list is not empty
		g.Expect(resourcesList).NotTo(BeEmpty(), resourceEmptyErrorMsg, ro.ResourceID, ro.GVK.Kind)

		// If a condition is provided via WithCondition, apply it inside the Eventually block
		if ro.Condition != nil {
			// Apply the condition matcher (e.g., length check, label check, etc.)
			applyMatchers(g, ro.ResourceID, ro.GVK, resourcesList, nil, ro.Condition, ro.CustomErrorArgs)
		}
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
// Parameters:
//   - opts (...ResourceOpts): Optional functional arguments that customize the behavior of the operation.
func (tc *TestContext) EnsureResourcesGone(opts ...ResourceOpts) {
	// Create a ResourceOptions object based on the provided opts.
	ro := tc.NewResourceOptions(opts...)

	// Use Eventually to retry checking the resource until it disappears or timeout occurs
	tc.g.Eventually(func(g Gomega) {
		err := tc.ensureResourcesDoNotExist(g, ro)
		g.Expect(err).NotTo(HaveOccurred())
	}).Should(Succeed())
}

// FetchSubscription get a subscription if exists.
//
// Parameters:
//   - nn (types.NamespacedName): The namespace and name of the Subscription.
//
// Returns:
//   - *unstructured.Unstructured: The existing subscription or nil.
func (tc *TestContext) GetSubscription(nn types.NamespacedName, channelName string) *unstructured.Unstructured {
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
	tc.EnsureOperatorInstalledWithChannel(nn, skipOperatorGroupCreation, defaultOperatorChannel)
}

func (tc *TestContext) EnsureOperatorInstalledWithChannel(nn types.NamespacedName, skipOperatorGroupCreation bool, channelName string) {
	// Construct a resource identifier.
	resourceID := resources.FormatNamespacedName(nn)

	// Ensure the operator's namespace is created.
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: nn.Namespace}),
		WithCustomErrorMsg("Failed to create or update namespace '%s'", nn.Namespace),
	)

	// Ensure the operator group is created or updated only if necessary.
	if !skipOperatorGroupCreation {
		tc.EventuallyResourceCreatedOrUpdated(
			WithMinimalObject(gvk.OperatorGroup, nn),
			WithCustomErrorMsg("Failed to create or update operator group '%s'", resourceID),
		)
	}

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
		csv := tc.FetchClusterServiceVersion(types.NamespacedName{Namespace: nn.Namespace, Name: csvName})
		g.Expect(csv.Status.Phase).To(
			Equal(ofapi.CSVPhaseSucceeded),
			"CSV %s did not reach 'Succeeded' phase", resourceID,
		)
	}).WithTimeout(tc.TestTimeouts.mediumEventuallyTimeout).WithPolling(tc.TestTimeouts.defaultEventuallyPollInterval)
}

// DeleteResource deletes a Kubernetes resource. If IgnoreNotFound is set via WithIgnoreNotFound,
// the function will not check for existence beforehand and will silently ignore if the resource does not exist.
//
// If WaitForDeletion is set via WithWaitForDeletion, the function will wait until the resource is fully deleted.
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

	// Perform the delete (client gracefully handles NotFound already)
	tc.g.Delete(
		ro.GVK,
		ro.NN,
		ro.ClientDeleteOptions,
	).Eventually().Should(Succeed(), "Failed to delete %s instance %s", ro.GVK.Kind, ro.ResourceID)

	if ro.WaitForDeletion {
		opts = append(opts, WithCustomErrorMsg("Resource %s instance %s was not fully deleted", ro.GVK.Kind, ro.ResourceID))
		tc.EnsureResourceGone(opts...)
	}
}

// EnsureResourceDeletedThenRecreated provides a robust deletion-recreation test pattern
// that handles the race condition between client deletion and controller recreation.
func (tc *TestContext) EnsureResourceDeletedThenRecreated(opts ...ResourceOpts) *unstructured.Unstructured {
	ro := tc.NewResourceOptions(opts...)

	// Step 1: Capture original resource metadata
	originalResource := tc.EnsureResourceExists(opts...)
	originalUID := string(originalResource.GetUID())
	originalResourceVersion := originalResource.GetResourceVersion()

	// Step 2: Delete the resource using standard deletion
	tc.DeleteResource(opts...)

	// Step 3: Wait for controller to recreate with new identity
	var recreatedResource *unstructured.Unstructured
	tc.g.Eventually(func(g Gomega) {
		// Apply grace period if specified
		if ro.GracePeriod > 0 {
			time.Sleep(ro.GracePeriod)
		}

		recreatedResource = tc.EnsureResourceExists(opts...)

		// Verify it's actually a new resource (different UID)
		newUID := string(recreatedResource.GetUID())
		newResourceVersion := recreatedResource.GetResourceVersion()

		g.Expect(newUID).NotTo(Equal(originalUID),
			"Recreated resource should have different UID. Original: %s, New: %s", originalUID, newUID)
		g.Expect(newResourceVersion).NotTo(Equal(originalResourceVersion),
			"Recreated resource should have different ResourceVersion. Original: %s, New: %s",
			originalResourceVersion, newResourceVersion)
	}).WithTimeout(10*time.Minute).Should(Succeed(),
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

// FetchClusterServiceVersion retrieves a ClusterServiceVersion (CSV) for an operator by name and namespace.
// If the CSV does not exist, the function will fail the test using Gomega assertions.
//
// Parameters:
//   - nn (types.NamespacedName): The coordinates of the ClusterServiceVersion to retrieve.
//
// Returns:
//   - *ofapi.ClusterServiceVersion: A pointer to the retrieved ClusterServiceVersion object.
func (tc *TestContext) FetchClusterServiceVersion(nn types.NamespacedName) *ofapi.ClusterServiceVersion {
	// Construct a resource identifier.
	resourceID := resources.FormatNamespacedName(nn)

	// Retrieve the CSV
	csv := &ofapi.ClusterServiceVersion{}
	tc.FetchTypedResource(csv, WithMinimalObject(gvk.ClusterServiceVersion, nn))

	// Assert that we found the CSV
	tc.g.Expect(csv).NotTo(BeNil(), "CSV %s not found", resourceID)

	return csv
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
//   - *dsciv1.DSCInitialization: The retrieved DSCInitialization object.
func (tc *TestContext) FetchDSCInitialization() *dsciv1.DSCInitialization {
	// Ensure the DSCInitialization exists and retrieve the object
	dsci := &dsciv1.DSCInitialization{}
	tc.FetchTypedResource(dsci, WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName))

	return dsci
}

// FetchDataScienceCluster retrieves the DataScienceCluster resource.
//
// This function ensures that the DataScienceCluster resource exists and then retrieves it
// as a strongly typed object.
//
// Returns:
//   - *dsciv1.DataScienceCluster: The retrieved DataScienceCluster object.
func (tc *TestContext) FetchDataScienceCluster() *dscv1.DataScienceCluster {
	// Ensure the DataScienceCluster exists and retrieve the object
	dsc := &dscv1.DataScienceCluster{}
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
	resourcesList, _ := fetchResource(ro)

	return resourcesList
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

// Check if an operator with name starting with operatorNamePrefix exists.
func (tc *TestContext) CheckOperatorExists(operatorNamePrefix string) (bool, error) {
	return cluster.OperatorExists(tc.Context(), tc.Client(), operatorNamePrefix)
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

func (tc *TestContext) convertToResource(u *unstructured.Unstructured, obj client.Object) {
	// Convert Unstructured object to the given resource object
	err := resources.ObjectFromUnstructured(tc.Scheme(), u, obj)
	tc.g.Expect(err).NotTo(HaveOccurred(), "Failed converting %T from Unstructured.Object: %v", obj, u)
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
	u, err := fetchResource(ro)

	// Ensure no unexpected errors occurred while fetching the resource
	ro.tc.g.Expect(err).NotTo(
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
	u, err := fetchResource(ro)

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
	resourcesList, err := fetchResources(ro)

	// Ensure that the resources list is empty (resources should not exist)
	g.Expect(resourcesList).To(BeEmpty(), resourceListNotEmptyErrorMsg, ro.ResourceID, ro.GVK.Kind)

	return err
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
	// Expect the error to be a Forbidden (HTTP 403) from the webhook validation
	g.Expect(k8serr.IsForbidden(err)).To(BeTrue(),
		defaultErrorMessageIfNone(
			"Expected Forbidden error from webhook validation for %s, got: %v",
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
