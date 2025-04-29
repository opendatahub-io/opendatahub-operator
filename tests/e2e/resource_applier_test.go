package e2e_test

import (
	"reflect"

	"github.com/onsi/gomega/matchers"
	gTypes "github.com/onsi/gomega/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

// Check if the matcher is an error matcher like Succeed(), HaveOccurred(), etc.
func isErrorMatcher(matcher gTypes.GomegaMatcher) bool {
	switch matcher.(type) {
	case *matchers.SucceedMatcher, *matchers.HaveOccurredMatcher, *matchers.MatchErrorMatcher:
		return true
	default:
		return false
	}
}

// isNumericMatcher checks if the given matcher is used for numeric comparisons, such as BeNumerically.
func isNumericMatcher(matcher gTypes.GomegaMatcher) bool {
	switch matcher.(type) {
	case *matchers.BeNumericallyMatcher, // for numerical comparison
		*matchers.EqualMatcher,          // for equality check
		*matchers.BeEquivalentToMatcher: // for equivalence check
		return true
	default:
		return false
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
			g.Expect(err).To(v, defaultErrorMessageIfNone(
				"Expected error when applying resource '%s' of kind '%s'.",
				[]any{resourceID, gvk.Kind}, args,
			)...)
			return
		}

		// If working on a list of resources
		if resourcesList, ok := resource.([]unstructured.Unstructured); ok {
			// If the matcher applies to numeric values, apply it to len(resourcesList)
			if isNumericMatcher(v) {
				g.Expect(len(resourcesList)).To(v, defaultErrorMessageIfNone(
					"Expected number of '%s' resources to match condition %v, but found %d.",
					[]any{gvk.Kind, dereferenceCondition(condition), len(resourcesList)}, args,
				)...)
			} else {
				// Apply the matcher to the list
				g.Expect(resourcesList).To(v, defaultErrorMessageIfNone(
					"Expected list of '%s' resources to match condition %v.",
					[]any{gvk.Kind, dereferenceCondition(condition)}, args,
				)...)
			}
			return
		}

		// If working on a single resource
		if u, ok := resource.(*unstructured.Unstructured); ok {
			g.Expect(u.Object).To(v, defaultErrorMessageIfNone(
				"Expected resource '%s' of kind '%s' to match condition %v.",
				[]any{resourceID, gvk.Kind, dereferenceCondition(condition)}, args,
			)...)
		}
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

// Helper function to evaluate if failure is expected based on the matcher.
func isFailureExpected(condition gTypes.GomegaMatcher) bool {
	// Check if the condition is an And, Or, or Not and recursively inspect the inner matchers
	switch v := condition.(type) {
	case *matchers.AndMatcher:
		// If the condition is And(), we recursively check the inner matchers
		for _, inner := range v.Matchers {
			if isFailureExpected(inner) {
				return true
			}
		}
	case *matchers.OrMatcher:
		// If the condition is Or(), we recursively check the inner matchers
		for _, inner := range v.Matchers {
			if isFailureExpected(inner) {
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

// ensureResourceApplied is a common function for handling create/patch and create/update operations.
// It applies a given resource and ensures it meets the expected condition.
//
// Parameters:
//   - ro: The ResourceOptions object that contains all the configuration for the resource, condition, and mutation function.
//   - applyResourceFn: The function responsible for applying the resource (create, patch, etc.).
//
// Returns:
//   - *unstructured.Unstructured: The applied resource if it meets the expected condition.
func ensureResourceApplied(
	ro *ResourceOptions,
	applyResourceFn func(obj *unstructured.Unstructured, fn ...func(obj *unstructured.Unstructured) error) *testf.EventuallyValue[*unstructured.Unstructured],
) *unstructured.Unstructured {
	// Wrap condition if it's not already wrapped correctly
	wrapConditionIfNeeded(&ro.Condition)

	// Use Eventually to retry getting the resource until it appears
	var u *unstructured.Unstructured
	ro.tc.g.Eventually(func(innerG Gomega) {
		// Fetch the resource
		var err error
		u, err = applyResourceFn(ro.Obj, ro.MutateFunc).Get()

		// Evaluate the condition to check if failure is expected
		expectingFailure := isFailureExpected(ro.Condition)

		// Error handling based on failure expectation
		if !expectingFailure {
			// Expect no error if success is expected
			innerG.Expect(err).NotTo(
				HaveOccurred(),
				"Error occurred while applying the resource '%s' of kind '%s': %v",
				ro.ResourceID,
				ro.GVK.Kind,
				err,
			)

			// Ensure that the resource object is not nil
			innerG.Expect(u).NotTo(BeNil(), resourceNotFoundErrorMsg, ro.ResourceID, ro.GVK.Kind)
		} else {
			// Expect error if failure is expected
			innerG.Expect(err).To(HaveOccurred(), "Expected applyResourceFn to fail but it succeeded")
		}

		// Apply the matchers based on the condition
		applyMatchers(innerG, ro.ResourceID, ro.GVK, u, err, ro.Condition, ro.CustomErrorArgs)
	}).Should(Succeed())

	return u
}
