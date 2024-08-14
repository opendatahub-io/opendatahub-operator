package feature

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/provider"
)

// Value allows to define association between a name under which the data is stored in the Feature and underlying
// struct needed by the Feature to e.g. process templates.
func Value(key string, value any) *PlainValue {
	return &PlainValue{key: key, value: value}
}

type PlainValue struct {
	key   string
	value any
}

var _ Entry = PlainValue{}

func (e PlainValue) AddTo(f *Feature) error {
	return f.Set(e.key, e.value)
}

type ValueProvider[T any] struct {
	key   string
	value provider.DataProviderFunc[T]
}

var _ Entry = ValueProvider[any]{}

func (e ValueProvider[T]) AddTo(f *Feature) error {
	value, err := e.value()
	if err != nil {
		return err
	}

	return f.Set(e.key, value)
}

// Provider allows to define association between a name under which the data is stored in the Feature and a data provider
// defining the logic for fetching. Provider is a function allowing to fetch a value for a given key dynamically by
// interacting with Kubernetes client.
func Provider[T any](key string, providerFunc provider.DataProviderFunc[T]) *ValueProvider[T] {
	return &ValueProvider[T]{key: key, value: providerFunc}
}

// ExtractEntry is a convenient way to define how to extract a value from the given Feature's data using defined key.
func ExtractEntry[T any](key string) func(f *Feature) (T, error) {
	return func(f *Feature) (T, error) {
		return Get[T](f, key)
	}
}

// DataDefinition defines how the data is created and fetched from the Feature's data context.
// S is a source type from which the data is created.
// T is a type of the data stored in the Feature.
type DataDefinition[S, T any] struct {
	// Create is a factory function to create a Feature's DataEntry from the given source.
	Create func(ctx context.Context, cli client.Client, source S) (T, error)
	// Extract allows to extract data from the Feature.
	Extract func(f *Feature) (T, error)
}

// Get allows to retrieve arbitrary value from the Feature's data container.
func Get[T any](f *Feature, key string) (T, error) {
	var data T
	var ok bool

	input, found := f.data[key]
	if !found {
		return data, fmt.Errorf("key %s not found in feature %s", key, f.Name)
	}

	data, ok = input.(T)
	if !ok {
		return data, fmt.Errorf("invalid type %T for key %s in feature %s", f.data[key], key, f.Name)
	}

	return data, nil
}

// Set allows to store a value under given key in the Feature's data container.
func (f *Feature) Set(key string, data any) error {
	if f.data == nil {
		f.data = map[string]any{}
	}

	f.data[key] = data

	return nil
}
