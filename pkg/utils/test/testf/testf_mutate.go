package testf

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// Mutate adapts a typed object mutation to an unstructured transform. The
// object's GVK is used to construct a fresh typed object from the scheme.
func Mutate[T client.Object](scheme *runtime.Scheme, mutate func(T) error) TransformFn {
	return func(obj *unstructured.Unstructured) error {
		if obj == nil {
			return errors.New("cannot mutate a nil unstructured object")
		}
		if scheme == nil {
			return errors.New("cannot mutate an object with a nil scheme")
		}
		if mutate == nil {
			return errors.New("cannot mutate an object with a nil mutation function")
		}

		gvk := obj.GroupVersionKind()
		runtimeObj, err := scheme.New(gvk)
		if err != nil {
			return fmt.Errorf("create typed object for %s: %w", gvk, err)
		}

		typedObj, ok := runtimeObj.(T)
		if !ok {
			return fmt.Errorf("object for %s has type %T, not requested type %T", gvk, runtimeObj, *new(T))
		}
		if err := resources.ObjectFromUnstructured(scheme, obj, typedObj); err != nil {
			return fmt.Errorf("convert %s to typed object: %w", gvk, err)
		}
		if err := mutate(typedObj); err != nil {
			return fmt.Errorf("mutate typed object %s: %w", gvk, err)
		}

		updated, err := resources.ObjectToUnstructured(scheme, typedObj)
		if err != nil {
			return fmt.Errorf("convert mutated %s to unstructured: %w", gvk, err)
		}

		obj.Object = updated.Object
		return nil
	}
}
