package provider

import (
	"context"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DataProvider is a contract on how the data for the Feature container can be fetched.
// It is expected that either found instance is returned or error occurred during invocation.
type DataProvider[T any] interface {
	Get(ctx context.Context, c client.Client) (T, error)
}

// DataProviderFunc defines function signature which is used for fetching data.
// This allows to pass simple closures while construction data providers.
type DataProviderFunc[T any] func(ctx context.Context, c client.Client) (T, error)

// ValueOf is a constructor which allows to define a value with optional provider.
func ValueOf[T any](value T) DataProviderWithDefault[T] {
	return DataProviderWithDefault[T]{value: value}
}

// Defaulter defines how a default value can be supplied when original one is zero-value.
type Defaulter[T any] interface {
	Value() T
	OrElse(other T) DataProviderFunc[T]
	OrGet(getFunc DataProviderFunc[T]) DataProviderFunc[T]
}

// DataProviderWithDefault allows to define a value and optional means of supplying it if original value is empty.
// When the original value is zero the alternative can be provided using:
// - `OrElse` to define a static value
// - `OrGet` to perform dynamic lookup by providing DataProviderFunc.
type DataProviderWithDefault[T any] struct {
	value T //nolint:structcheck //reason used in e.g. Get
}

var _ DataProvider[any] = (*DataProviderWithDefault[any])(nil)
var _ Defaulter[any] = (*DataProviderWithDefault[any])(nil)

// Get returns Value() of Defaulter and ensures DataProviderWithDefault can be used as DataProviderFunc.
func (d DataProviderWithDefault[T]) Get(_ context.Context, _ client.Client) (T, error) {
	return d.value, nil
}

// Value returns actual value stored by the provider.
func (d DataProviderWithDefault[T]) Value() T {
	return d.value
}

// OrElse allows to define static default value when the stored one is a zero-value.
func (d DataProviderWithDefault[T]) OrElse(other T) DataProviderFunc[T] {
	if reflect.ValueOf(d.value).IsZero() {
		d.value = other
	}

	return d.Get
}

// OrGet allows to define dynamic value provider when the stored one is a zero-value.
func (d DataProviderWithDefault[T]) OrGet(getFunc DataProviderFunc[T]) DataProviderFunc[T] {
	if reflect.ValueOf(d.value).IsZero() {
		return getFunc
	}

	return d.Get
}
