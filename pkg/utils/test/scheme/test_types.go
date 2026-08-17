package scheme

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

const TestPlatformObjectKind = "TestPlatformObject"

var TestSchemeGroupVersion = schema.GroupVersion{Group: "test.opendatahub.io", Version: "v1"}

var TestPlatformObjectGVK = TestSchemeGroupVersion.WithKind(TestPlatformObjectKind)

// addTestTypesToScheme registers TestPlatformObject in the scheme with conversion functions
// for unstructured ↔ typed round-tripping. This is required by envtest clients that call
// Scheme().Convert() after server-side apply (see pkg/resources/resources.go Apply/ApplyStatus).
func addTestTypesToScheme(s *runtime.Scheme) error {
	s.AddKnownTypes(TestSchemeGroupVersion,
		&TestPlatformObject{},
		&TestPlatformObjectList{},
	)
	metav1.AddToGroupVersion(s, TestSchemeGroupVersion)

	// Register unstructured → typed conversion (used by Apply/ApplyStatus to write
	// the server response back into the typed object).
	if err := s.AddConversionFunc(
		&unstructured.Unstructured{},
		&TestPlatformObject{},
		func(a, b interface{}, _ conversion.Scope) error {
			src, ok := a.(*unstructured.Unstructured)
			if !ok {
				return fmt.Errorf("expected *unstructured.Unstructured, got %T", a)
			}
			dst, ok := b.(*TestPlatformObject)
			if !ok {
				return fmt.Errorf("expected *TestPlatformObject, got %T", b)
			}
			return runtime.DefaultUnstructuredConverter.FromUnstructured(src.Object, dst)
		},
	); err != nil {
		return err
	}

	// Register typed → typed identity conversion.
	return s.AddConversionFunc(
		&TestPlatformObject{},
		&TestPlatformObject{},
		func(a, b interface{}, _ conversion.Scope) error {
			in, ok := a.(*TestPlatformObject)
			if !ok {
				return fmt.Errorf("expected *TestPlatformObject, got %T", a)
			}
			out, ok := b.(*TestPlatformObject)
			if !ok {
				return fmt.Errorf("expected *TestPlatformObject, got %T", b)
			}
			*out = *in
			return nil
		},
	)
}

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = &TestPlatformObject{}

// TestPlatformObject is a minimal common.PlatformObject implementation for use in tests
// where no specific platform CR type is relevant to the behavior under test.
type TestPlatformObject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Status common.Status `json:"status"`
}

// GetStatus implements common.WithStatus.
func (o *TestPlatformObject) GetStatus() *common.Status { return &o.Status }

// GetConditions implements common.ConditionsAccessor.
func (o *TestPlatformObject) GetConditions() []common.Condition { return o.Status.GetConditions() }

// SetConditions implements common.ConditionsAccessor.
func (o *TestPlatformObject) SetConditions(c []common.Condition) { o.Status.SetConditions(c) }

func (o *TestPlatformObject) DeepCopyObject() runtime.Object { //nolint:ireturn,nolintlint // runtime.Object return is required by interface
	if o == nil {
		return nil
	}
	out := &TestPlatformObject{
		TypeMeta:   o.TypeMeta,
		ObjectMeta: *o.DeepCopy(),
	}
	o.Status.DeepCopyInto(&out.Status)
	return out
}

// TestPlatformObjectList is the list type for TestPlatformObject, required for scheme registration.
type TestPlatformObjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []TestPlatformObject `json:"items"`
}

func (l *TestPlatformObjectList) DeepCopyObject() runtime.Object { //nolint:ireturn,nolintlint // runtime.Object return is required by interface
	if l == nil {
		return nil
	}
	out := &TestPlatformObjectList{
		TypeMeta: l.TypeMeta,
		ListMeta: *l.DeepCopy(),
		Items:    make([]TestPlatformObject, len(l.Items)),
	}
	for i := range l.Items {
		src := &l.Items[i]
		out.Items[i] = TestPlatformObject{
			TypeMeta:   src.TypeMeta,
			ObjectMeta: *src.DeepCopy(),
		}
		src.Status.DeepCopyInto(&out.Items[i].Status)
	}
	return out
}
