package e2e_test

import (
	"time"

	gTypes "github.com/onsi/gomega/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

// ==============================
//        RESOURCE OPTIONS
// ==============================

// ResourceOptions encapsulates the various configuration options for resource handling operations,
// such as fetching, creating, updating, patching, and deleting resources. It allows customization of
// the behavior of resource handling functions by setting custom error messages, conditions, and resource
// fetch or manipulation logic. These options are used across a range of functions to perform operations
// on Kubernetes resources with flexible configuration.
type ResourceOptions struct {
	// tc is the test context that provides access to the Gomega configuration,
	// including default timeouts and polling intervals for Eventually and Consistently assertions.
	tc *TestContext

	// ObjFn is responsible for retrieving or creating the resource.
	ObjFn func(*TestContext) *unstructured.Unstructured

	// Cached object after ObjFn is called.
	// Holds the result of the ObjFn call and is used throughout subsequent operations.
	Obj *unstructured.Unstructured

	// ListOptions for resource listing, such as label selectors.
	// This field defines the filtering criteria when listing resources, such as label selectors,
	// field selectors, or other Kubernetes-specific options to narrow down the resource list.
	ListOptions *client.ListOptions

	// ClientDeleteOptions defines the behavior of resource deletion.
	// This field holds the options that control how a resource should be deleted (e.g., cascading deletion
	// policy). It is used when calling Kubernetes client methods for resource
	// deletion. Deletion behavior such as the propagation policy (Foreground, Background, or Orphan) can
	// be set via these options. It allows fine-grained control over how the resource is removed from the cluster.
	ClientDeleteOptions *client.DeleteOptions

	// MutateFunc is responsible for modifying the resource before it is applied (e.g., setting fields or configurations).
	// This function can be used to mutate the resource object before applying any changes to it.
	MutateFunc func(obj *unstructured.Unstructured) error

	// Condition is the Gomega matcher that checks the final condition of the resource.
	// This matcher allows the user to specify conditions on the resource that should be met in order for the
	// operation to be considered successful. For example, checking if the resource has the correct status or labels.
	Condition gTypes.GomegaMatcher

	// Custom error message and arguments for error handling.
	// A customizable error message template with placeholders for dynamic values. This can be used to provide
	// detailed error messages when operations fail.
	CustomErrorArgs []any

	// AcceptableErrMatcher is a Gomega matcher for error validation during resource operations.
	// Use WithAcceptableErr() to set this - it creates the appropriate MatchError matcher.
	AcceptableErrMatcher gTypes.GomegaMatcher

	// GroupVersionKind and NamespacedName of the resource.
	// These fields define the type (GVK) and identifier (NN) for the resource. GVK is used to specify the
	// kind of resource (e.g., Pod, Deployment), and NamespacedName specifies the resource's namespace and name.
	GVK schema.GroupVersionKind
	NN  types.NamespacedName

	// Unique identifier for logging and error messages.
	// The ResourceID is used to provide a unique identifier for the resource, which is especially useful
	// when working with multiple resources of the same type.
	ResourceID string

	// IgnoreNotFound determines whether to ignore "not found" errors during operations.
	// Useful in scenarios where the resource may not exist and that's considered acceptable (e.g., optional cleanup).
	IgnoreNotFound bool

	// WaitForDeletion determines whether to wait for the resource to be fully deleted from the cluster.
	// If true, the DeleteResource function will block until the resource is confirmed to be gone.
	WaitForDeletion bool

	// WaitForRecreation determines whether to wait for the resource to be recreated after deletion.
	// This helps handle controllers that immediately recreate resources after deletion.
	WaitForRecreation bool

	// RemoveFinalizersOnDelete determines whether to automatically remove finalizers before deletion.
	// If true, DeleteResource will attempt to remove all finalizers if deletion is blocked.
	// This helps with resources that get stuck in deletion due to finalizers.
	RemoveFinalizersOnDelete bool

	// DeleteAllOfOptions defines the behavior of bulk resource deletion using DeleteAllOf.
	// This field holds the options that control how multiple resources should be deleted in bulk operations.
	DeleteAllOfOptions []client.DeleteAllOfOption

	// Webhook validation fields for testing
	// InvalidValue holds the description of the invalid value being tested in webhook validation
	InvalidValue string
	// FieldName holds the name of the field being validated by the webhook
	FieldName string
}

// ResourceOpts is a function type used to configure options for the ResourceOptions object.
// These options modify the behavior of resource handling operations, such as custom error messages, conditions, etc.
// The functions that modify ResourceOptions can be chained together to build a customized resource operation.
type ResourceOpts func(*ResourceOptions)

// ==============================
//        RESOURCE HELPERS
// ==============================

// WithObjectToCreate creates a ResourceOpts function that sets the ObjFn field of the ResourceOptions to
// convert the provided object into an unstructured resource. This is used when the resource doesn't exist
// yet and needs to be created or updated.
func WithObjectToCreate(obj client.Object) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.ObjFn = func(tc *TestContext) *unstructured.Unstructured {
			// Convert the input object to unstructured
			u, err := resources.ObjectToUnstructured(tc.Scheme(), obj)
			tc.g.Expect(err).NotTo(HaveOccurred())
			return u
		}
		ro.GVK = obj.GetObjectKind().GroupVersionKind()
		ro.NN = types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
		ro.ResourceID = resources.FormatNamespacedName(ro.NN)
	}
}

