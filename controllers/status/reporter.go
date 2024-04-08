//nolint:structcheck,ireturn // Reason: false positive, complains about unused fields - see Update method. ireturn to statisfy client.Object interface
package status

import (
	"context"
	"fmt"

	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Reporter defines how the condition should be updated.
// Its centerpiece is an calculateCondition closure which contains the logic of how the given condition should be calculated.
type Reporter[T client.Object] struct {
	object             T
	client             client.Client
	calculateCondition CalculateCondition[T]
}

// CalculateCondition is a closure function that allow to define how condition should be set.
// It can use Reporter.Err if available to set faulty condition.
// It should return a SaveStatusFunc which will be used to update the status of the object.
type CalculateCondition[T client.Object] func(err error) SaveStatusFunc[T]

// NewStatusReporter creates r new Reporter with all required fields.
func NewStatusReporter[T client.Object](cli client.Client, object T, calculate CalculateCondition[T]) *Reporter[T] {
	return &Reporter[T]{
		object:             object,
		client:             cli,
		calculateCondition: calculate,
	}
}

// ReportCondition updates the status of the object using the calculateCondition function.
func (r *Reporter[T]) ReportCondition(optionalErr error) (T, error) {
	return UpdateWithRetry[T](context.Background(), r.client, r.object, r.calculateCondition(optionalErr))
}

// SaveStatusFunc is a closure function that allow to define custom logic of updating status of a concrete resource object.
type SaveStatusFunc[T client.Object] func(saved T)

// UpdateWithRetry updates the status of object using passed function and retries on conflict.
func UpdateWithRetry[T client.Object](ctx context.Context, cli client.Client, original T, update SaveStatusFunc[T]) (T, error) {
	saved, ok := original.DeepCopyObject().(T)
	if !ok {
		return *new(T), fmt.Errorf("failed to deep copy object")
	}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := cli.Get(ctx, client.ObjectKeyFromObject(original), saved); err != nil {
			return err
		}

		update(saved)

		// Return rr itself here (not wrapped inside another error)
		// so that RetryOnConflict can identify it correctly.
		return cli.Status().Update(ctx, saved)
	})

	return saved, err
}
