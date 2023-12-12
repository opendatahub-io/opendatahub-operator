package monitoring

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
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
		if len(componentDeploymentList.Items) != 0 {
			for _, deployment := range componentDeploymentList.Items {
				if deployment.Status.ReadyReplicas == deployment.Status.Replicas {
					isReady = true
				} else {
					fmt.Printf("waiting for %d deployment to be ready for %s\n", len(componentDeploymentList.Items), componentName)
					isReady = false
				}
			}
		}
		return isReady, nil
	})
}

// use extras to set port and endpoint if needed
func WaitForServiceReady(ctx context.Context, restConfig *rest.Config, serviceName string, namespace string, interval int, timeout int, extras ...string) error {
	resourceInterval := time.Duration(interval) * time.Second
	resourceTimeout := time.Duration(timeout) * time.Minute
	var servicePort, serviceEndpoint string
	for i, param := range extras {
		switch i {
		case 0:
			servicePort = param
		case 1:
			serviceEndpoint = param
		}
	}
	serviceURL := fmt.Sprintf("https://%s.%s.svc:%s%s", serviceName, namespace, servicePort, serviceEndpoint)
	client := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: time.Second * 1,
	}
	return wait.PollUntilContextTimeout(context.TODO(), resourceInterval, resourceTimeout, true, func(ctx context.Context) (bool, error) {
		clientset, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			return false, fmt.Errorf("error getting client: %v", err)
		}
		_, err = clientset.CoreV1().Services(namespace).Get(context.TODO(), serviceName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
		}
		isReady := false
		response, err := client.Get(serviceURL)
		if err != nil {
			return false, fmt.Errorf("error to query service: %v", err)
		}
		defer response.Body.Close()
		// 200 or 403 as long as return
		if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusForbidden {
			fmt.Printf("service %s is ready to server\n", serviceName)
			isReady = true
		} else {
			fmt.Printf("waiting for service %s to be ready, got return code %d\n", serviceName, response.StatusCode)
			isReady = false
		}
		return isReady, nil
	})
}
