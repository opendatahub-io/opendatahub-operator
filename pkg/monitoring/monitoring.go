package monitoring

import (
	"context"
	"fmt"
	"time"

	errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// WaitForDeploymentAvailable to check if component deployment from 'namepsace' is ready within 'timeout' before apply prometheus rules for the component.
func WaitForDeploymentAvailable(_ context.Context, restConfig *rest.Config, componentName string, namespace string, interval int, timeout int) error {
	resourceInterval := time.Duration(interval) * time.Second
	resourceTimeout := time.Duration(timeout) * time.Minute
	return wait.PollUntilContextTimeout(context.TODO(), resourceInterval, resourceTimeout, true, func(ctx context.Context) (bool, error) {
		clientset, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			return false, fmt.Errorf("error getting client %w", err)
		}
		componentDeploymentList, err := clientset.AppsV1().Deployments(namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app.opendatahub.io/" + componentName,
		})
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
		}
		isReady := false
		fmt.Printf("waiting for %d deployment to be ready for %s\n", len(componentDeploymentList.Items), componentName)
		if len(componentDeploymentList.Items) != 0 {
			for _, deployment := range componentDeploymentList.Items {
				if deployment.Status.ReadyReplicas == deployment.Status.Replicas {
					isReady = true
				} else {
					isReady = false
				}
			}
		}
		return isReady, nil
	})
}
