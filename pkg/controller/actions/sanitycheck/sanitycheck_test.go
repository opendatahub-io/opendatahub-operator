package sanitycheck_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/sanitycheck"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func TestPerformV3UpgradeSanityChecks(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "components.platform.opendatahub.io", Version: "v1alpha1", Kind: "TestFake"}
	gvkList := schema.GroupVersionKind{Group: "components.platform.opendatahub.io", Version: "v1alpha1", Kind: "TestFakeList"}
	// Create a custom scheme and register the TestFake GVK
	fakeSchema, err := scheme.New()
	require.NoError(t, err)
	fakeSchema.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	fakeSchema.AddKnownTypeWithName(gvkList, &unstructured.UnstructuredList{})

	testCases := []struct {
		name          string
		setupClient   func(g *WithT, mockCRD *apiextv1.CustomResourceDefinition) client.Client
		expectError   bool
		errorContains string
	}{
		{
			name: "returns no error if CRD not exists",
			setupClient: func(g *WithT, mockCRD *apiextv1.CustomResourceDefinition) client.Client {
				cli, err := fakeclient.New()
				g.Expect(err).ShouldNot(HaveOccurred())
				return cli
			},
			expectError: false,
		},
		{
			name: "returns no error if CRD exists but no resources are present",
			setupClient: func(g *WithT, mockCRD *apiextv1.CustomResourceDefinition) client.Client {
				cli, err := fakeclient.New(
					fakeclient.WithObjects(mockCRD),
					fakeclient.WithScheme(fakeSchema),
				)
				g.Expect(err).ShouldNot(HaveOccurred())
				return cli
			},
			expectError: false,
		},
		{
			name: "returns error if CRD exists and resources are present",
			setupClient: func(g *WithT, mockCRD *apiextv1.CustomResourceDefinition) client.Client {
				resource := &unstructured.Unstructured{}
				resource.SetGroupVersionKind(gvk)
				resource.SetName("test-fake")

				cli, err := fakeclient.New(
					fakeclient.WithObjects(mockCRD, resource),
					fakeclient.WithScheme(fakeSchema),
				)
				g.Expect(err).ShouldNot(HaveOccurred())
				return cli
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			errorMessage := "TestFake resources present"

			mockCRD := mocks.NewMockCRD("components.platform.opendatahub.io", "v1alpha1", "TestFake", "fakeName")
			mockCRD.Status.StoredVersions = append(mockCRD.Status.StoredVersions, "v1alpha1")

			cli := tc.setupClient(g, mockCRD)

			rr := &types.ReconciliationRequest{
				Client: cli,
			}

			action := sanitycheck.NewAction(sanitycheck.WithUnwantedResource(gvk, errorMessage))
			err = action(ctx, rr)

			if tc.expectError {
				g.Expect(err).Should(HaveOccurred())
				g.Expect(err.Error()).Should(ContainSubstring(errorMessage))
			} else {
				g.Expect(err).ShouldNot(HaveOccurred())
			}
		})
	}
}
