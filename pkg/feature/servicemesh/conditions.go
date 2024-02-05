package servicemesh

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/gvr"
)

const (
	interval = 2 * time.Second
	duration = 5 * time.Minute
)

func EnsureServiceMeshOperatorInstalled(f *feature.Feature) error {
	if err := feature.EnsureCRDIsInstalled("servicemeshcontrolplanes.maistra.io")(f); err != nil {
		f.Log.Info("Failed to find the pre-requisite Service Mesh Control Plane CRD, please ensure Service Mesh Operator is installed.")

		return err
	}

	return nil
}

func EnsureServiceMeshInstalled(f *feature.Feature) error {
	if err := EnsureServiceMeshOperatorInstalled(f); err != nil {
		return err
	}

	smcp := f.Spec.ControlPlane.Name
	smcpNs := f.Spec.ControlPlane.Namespace

	if err := WaitForControlPlaneToBeReady(f); err != nil {
		f.Log.Error(err, "failed waiting for control plane being ready", "control-plane", smcp, "namespace", smcpNs)

		return multierror.Append(err, errors.New("service mesh control plane is not ready")).ErrorOrNil()
	}

	return nil
}

func WaitForControlPlaneToBeReady(f *feature.Feature) error {
	smcp := f.Spec.ControlPlane.Name
	smcpNs := f.Spec.ControlPlane.Namespace

	f.Log.Info("waiting for control plane components to be ready", "control-plane", smcp, "namespace", smcpNs, "duration (s)", duration.Seconds())

	return wait.PollUntilContextTimeout(context.TODO(), interval, duration, false, func(ctx context.Context) (bool, error) {
		ready, err := CheckControlPlaneComponentReadiness(f.DynamicClient, smcp, smcpNs)

		if ready {
			f.Log.Info("done waiting for control plane components to be ready", "control-plane", smcp, "namespace", smcpNs)
		}

		return ready, err
	})
}

func CheckControlPlaneComponentReadiness(dynamicClient dynamic.Interface, smcp, smcpNs string) (bool, error) {
	unstructObj, err := dynamicClient.Resource(gvr.SMCP).Namespace(smcpNs).Get(context.TODO(), smcp, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to find Service Mesh Control Plane: %w", err)
	}

	components, found, err := unstructured.NestedMap(unstructObj.Object, "status", "readiness", "components")
	if err != nil || !found {
		return false, fmt.Errorf("status conditions not found or error in parsing of Service Mesh Control Plane: %w", err)
	}

	readyComponents := len(components["ready"].([]interface{}))     //nolint:forcetypeassert
	pendingComponents := len(components["pending"].([]interface{})) //nolint:forcetypeassert
	unreadyComponents := len(components["unready"].([]interface{})) //nolint:forcetypeassert

	return pendingComponents == 0 && unreadyComponents == 0 && readyComponents > 0, nil
}
