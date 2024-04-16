package monitoring

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// WaitForDeploymentAvailable to check if component deployment from 'namespace' is ready within 'timeout' before apply prometheus rules for the component.
func WaitForDeploymentAvailable(ctx context.Context, c client.Client, componentName string, namespace string, interval int, timeout int) error {
	resourceInterval := time.Duration(interval) * time.Second
	resourceTimeout := time.Duration(timeout) * time.Minute

	return wait.PollUntilContextTimeout(ctx, resourceInterval, resourceTimeout, true, func(ctx context.Context) (bool, error) {
		componentDeploymentList := &v1.DeploymentList{}
		err := c.List(ctx, componentDeploymentList, client.InNamespace(namespace), client.HasLabels{labels.ODH.Component(componentName)})
		if err != nil {
			return false, fmt.Errorf("error fetching list of deployments: %w", err)
		}

		fmt.Printf("waiting for %d deployment to be ready for %s\n", len(componentDeploymentList.Items), componentName)
		for _, deployment := range componentDeploymentList.Items {
			if deployment.Status.ReadyReplicas != deployment.Status.Replicas {
				return false, nil
			}
		}

		return true, nil
	})
}