// WithFetchedObject creates a ResourceOpts function that sets the ObjFn field of the ResourceOptions to
// fetch an existing resource by its GroupVersionKind (GVK) and NamespacedName (NN). This is useful when
// the resource already exists and needs to be updated or patched.
func WithFetchedObject(gvk schema.GroupVersionKind, nn types.NamespacedName) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.ObjFn = func(tc *TestContext) *unstructured.Unstructured {
			u, err := tc.g.Get(gvk, nn).Get()
			tc.g.Expect(err).NotTo(HaveOccurred(), "Failed to fetch resource %s", nn.Name)
			return u
		}
		ro.NN = nn
		ro.GVK = gvk
		ro.ResourceID = resources.FormatNamespacedName(nn)
	}
}

// WithMinimalObject creates a ResourceOpts function that sets the ObjFn field of the ResourceOptions to
// create a minimal unstructured resource with the provided GroupVersionKind (GVK) and NamespacedName (NN).
// This is useful for scenarios where only a few essential fields of the resource need to be specified, such
// as when creating simple resources like namespaces.
func WithMinimalObject(gvk schema.GroupVersionKind, nn types.NamespacedName) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.ObjFn = func(tc *TestContext) *unstructured.Unstructured {
			// Create a new unstructured object and set the necessary fields
			u := resources.GvkToUnstructured(gvk) // Set the GroupVersionKind
			u.SetNamespace(nn.Namespace)          // Set the Namespace
			u.SetName(nn.Name)                    // Set the Name

			// Return the object with only the essential fields set
			return u
		}
		ro.NN = nn
		ro.GVK = gvk
		ro.ResourceID = resources.FormatNamespacedName(nn)
	}
}

// WithListOptions creates a ResourceOpts function that sets the ListOptions field of the ResourceOptions.
// ListOptions are used to specify filters like label selectors or other query parameters when listing resources.
func WithListOptions(listOptions *client.ListOptions) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.ListOptions = listOptions
	}
}

// WithIgnoreNotFound sets the IgnoreNotFound flag.
// By default, the flag is true to skip errors when the resource is not found,
// which is often useful in situations where the resource may not exist but its absence
// doesn't necessarily indicate a failure (e.g., when attempting to delete a resource).
// Set it to false if you want to enforce checking for the resource before performing operations.
func WithIgnoreNotFound(ignore bool) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.IgnoreNotFound = ignore
	}
}

// WithClientDeleteOptions creates a ResourceOpts function that sets the ClientDeleteOptions field
// of the ResourceOptions. This will be used to configure the deletion behavior (e.g., propagation policy).
func WithClientDeleteOptions(deleteOptions *client.DeleteOptions) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.ClientDeleteOptions = deleteOptions
	}
}

// WithDeletionPropagation sets the deletion propagation policy.
func WithDeletionPropagation(policy metav1.DeletionPropagation) ResourceOpts {
	return WithClientDeleteOptions(&client.DeleteOptions{
		PropagationPolicy: &policy,
	})
}

// WithForegroundDeletion ensures dependent resources are deleted before the parent resource.
// Use this when you need to guarantee that all child resources (like Pods from a Deployment)
// are fully cleaned up before the parent is removed, preventing orphaned resources.
func WithForegroundDeletion() ResourceOpts {
	return WithDeletionPropagation(metav1.DeletePropagationForeground)
}

// WithWaitForDeletion sets the WaitForDeletion flag.
// When enabled, DeleteResource will wait until the resource is fully removed from the cluster.
func WithWaitForDeletion(wait bool) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.WaitForDeletion = wait
	}
}

// WithWaitForRecreation sets the WaitForRecreation flag.
// When enabled, DeleteResource will wait for the resource to be recreated after deletion,
// handling controllers that immediately recreate managed resources.
func WithWaitForRecreation(wait bool) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.WaitForRecreation = wait
	}
}

// WithRemoveFinalizersOnDelete enables automatic finalizer removal before deletion.
// When enabled, DeleteResource will attempt to remove all finalizers if deletion is blocked.
// This helps with resources that get stuck in deletion due to finalizers.
func WithRemoveFinalizersOnDelete(remove bool) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.RemoveFinalizersOnDelete = remove
	}
}

