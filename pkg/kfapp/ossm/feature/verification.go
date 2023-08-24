package feature

import (
	"context"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

func EnsureCRDIsInstalled(group string, version string, resource string) action {
	return func(f *Feature) error {
		crdGVR := schema.GroupVersionResource{
			Group:    group,
			Version:  version,
			Resource: resource,
		}

		_, err := f.dynamicClient.Resource(crdGVR).List(context.Background(), metav1.ListOptions{})

		return err
	}
}

func EnsureServiceMeshInstalled(feature *Feature) error {
	if err := EnsureCRDIsInstalled("maistra.io", "v2", "servicemeshcontrolplanes")(feature); err != nil {
		log.Info("Failed to find the pre-requisite SMCP CRD, please ensure OSSM operator is installed.")
		return err
	}

	smcp := feature.Spec.Mesh.Name
	smcpNs := feature.Spec.Mesh.Namespace

	status, err := checkSMCPStatus(feature.dynamicClient, smcp, smcpNs)
	if err != nil {
		log.Info("An error occurred while checking SMCP status - ensure the SMCP referenced exists.")
		return err
	}
	if status != "Ready" {
		log.Info("The referenced SMCP is not ready.", "name", smcp, "namespace", smcpNs)
		return errors.New("SMCP status is not ready")
	}
	return nil

}

func checkSMCPStatus(dynamicClient dynamic.Interface, name, namespace string) (string, error) {
	gvr := schema.GroupVersionResource{
		Group:    "maistra.io",
		Version:  "v1",
		Resource: "servicemeshcontrolplanes",
	}

	unstructObj, err := dynamicClient.Resource(gvr).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		log.Info("Failed to find SMCP")
		return "", err
	}

	conditions, found, err := unstructured.NestedSlice(unstructObj.Object, "status", "conditions")
	if err != nil || !found {
		log.Info("status conditions not found or error in parsing of SMCP")
		return "", err
	}
	lastCondition := conditions[len(conditions)-1].(map[string]interface{})
	status := lastCondition["type"].(string)

	return status, nil
}
