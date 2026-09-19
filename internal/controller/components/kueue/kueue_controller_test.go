//nolint:testpackage // white-box tests for unexported ClusterExtension watch predicates.
package kueue

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	. "github.com/onsi/gomega"
)

func TestClusterExtensionMatchesInstalledPackage(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	g.Expect(clusterExtensionMatchesInstalledPackage(
		newInstalledClusterExtension("kueue-ext", kueueOperator),
		kueueOperator,
	)).To(BeTrue())

	g.Expect(clusterExtensionMatchesInstalledPackage(
		newInstalledClusterExtension("kueue-ext", "other-operator"),
		kueueOperator,
	)).To(BeFalse())

	g.Expect(clusterExtensionMatchesInstalledPackage(
		newClusterExtensionWithSourceType("kueue-ext", kueueOperator, "Bundle"),
		kueueOperator,
	)).To(BeFalse())

	g.Expect(clusterExtensionMatchesInstalledPackage(
		newClusterExtensionPendingInstall("kueue-ext", kueueOperator),
		kueueOperator,
	)).To(BeFalse())

	g.Expect(clusterExtensionMatchesInstalledPackage(nil, kueueOperator)).To(BeFalse())
}

func TestClusterExtensionForPackagePredicate(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	p := clusterExtensionForPackage(kueueOperator)
	installed := newInstalledClusterExtension("kueue-ext", kueueOperator)
	pending := newClusterExtensionPendingInstall("kueue-ext", kueueOperator)

	g.Expect(p.Create(event.CreateEvent{Object: pending})).To(BeFalse())
	g.Expect(p.Create(event.CreateEvent{Object: installed})).To(BeTrue())

	g.Expect(p.Update(event.UpdateEvent{
		ObjectOld: pending,
		ObjectNew: installed,
	})).To(BeTrue())

	g.Expect(p.Update(event.UpdateEvent{
		ObjectOld: installed,
		ObjectNew: pending,
	})).To(BeTrue())

	g.Expect(p.Delete(event.DeleteEvent{Object: installed})).To(BeTrue())
}

func newClusterExtensionWithSourceType(name, packageName, sourceType string) client.Object {
	ext := newInstalledClusterExtension(name, packageName)
	_ = unstructured.SetNestedField(ext.Object, sourceType, "spec", "source", "sourceType")
	return ext
}

func newClusterExtensionPendingInstall(name, packageName string) client.Object {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "olm.operatorframework.io/v1",
			"kind":       "ClusterExtension",
			"metadata":   map[string]any{"name": name},
			"spec": map[string]any{
				"source": map[string]any{
					"sourceType": "Catalog",
					"catalog":    map[string]any{"packageName": packageName},
				},
			},
			"status": map[string]any{
				"conditions": []any{
					map[string]any{
						"type":   "Installed",
						"status": "False",
						"reason": "Progressing",
					},
				},
			},
		},
	}
}
