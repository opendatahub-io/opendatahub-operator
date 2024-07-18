package deploy

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func isSharedResource(componentCounter []string, componentName string) bool {
	return len(componentCounter) > 1 || (len(componentCounter) == 1 && componentCounter[0] != componentName)
}

func isOwnedByODHCRD(ownerReferences []metav1.OwnerReference) bool {
	for _, owner := range ownerReferences {
		if owner.Kind == "DataScienceCluster" || owner.Kind == "DSCInitialization" {
			return true
		}
	}
	return false
}

func getComponentCounter(foundLabels map[string]string) []string {
	var componentCounter []string
	for label := range foundLabels {
		if strings.Contains(label, labels.ODHAppPrefix) {
			compFound := strings.Split(label, "/")[1]
			componentCounter = append(componentCounter, compFound)
		}
	}
	return componentCounter
}
