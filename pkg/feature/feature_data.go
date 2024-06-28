package feature

import (
	"context"
	"fmt"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/provider"
)

// Entry allows to define association between a name under which the data is stored in the Feature and a data provider
// defining the logic for fetching. Provider is a function allowing to fetch a value for a given key dynamically by interacting with Kubernetes client.
// If the value is static, use provider.ValueOf(variable).Get as a function.
func Entry[T any](key string, providerFunc provider.DataProviderFunc[T]) Action {
	return addToContextFromProvider(key, providerFunc)
}

// ContextEntry associates data provider with a key under which the data is stored in the Feature.
type ContextEntry[T any] struct {
	Key   string
	Value provider.DataProviderFunc[T]
}

// ContextDefinition defines how the data is created and fetched from the Feature's context.
type ContextDefinition[S, T any] struct {
	// Create is a factory function to create a Feature's ContextEntry from the given source.
	Create func(source *S) ContextEntry[T]
	// From allows to fetch data from the Feature.
	From func(f *Feature) (T, error)
}

// ExtractEntry is a template for defining a function to extract a value from the Feature's context using defined key.
func ExtractEntry[T any](key string) func(f *Feature) (T, error) {
	return func(f *Feature) (T, error) {
		return Get[T](f, key)
	}
}

// AsAction converts ContextEntry to an Action which is the used to populate defined key-value in the feature itself.
func (c ContextEntry[T]) AsAction() Action {
	return Entry[T](c.Key, c.Value)
}

func addToContextFromProvider[T any](key string, provider provider.DataProviderFunc[T]) Action {
	return func(ctx context.Context, f *Feature) error {
		data, err := provider(ctx, f.Client)
		if err != nil {
			return err
		}

		return f.Set(key, data)
	}
}

// Get allows to retrieve arbitrary value from the Feature's context.
func Get[T any](f *Feature, key string) (T, error) {
	var data T
	var ok bool

	input, found := f.data[key]
	if !found {
		return data, fmt.Errorf("key %s not found", key)
	}

	data, ok = input.(T)
	if !ok {
		return data, fmt.Errorf("invalid type %T", f.data[key])
	}

	return data, nil
}

// Set allows to store a value under given key in the Feature's context.
func (f *Feature) Set(key string, data any) error {
	if f.data == nil {
		f.data = map[string]any{}
	}

	f.data[key] = data

	return nil
}
