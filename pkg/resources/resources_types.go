package resources

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

type UnstructuredList []unstructured.Unstructured

func (l UnstructuredList) Clone() []unstructured.Unstructured {
	if len(l) == 0 {
		return nil
	}

	result := make([]unstructured.Unstructured, len(l))

	for i := range l {
		result[i] = *l[i].DeepCopy()
	}

	return result
}
