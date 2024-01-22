package feature

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	interval = 2 * time.Second
	duration = 5 * time.Minute
)

func EnsureCRDIsInstalled(name string) Action {
	return func(f *Feature) error {
		return f.Client.Get(context.TODO(), client.ObjectKey{Name: name}, &apiextv1.CustomResourceDefinition{})
	}
}

func WaitForPodsToBeReady(namespace string) Action {
	return func(f *Feature) error {
		f.Log.Info("waiting for pods to become ready", "namespace", namespace, "duration (s)", duration.Seconds())

		return wait.PollUntilContextTimeout(context.TODO(), interval, duration, false, func(ctx context.Context) (bool, error) {
			podList, err := f.Clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
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

func WaitForResourceToBeCreated(namespace string, gvr schema.GroupVersionResource) Action {
	return func(f *Feature) error {
		f.Log.Info("waiting for resource to be created", "namespace", namespace, "resource", gvr)

		return wait.PollUntilContextTimeout(context.TODO(), interval, duration, false, func(ctx context.Context) (bool, error) {
			resources, err := f.DynamicClient.Resource(gvr).Namespace(namespace).List(context.TODO(), metav1.ListOptions{Limit: 1})
			if err != nil {
				f.Log.Error(err, "failed waiting for resource", "namespace", namespace, "resource", gvr)

				return false, err
			}

			if len(resources.Items) > 0 {
				f.Log.Info("resource created", "namespace", namespace, "resource", gvr)

				return true, nil
			}

			return false, nil
		})
	}
}
