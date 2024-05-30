package conversion

import (
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	// "sigs.k8s.io/kustomize/api/resmap".
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/yaml"
)

const (
	resourceSeparator = "(?m)^---[ \t]*$"
)

// StrToUnstructured converts a string containing multiple resources in YAML format to a slice of Unstructured objects.
// The input string is split by "---" separator and each part is unmarshalled into an Unstructured object.
func StrToUnstructured(resources string) ([]*unstructured.Unstructured, error) {
	splitter := regexp.MustCompile(resourceSeparator)
	objectStrings := splitter.Split(resources, -1)
	objs := make([]*unstructured.Unstructured, 0, len(objectStrings))
	for _, str := range objectStrings {
		if strings.TrimSpace(str) == "" {
			continue
		}
		u := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(str), u); err != nil {
			return nil, err
		}

		objs = append(objs, u)
	}
	return objs, nil
}

// ResMapToUnstructured converts a ResMap to a slice of Unstructured objects.
// func ResMapToUnstructured(resMap resmap.ResMap) ([]*unstructured.Unstructured, error) {
// 	resources := make([]*unstructured.Unstructured, 0, resMap.Size())
// 	for _, res := range resMap.Resources() {
// 		u := &unstructured.Unstructured{}
// 		asYAML, errToYAML := res.AsYAML()
// 		if errToYAML != nil {
// 			return nil, errToYAML
// 		}
// 		if errUnmarshal := yaml.Unmarshal(asYAML, u); errUnmarshal != nil {
// 			return nil, errUnmarshal
// 		}
// 		resources = append(resources, u)
// 	}

// 	return resources, nil
// }

func Resource2Unstructured(res *resource.Resource) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}

	if err := yaml.Unmarshal([]byte(res.MustYaml()), u); err != nil {
		return nil, err
	}

	return u, nil
}