// WithMutateFunc creates a ResourceOpts function that sets a function that modifies the resource before it is applied.
// This function can be used to mutate or update the resource in any desired way.
func WithMutateFunc(fn func(obj *unstructured.Unstructured) error) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.MutateFunc = fn
	}
}

// WithTransforms is a convenience wrapper for WithMutateFunc(testf.TransformPipeline(...))
// when you have multiple transform functions to apply.
func WithTransforms(transforms ...testf.TransformFn) ResourceOpts {
	return WithMutateFunc(testf.TransformPipeline(transforms...))
}

// WithCondition creates a ResourceOpts function that sets a custom Gomega matcher condition (e.g., Expect(Succeed())).
// This condition is used for verifying whether the resource operation has succeeded or failed, and can be used to
// customize the expected behavior of the resource handling function.
//
// Important: Calling WithCondition multiple times overrides any previously set condition.
// Only the last WithCondition call is used when verifying the resource.
//
// If you need to check multiple conditions, combine them explicitly using
// gomega.And(...) or gomega.Or(...), depending on the desired behavior.
func WithCondition(condition gTypes.GomegaMatcher) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.Condition = condition
	}
}

// WithAcceptableErr creates a ResourceOpts function that sets the AcceptableErr field in ResourceOptions.
// This allows the test to accept a specific error as successful completion rather than failure.
// When the specified error occurs, it's validated and the test continues successfully.
// Works exactly like MatchError() - accepts the same parameters.
// Examples:
//
//	WithAcceptableErr("error message")                   // String matching
//	WithAcceptableErr(someError)                         // Error instance matching
//	WithAcceptableErr(k8serr.IsInvalid, "IsInvalid")     // Function matching
//	WithAcceptableErr(ContainSubstring("not found"))     // Matcher
func WithAcceptableErr(expected any, functionErrorDescription ...any) func(*ResourceOptions) {
	return func(ro *ResourceOptions) {
		ro.AcceptableErrMatcher = MatchError(expected, functionErrorDescription...)
	}
}

// WithCustomErrorMsg creates a ResourceOpts function that sets a custom error message with the specified
// formatting pattern and arguments. This allows users to customize the error message displayed when an error
// occurs during resource operations, such as when applying, updating, or patching a resource.
func WithCustomErrorMsg(args ...interface{}) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.CustomErrorArgs = args
	}
}

// WithEventuallyTimeout sets the default timeout for Eventually assertions.
func WithEventuallyTimeout(value time.Duration) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.tc.g.SetDefaultEventuallyTimeout(value)
	}
}

// WithEventuallyPollingInterval sets the default polling interval for Eventually assertions.
func WithEventuallyPollingInterval(value time.Duration) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.tc.g.SetDefaultEventuallyPollingInterval(value)
	}
}

// WithConsistentlyDuration sets the default duration for Consistently assertions.
func WithConsistentlyDuration(value time.Duration) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.tc.g.SetDefaultConsistentlyDuration(value)
	}
}

// WithConsistentlyPollingInterval creates a ResourceOpts function that sets the polling interval for Consistently assertions.
func WithConsistentlyPollingInterval(value time.Duration) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.tc.g.SetDefaultConsistentlyPollingInterval(value)
	}
}

// WithInvalidValue creates a ResourceOpts function that sets the invalid value description for webhook validation tests.
// This is used for better error messages in webhook validation tests.
func WithInvalidValue(description string) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.InvalidValue = description
	}
}

// WithFieldName creates a ResourceOpts function that sets the field name being validated by the webhook.
// This is used for better error messages and validation in webhook tests.
func WithFieldName(fieldName string) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.FieldName = fieldName
	}
}

// WithDeleteAllOfOptions creates a ResourceOpts function that sets the DeleteAllOfOptions field
// of the ResourceOptions. This will be used to configure the bulk deletion behavior.
func WithDeleteAllOfOptions(deleteAllOfOptions ...client.DeleteAllOfOption) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.DeleteAllOfOptions = append(ro.DeleteAllOfOptions, deleteAllOfOptions...)
	}
}

// WithNamespaceFilter is a convenience helper for the most common bulk deletion pattern.
// It adds client.InNamespace to the DeleteAllOfOptions, enabling namespace-scoped bulk deletion.
// Note: Do NOT set Namespace in WithMinimalObject for bulk operations, it will be ignored.
func WithNamespaceFilter(namespace string) ResourceOpts {
	return func(ro *ResourceOptions) {
		ro.DeleteAllOfOptions = append(ro.DeleteAllOfOptions, client.InNamespace(namespace))
	}
}
