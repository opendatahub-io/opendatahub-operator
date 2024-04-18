package action

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ResourceSpec struct {
	Gvk       schema.GroupVersionKind
	Namespace string
	// path to the field, like "metadata", "name"
	Path []string
	// set of values for the field to match object, any one matches
	Values []string
}

type MatcherFunc func(r ResourceSpec, obj *unstructured.Unstructured) (bool, error)
type Func func(ctx context.Context, c client.Client, r ResourceSpec, obj *unstructured.Unstructured) error
type RetryCheckFunc func(ctx context.Context, c client.Client, resources ...ResourceSpec) (bool, error)

type Action struct {
	client client.Client

	matcher MatcherFunc
	actions []Func
}

// shouldn't just return false on error?
func DefaultMatcher(r ResourceSpec, obj *unstructured.Unstructured) (bool, error) {
	if len(r.Path) == 0 || len(r.Values) == 0 {
		return true, nil
	}

	v, ok, err := unstructured.NestedString(obj.Object, r.Path...)
	if err != nil {
		return false, fmt.Errorf("failed to get field %v for %s %s/%s: %w", r.Path, r.Gvk.Kind, r.Namespace, obj.GetName(), err)
	}

	if !ok {
		return false, fmt.Errorf("unexisting path to handle: %v", r.Path)
	}

	for _, toDelete := range r.Values {
		if v == toDelete {
			return true, nil
		}
	}

	return false, nil
}

func New(c client.Client) *Action {
	return &Action{
		client:  c,
		matcher: DefaultMatcher,
	}
}

func Not(m MatcherFunc) MatcherFunc {
	return func(r ResourceSpec, obj *unstructured.Unstructured) (bool, error) {
		matched, err := m(r, obj)
		return !matched, err
	}
}

func Any(matchers ...MatcherFunc) MatcherFunc {
	return func(r ResourceSpec, obj *unstructured.Unstructured) (bool, error) {
		for _, m := range matchers {
			matched, err := m(r, obj)
			if err != nil {
				return false, err
			}
			if matched {
				return true, err
			}
		}
		return false, nil
	}
}

func All(matchers ...MatcherFunc) MatcherFunc {
	return func(r ResourceSpec, obj *unstructured.Unstructured) (bool, error) {
		for _, m := range matchers {
			matched, err := m(r, obj)
			if err != nil {
				return false, err
			}
			if !matched {
				return false, err
			}
		}
		return true, nil
	}
}

func (o *Action) ForMatched(m MatcherFunc) *Action {
	o.matcher = m
	return o
}

func (o *Action) Do(a Func) *Action {
	o.actions = append(o.actions, a)
	return o
}

