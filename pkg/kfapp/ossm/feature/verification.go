package feature

import (
	"context"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

// CreateNamespace will create namespace with the given name if it does not exist yet.
func CreateNamespace(namespace string) action {
	return func(f *Feature) error {
		nsClient := f.clientset.CoreV1().Namespaces()

		_, err := nsClient.Get(context.Background(), namespace, metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			_, err := nsClient.Create(context.Background(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}, metav1.CreateOptions{})

			// we either successfully created new namespace or failed during the process
			// returning err which indicates the state
			return err
		}

		return err
	}
}

func EnsureCRDIsInstalled(name string) action {
	return func(f *Feature) error {
		return f.client.Get(context.Background(), client.ObjectKey{Name: name}, &apiextv1.CustomResourceDefinition{})
	}
}

func EnsureServiceMeshInstalled(feature *Feature) error {
	if err := EnsureCRDIsInstalled("servicemeshcontrolplanes.maistra.io")(feature); err != nil {
		log.Info("Failed to find the pre-requisite Service Mesh Control Plane CRD, please ensure Service Mesh Operator is installed.", "feature", feature.Name)

		return err
	}

	smcp := feature.Spec.Mesh.Name
	smcpNs := feature.Spec.Mesh.Namespace

	if err := WaitForControlPlaneToBeReady(feature); err != nil {
		log.Error(err, "failed waiting for control plane being ready", "feature", feature.Name, "control-plane", smcp, "namespace", smcpNs)

		return multierror.Append(err, errors.New("service mesh control plane is not ready")).ErrorOrNil()
	}

	return nil
}

const (
	interval = 2 * time.Second
	duration = 5 * time.Minute
)

func WaitForPodsToBeReady(namespace string) action {
	return func(feature *Feature) error {
		return wait.Poll(interval, duration, func() (done bool, err error) {
			log.Info("waiting for control plane pods to become ready", "feature", feature.Name, "namespace", namespace, "duration (s)", duration.Seconds())
			podList, err := feature.clientset.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
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

			return readyPods == totalPods, nil
		})
	}
}

func WaitForControlPlaneToBeReady(feature *Feature) error {
	return wait.Poll(interval, duration, func() (done bool, err error) {
		smcp := feature.Spec.Mesh.Name
		smcpNs := feature.Spec.Mesh.Namespace

		log.Info("waiting for control plane components to be ready", "feature", feature.Name, "control-plane", smcp, "namespace", smcpNs, "duration (s)", duration.Seconds())

		return CheckControlPlaneComponentReadiness(feature.dynamicClient, smcp, smcpNs)
	})
}

func CheckControlPlaneComponentReadiness(dynamicClient dynamic.Interface, smcp, smcpNs string) (bool, error) {
	unstructObj, err := dynamicClient.Resource(smcpGVR).Namespace(smcpNs).Get(context.Background(), smcp, metav1.GetOptions{})
	if err != nil {
		log.Info("failed to find Service Mesh Control Plane", "control-plane", smcp, "namespace", smcpNs)

		return false, err
	}

	components, found, err := unstructured.NestedMap(unstructObj.Object, "status", "readiness", "components")
	if err != nil || !found {
		log.Info("status conditions not found or error in parsing of Service Mesh Control Plane")

		return false, err
	}

	readyComponents := len(components["ready"].([]interface{}))
	pendingComponents := len(components["pending"].([]interface{}))
	unreadyComponents := len(components["unready"].([]interface{}))

	return pendingComponents == 0 && unreadyComponents == 0 && readyComponents > 0, nil
}
