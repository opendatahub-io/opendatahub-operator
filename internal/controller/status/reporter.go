package status

import (
	"context"
	"errors"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Reporter handles condition reporting for a given object.
// The logic of how the given condition should be calculated is defined by the determineCondition function.
type Reporter[T client.Object] struct {
	object             T
	client             client.Client
	determineCondition DetermineCondition[T]
}

// DetermineCondition is a function that allow to define how condition should be set.
// It can use err if available to set faulty condition.
// It should return a SaveStatusFunc which will be used to update the status of the object.
type DetermineCondition[T client.Object] func(err error) SaveStatusFunc[T]

// NewStatusReporter creates r new Reporter with all required fields.
func NewStatusReporter[T client.Object](cli client.Client, object T, determine DetermineCondition[T]) *Reporter[T] {
	return &Reporter[T]{
		object:             object,
		client:             cli,
		determineCondition: determine,
	}
}

// ReportCondition updates the status of the object using the determineCondition function.
func (r *Reporter[T]) ReportCondition(ctx context.Context, optionalErr error) (T, error) {
	return UpdateWithRetry[T](ctx, r.client, r.object, r.determineCondition(optionalErr))
}

// SaveStatusFunc is a function that allow to define custom logic of updating status of a concrete resource object.
type SaveStatusFunc[T client.Object] func(saved T)

// UpdateWithRetry updates the status of object using passed function and retries on conflict.
func UpdateWithRetry[T client.Object](ctx context.Context, cli client.Client, original T, update SaveStatusFunc[T]) (T, error) {
	saved, ok := original.DeepCopyObject().(T)
	if !ok {
		return *new(T), errors.New("failed to deep copy object")
	}
	err := retry.OnError(retry.DefaultBackoff, retryOnNotFoundOrConflict, func() error {
		if err := cli.Get(ctx, client.ObjectKeyFromObject(original), saved); err != nil {
			return err
		}

		update(saved)

		// Return err itself here (not wrapped inside another error)
		// so that Retry can identify it correctly.
		return cli.Status().Update(ctx, saved)
	})

	return saved, err
}

func retryOnNotFoundOrConflict(err error) bool {
	// We are now sharing the client, read/write can occur on delay
	return k8serr.IsConflict(err) || k8serr.IsNotFound(err)
}