func (o *Action) execOneResource(ctx context.Context, r ResourceSpec, objs []*unstructured.Unstructured) error {
	for _, item := range objs {
		for _, a := range o.actions {
			err := a(ctx, o.client, r, item)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func ListMatched(ctx context.Context, c client.Client, matcher MatcherFunc, resources ...ResourceSpec) (map[*ResourceSpec][]*unstructured.Unstructured, error) {
	ret := make(map[*ResourceSpec][]*unstructured.Unstructured)

	for _, r := range resources {
		r := r
		var items []*unstructured.Unstructured

		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(r.Gvk)

		err := c.List(ctx, list, client.InNamespace(r.Namespace))
		if err != nil {
			if errors.Is(err, &meta.NoKindMatchError{}) {
				fmt.Printf("Could not list %v: CRD not found\n", r.Gvk)
				continue
			}
			return ret, fmt.Errorf("failed to list %s: %w", r.Gvk.Kind, err)
		}

		for _, item := range list.Items {
			item := item

			matched, err := matcher(r, &item)
			if err != nil {
				return ret, err
			}

			if !matched {
				continue
			}

			items = append(items, &item)
		}

		if len(items) > 0 {
			ret[&r] = items
		}
	}

	return ret, nil
}

func (o *Action) Exec(ctx context.Context, resources ...ResourceSpec) error {
	var errors *multierror.Error

	matched, err := ListMatched(ctx, o.client, o.matcher, resources...)
	if err != nil {
		return err
	}

	for r, objs := range matched {
		err := o.execOneResource(ctx, *r, objs)
		errors = multierror.Append(errors, err)
	}

	return errors.ErrorOrNil()
}

func (o *Action) ExecWithRetry(ctx context.Context, shouldRetry RetryCheckFunc, resources ...ResourceSpec) error {
	return wait.ExponentialBackoffWithContext(ctx, wait.Backoff{
		// 5, 10, ,20, 40 then timeout
		Duration: 5 * time.Second,
		Factor:   2.0,
		Jitter:   0.1,
		Steps:    4,
		Cap:      1 * time.Minute,
	}, func(ctx context.Context) (bool, error) {
		err := o.Exec(ctx, resources...)
		if err != nil {
			return false, err
		}
		return shouldRetry(ctx, o.client, resources...)
	})
}

func (o *Action) DryRun(_ context.Context, _ ...ResourceSpec) error {
	return nil
}

func Delete(ctx context.Context, c client.Client, _ ResourceSpec, obj *unstructured.Unstructured) error {
	return client.IgnoreNotFound(c.Delete(ctx, obj))
}

func IfAnyLeft(matcher MatcherFunc) RetryCheckFunc {
	return func(ctx context.Context, c client.Client, resources ...ResourceSpec) (bool, error) {
		matched, err := ListMatched(ctx, c, matcher, resources...)
		if err != nil {
			return false, err
		}

		return len(matched) == 0, nil
	}
}

func deleteField(obj map[string]any, path ...string) error {
	if len(path) < 1 {
		return fmt.Errorf("path is empty")
	}

	parent := path[:len(path)-1]
	field := path[len(path)-1]

	v, ok, err := unstructured.NestedFieldNoCopy(obj, parent...)
	if err != nil || !ok {
		return fmt.Errorf("Not found or error")
	}

	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("field is not map")
	}
	delete(m, field)
	return nil
}

func DeleteField(path ...string) Func {
	return func(ctx context.Context, c client.Client, r ResourceSpec, obj *unstructured.Unstructured) error {
		err := deleteField(obj.Object, path...)
		if err != nil {
			return fmt.Errorf("could not delete field %v in object %s : %w", path, obj.GetName(), err)
		}

		err = c.Update(ctx, obj)
		if err != nil {
			return fmt.Errorf("error updating object while removing %v from %v : %w", path, obj.GetName(), err)
		}

		return nil
	}
}

func MatchMap(key, value string, keyMatch func(value, pattern string) bool, path ...string) MatcherFunc {
	return func(r ResourceSpec, obj *unstructured.Unstructured) (bool, error) {
		m, ok, err := unstructured.NestedStringMap(obj.Object, path...)
		if err != nil || !ok {
			return false, err
		}

		for k, v := range m {
			if !keyMatch(k, key) {
				continue
			}

			if value == "" || v == value {
				return true, nil
			}
		}

		return false, nil
	}
}

func MatchMapKeyContains(key string, path ...string) MatcherFunc {
	return MatchMap(key, "", strings.Contains, path...)
}

func NewDelete(c client.Client) *Action {
	return New(c).Do(Delete)
}

func NewDeleteMatched(c client.Client, m MatcherFunc) *Action {
	return New(c).Do(Delete).ForMatched(m)
}

func NewDeleteWithFinalizer(c client.Client) *Action {
	return New(c).
		Do(DeleteField("metadata", "finalizer")).
		Do(Delete)
}

func NewDeleteOwnersReferences(c client.Client) *Action {
	return New(c).
		Do(DeleteField("metadata", "ownerReferences"))
}

func NewDeleteLabel(c client.Client, label string) *Action {
	return New(c).
		Do(DeleteField("metadata", "labels", label))
}
