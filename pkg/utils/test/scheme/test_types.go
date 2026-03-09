package scheme

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

var testSchemeGroupVersion = schema.GroupVersion{Group: "test.opendatahub.io", Version: "v1"}

func addTestTypesToScheme(s *runtime.Scheme) error {
	s.AddKnownTypes(testSchemeGroupVersion,
		&TestPlatformObject{},
		&TestPlatformObjectList{},
	)
	metav1.AddToGroupVersion(s, testSchemeGroupVersion)
	return nil
}

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
func (o *TestPlatformObject) DeepCopyObject() runtime.Object { //nolint:ireturn
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

func (l *TestPlatformObjectList) DeepCopyObject() runtime.Object { //nolint:ireturn
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
