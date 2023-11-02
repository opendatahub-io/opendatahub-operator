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
	return func(feature *Feature) error {
		return wait.PollUntilContextTimeout(context.TODO(), interval, duration, false, func(ctx context.Context) (bool, error) {
			log.Info("waiting for pods to become ready", "feature", feature.Name, "namespace", namespace, "duration (s)", duration.Seconds())
			podList, err := feature.Clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return false, err
			}

			readyPods := 0
			totalPods := len(podList.Items)

			for _, pod := range podList.Items {
				podReady := true
				for _, condition := range pod.Status.Conditions {
					if condition.Type == corev1.PodReady {
						if condition.Status != corev1.ConditionTrue {
							podReady = false
							break
						}
					}
				}
				if podReady {
					readyPods++
				}
			}

			done := readyPods == totalPods

			if done {
				log.Info("done waiting for pods to become ready", "feature", feature.Name, "namespace", namespace)
			}

			return done, nil
		})
	}
}

func WaitForResourceToBeCreated(namespace string, gvr schema.GroupVersionResource) Action {
	return func(feature *Feature) error {
		return wait.PollUntilContextTimeout(context.TODO(), interval, duration, false, func(ctx context.Context) (bool, error) {
			log.Info("waiting for resource to be created", "namespace", namespace, "resource", gvr)

			resources, err := feature.DynamicClient.Resource(gvr).Namespace(namespace).List(context.TODO(), metav1.ListOptions{Limit: 1})
			if err != nil {
				log.Error(err, "failed waiting for resource", "namespace", namespace, "resource", gvr)
				return false, err
			}

			if len(resources.Items) > 0 {
				log.Info("resource created", "namespace", namespace, "resource", gvr)
				return true, nil
			}

			log.Info("still waiting for resource", "namespace", namespace, "resource", gvr)
			return false, nil
		})
	}
}
