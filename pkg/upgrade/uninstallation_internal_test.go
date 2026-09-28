package upgrade

import (
	"context"
	"testing"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

const testOperatorNamespace = "opendatahub"

func TestRemoveClusterExtension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		platform         common.Platform
		installNamespace string
		objects          []client.Object
		wantRemaining    []string
		wantErrSubstr    string
		noMatchListErr   bool
		notFoundListErr  bool
	}{
		{
			name:             "deletes matching Catalog ClusterExtension for ODH",
			platform:         cluster.OpenDataHub,
			installNamespace: testOperatorNamespace,
			objects: []client.Object{
				newClusterExtensionForUninstall("odh-ext", odhOperatorPackage, testOperatorNamespace),
				newClusterExtensionForUninstall("other-ext", "other-operator", testOperatorNamespace),
			},
			wantRemaining: []string{"other-ext"},
		},
		{
			name:             "deletes matching Catalog ClusterExtension for SelfManagedRhoai",
			platform:         cluster.SelfManagedRhoai,
			installNamespace: testOperatorNamespace,
			objects: []client.Object{
				newClusterExtensionForUninstall("rhoai-ext", rhoaiOperatorPackage, testOperatorNamespace),
			},
			wantRemaining: nil,
		},
		{
			name:             "ignores Bundle sourceType",
			platform:         cluster.OpenDataHub,
			installNamespace: testOperatorNamespace,
			objects: []client.Object{
				newClusterExtensionWithSourceType("bundle-ext", odhOperatorPackage, "Bundle", testOperatorNamespace),
			},
			wantRemaining: []string{"bundle-ext"},
		},
		{
			name:             "ManagedRhoai skips deletion",
			platform:         cluster.ManagedRhoai,
			installNamespace: testOperatorNamespace,
			objects: []client.Object{
				newClusterExtensionForUninstall("odh-ext", odhOperatorPackage, testOperatorNamespace),
			},
			wantRemaining: []string{"odh-ext"},
		},
		{
			name:             "empty platform skips deletion",
			platform:         "",
			installNamespace: testOperatorNamespace,
			objects: []client.Object{
				newClusterExtensionForUninstall("odh-ext", odhOperatorPackage, testOperatorNamespace),
				newClusterExtensionForUninstall("rhoai-ext", rhoaiOperatorPackage, testOperatorNamespace),
			},
			wantRemaining: []string{"odh-ext", "rhoai-ext"},
		},
		{
			name:             "same package in another namespace is left alone",
			platform:         cluster.OpenDataHub,
			installNamespace: testOperatorNamespace,
			objects: []client.Object{
				newClusterExtensionForUninstall("ours", odhOperatorPackage, testOperatorNamespace),
				newClusterExtensionForUninstall("theirs", odhOperatorPackage, "other-ns"),
			},
			wantRemaining: []string{"theirs"},
		},
		{
			name:             "NoMatchError is success",
			platform:         cluster.OpenDataHub,
			installNamespace: testOperatorNamespace,
			noMatchListErr:   true,
			wantRemaining:    nil,
		},
		{
			name:             "NotFound is success",
			platform:         cluster.OpenDataHub,
			installNamespace: testOperatorNamespace,
			notFoundListErr:  true,
			wantRemaining:    nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			opts := []fakeclient.ClientOpts{
				fakeclient.WithObjects(tc.objects...),
				fakeclient.WithGVKs(fakeclient.GVKMapping{GVK: gvk.ClusterExtension, Scope: meta.RESTScopeRoot}),
			}
			if tc.noMatchListErr {
				opts = append(opts, interceptorListNoMatch(gvk.ClusterExtension))
			}
			if tc.notFoundListErr {
				opts = append(opts, interceptorListNotFound(gvk.ClusterExtension))
			}

			cli, err := fakeclient.New(opts...)
			g.Expect(err).ShouldNot(HaveOccurred())

			err = removeClusterExtension(t.Context(), cli, tc.platform, tc.installNamespace)
			if tc.wantErrSubstr != "" {
				g.Expect(err).Should(HaveOccurred())
				g.Expect(err.Error()).Should(ContainSubstring(tc.wantErrSubstr))
				return
			}
			g.Expect(err).ShouldNot(HaveOccurred())

			if tc.noMatchListErr || tc.notFoundListErr {
				return
			}

			remaining := listClusterExtensionNames(t, cli)
			if tc.wantRemaining == nil {
				g.Expect(remaining).Should(BeEmpty())
			} else {
				g.Expect(remaining).Should(ConsistOf(tc.wantRemaining))
			}
		})
	}
}

func TestClusterExtensionMatchesPackage(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	g.Expect(clusterExtensionMatchesPackage(
		newClusterExtensionForUninstall("ext", odhOperatorPackage, testOperatorNamespace),
		odhOperatorPackage,
		testOperatorNamespace,
	)).To(BeTrue())

	g.Expect(clusterExtensionMatchesPackage(
		newClusterExtensionForUninstall("ext", "other", testOperatorNamespace),
		odhOperatorPackage,
		testOperatorNamespace,
	)).To(BeFalse())

	g.Expect(clusterExtensionMatchesPackage(
		newClusterExtensionWithSourceType("ext", odhOperatorPackage, "Bundle", testOperatorNamespace),
		odhOperatorPackage,
		testOperatorNamespace,
	)).To(BeFalse())

	g.Expect(clusterExtensionMatchesPackage(
		newClusterExtensionForUninstall("ext", odhOperatorPackage, "other-ns"),
		odhOperatorPackage,
		testOperatorNamespace,
	)).To(BeFalse())
}

func newClusterExtensionForUninstall(name, packageName, installNamespace string) *unstructured.Unstructured {
	return newClusterExtensionWithSourceType(name, packageName, "Catalog", installNamespace)
}

func newClusterExtensionWithSourceType(name, packageName, sourceType, installNamespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "olm.operatorframework.io/v1",
			"kind":       "ClusterExtension",
			"metadata":   map[string]any{"name": name},
			"spec": map[string]any{
				"namespace": installNamespace,
				"source": map[string]any{
					"sourceType": sourceType,
					"catalog":    map[string]any{"packageName": packageName},
				},
			},
		},
	}
}

func listClusterExtensionNames(t *testing.T, cli client.Client) []string {
	t.Helper()
	g := NewWithT(t)

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk.ClusterExtension)
	g.Expect(cli.List(t.Context(), list)).Should(Succeed())

	names := make([]string, 0, len(list.Items))
	for i := range list.Items {
		names = append(names, list.Items[i].GetName())
	}
	return names
}

func interceptorListNoMatch(missingGVK schema.GroupVersionKind) fakeclient.ClientOpts {
	return fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			return &meta.NoKindMatchError{
				GroupKind:        missingGVK.GroupKind(),
				SearchedVersions: []string{missingGVK.Version},
			}
		},
	})
}

func interceptorListNotFound(missingGVK schema.GroupVersionKind) fakeclient.ClientOpts {
	return fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			return k8serr.NewNotFound(schema.GroupResource{
				Group:    missingGVK.Group,
				Resource: "clusterextensions",
			}, "")
		},
	})
}
