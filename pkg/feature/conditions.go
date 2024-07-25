package feature

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

const (
	interval = 2 * time.Second
	duration = 5 * time.Minute
)

type MissingOperatorError struct {
	operatorName string
	err          error
}

func NewMissingOperatorError(operatorName string, err error) *MissingOperatorError {
	return &MissingOperatorError{
		operatorName: operatorName,
		err:          err,
	}
}

func (e *MissingOperatorError) Unwrap() error {
	return e.err
}

func (e *MissingOperatorError) Error() string {
	return fmt.Sprintf("missing operator %q", e.operatorName)
}

func EnsureOperatorIsInstalled(operatorName string) Action {
	return func(ctx context.Context, f *Feature) error {
		if found, err := cluster.SubscriptionExists(ctx, f.Client, operatorName); !found || err != nil {
			return fmt.Errorf(
				"failed to find the pre-requisite operator subscription %q, please ensure operator is installed. %w",
				operatorName,
				NewMissingOperatorError(operatorName, err),
			)
		}
		return nil
	}
}

func WaitForPodsToBeReady(namespace string) Action {
	return func(ctx context.Context, f *Feature) error {
		f.Log.Info("waiting for pods to become ready", "namespace", namespace, "duration (s)", duration.Seconds())

		return wait.PollUntilContextTimeout(ctx, interval, duration, false, func(ctx context.Context) (bool, error) {
			var podList corev1.PodList

			err := f.Client.List(ctx, &podList, client.InNamespace(namespace))
			if err != nil {
				return false, err
			}

			readyPods := 0
			totalPods := len(podList.Items)

			if totalPods == 0 { // We want to wait for "something", so make sure we have "something" before we claim success.
				return false, nil
			}

			for _, pod := range podList.Items {
				podReady := true
				// Consider a "PodSucceeded" as ready, since these will never will
				// be in Ready condition (i.e. Jobs that already completed).
				if pod.Status.Phase != corev1.PodSucceeded {
					for _, condition := range pod.Status.Conditions {
						if condition.Type == corev1.PodReady {
							if condition.Status != corev1.ConditionTrue {
								podReady = false

								break
							}
						}
					}
				}
				if podReady {
					readyPods++
				}
			}

			done := readyPods == totalPods

			if done {
				f.Log.Info("done waiting for pods to become ready", "namespace", namespace)
			}

			return done, nil
		})
	}
}

func WaitForResourceToBeCreated(namespace string, gvk schema.GroupVersionKind) Action {
	return func(ctx context.Context, f *Feature) error {
		f.Log.Info("waiting for resource to be created", "namespace", namespace, "resource", gvk)

		return wait.PollUntilContextTimeout(ctx, interval, duration, false, func(ctx context.Context) (bool, error) {
			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(gvk)

			err := f.Client.List(ctx, list, client.InNamespace(namespace), client.Limit(1))
			if err != nil {
				f.Log.Error(err, "failed waiting for resource", "namespace", namespace, "resource", gvk)

				return false, err
			}

			if len(list.Items) > 0 {
				f.Log.Info("resource created", "namespace", namespace, "resource", gvk)

				return true, nil
			}

			return false, nil
		})
	}
}
