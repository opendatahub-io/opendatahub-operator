//nolint:testpackage
package modelsasservice

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega/types"
	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
	operatorv1 "github.com/openshift/api/operator/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	pkgtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

const testApplicationsNamespace = "tenant-test-ns"

func testDSCI() *dsciv2.DSCInitialization {
	return &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: testApplicationsNamespace,
		},
	}
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}

	name := handler.GetName()
	g.Expect(name).Should(Equal(componentApi.ModelsAsServiceComponentName))
}

func TestNewCRObject_ReturnsNil(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}
	dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)

	cr, err := handler.NewCRObject(context.Background(), nil, dsc)
	g.Expect(err).To(Succeed())
	g.Expect(cr).Should(BeNil(), "maas-controller owns Tenant creation, ODH NewCRObject must return nil")
}

func TestIsEnabled(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}

	testCases := []struct {
		name            string
		kserveState     operatorv1.ManagementState
		maasState       operatorv1.ManagementState
		expectedEnabled func() types.GomegaMatcher
	}{
		{"should be enabled when both KServe and MaaS are managed", operatorv1.Managed, operatorv1.Managed, BeTrue},
		{"should be disabled when KServe not managed", operatorv1.Removed, operatorv1.Managed, BeFalse},
		{"should be disabled when KServe managed but MaaS is not enabled", operatorv1.Managed, operatorv1.Removed, BeFalse},
		{"should be disabled when KServe is unmanaged", operatorv1.Unmanaged, operatorv1.Managed, BeFalse},
		{"should be disabled when both KServe and MaaS are unmanaged", operatorv1.Unmanaged, operatorv1.Unmanaged, BeFalse},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dsc := createDSCWithKServeAndMaaS(tc.kserveState, tc.maasState)
			g.Expect(handler.IsEnabled(dsc)).Should(tc.expectedEnabled())
		})
	}
}

func TestUpdateDSCStatus(t *testing.T) {
	handler := &componentHandler{}

	t.Run("should return ConditionFalse when component CR has deletionTimestamp", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
		cr := createTenantCR(true)
		now := metav1.Now()
		cr.SetDeletionTimestamp(&now)
		cr.SetFinalizers([]string{"test-finalizer"})

		cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(), dsc, cr))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionFalse))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.DeletingReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, ReadyConditionType, status.DeletingMessage),
		)))
	})

	for _, tc := range []struct {
		name           string
		tenantReady    *bool
		expectedStatus metav1.ConditionStatus
		expectedReason string
		expectedMsg    string
	}{
		{
			name:           "ready Tenant CR",
			tenantReady:    ptr(true),
			expectedStatus: metav1.ConditionTrue,
			expectedReason: status.ReadyReason,
			expectedMsg:    "Component is ready",
		},
		{
			name:           "not-ready Tenant CR",
			tenantReady:    ptr(false),
			expectedStatus: metav1.ConditionFalse,
			expectedReason: status.NotReadyReason,
			expectedMsg:    "Component is not ready",
		},
		{
			name:           "missing Tenant CR",
			tenantReady:    nil,
			expectedStatus: metav1.ConditionFalse,
			expectedReason: status.NotReadyReason,
			expectedMsg:    "Tenant CR not available yet",
		},
	} {
		t.Run("should handle enabled component with "+tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
			var opts []fakeclient.ClientOpts
			if tc.tenantReady != nil {
				opts = append(opts, fakeclient.WithObjects(testDSCI(), dsc, createTenantCR(*tc.tenantReady)))
			} else {
				opts = append(opts, fakeclient.WithObjects(testDSCI(), dsc))
			}

			cli, err := fakeclient.New(opts...)
			g.Expect(err).ShouldNot(HaveOccurred())

			cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
				Client:     cli,
				Instance:   dsc,
				Conditions: conditions.NewManager(dsc, ReadyConditionType),
			})

			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(cs).Should(Equal(tc.expectedStatus))

			g.Expect(dsc).Should(WithTransform(json.Marshal, And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, tc.expectedReason),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, ReadyConditionType, tc.expectedMsg),
			)))
		})
	}

	for _, tc := range []struct {
		name           string
		kserveState    operatorv1.ManagementState
		maasState      operatorv1.ManagementState
		expectedReason string
	}{
		{"disabled via MaaS Removed", operatorv1.Managed, operatorv1.Removed, string(operatorv1.Removed)},
		{"disabled via KServe not managed", operatorv1.Removed, operatorv1.Managed, string(operatorv1.Managed)},
	} {
		t.Run("should show MaaS management state when "+tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			dsc := createDSCWithKServeAndMaaS(tc.kserveState, tc.maasState)

			cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(), dsc))
			g.Expect(err).ShouldNot(HaveOccurred())

			cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
				Client:     cli,
				Instance:   dsc,
				Conditions: conditions.NewManager(dsc, ReadyConditionType),
			})

			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

			g.Expect(dsc).Should(WithTransform(json.Marshal, And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, tc.expectedReason),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .severity == "%s"`, ReadyConditionType, common.ConditionSeverityInfo),
			)))
		})
	}

	t.Run("should handle Tenant CR with no status conditions", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
		cr := &maasv1alpha1.Tenant{}
		cr.SetName(maasv1alpha1.TenantInstanceName)
		cr.SetNamespace(MaaSSubscriptionNamespace)
		cr.APIVersion = maasv1alpha1.GroupVersion.String()
		cr.Kind = maasv1alpha1.TenantKind

		cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(), dsc, cr))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionFalse))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.NotReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Tenant CR exists but has no ready condition yet"`, ReadyConditionType),
		)))
	})

	t.Run("should report CRD not installed when Tenant CRD is missing", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)

		noMatchErr := &apimeta.NoResourceMatchError{PartialResource: schema.GroupVersionResource{
			Group: "maas.opendatahub.io", Version: "v1alpha1", Resource: "tenants",
		}}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(testDSCI(), dsc),
			fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*maasv1alpha1.Tenant); ok {
						return noMatchErr
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionFalse))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.NotReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Tenant CRD not installed"`, ReadyConditionType),
		)))
	})

	t.Run("should propagate unexpected API errors", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)

		cli, err := fakeclient.New(
			fakeclient.WithObjects(testDSCI(), dsc),
			fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*maasv1alpha1.Tenant); ok {
						return errors.New("simulated API server error")
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		_, err = handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("failed to get Tenant"))
	})
}

func createDSCWithKServeAndMaaS(kserveState, maasState operatorv1.ManagementState) *dscv2.DataScienceCluster {
	dsc := dscv2.DataScienceCluster{}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)
	dsc.SetName("test-dsc")

	dsc.Spec.Components.Kserve.ManagementState = kserveState
	dsc.Spec.Components.Kserve.ModelsAsService.ManagementState = maasState

	return &dsc
}

func createTenantCR(ready bool) *maasv1alpha1.Tenant {
	c := &maasv1alpha1.Tenant{}
	c.SetName(maasv1alpha1.TenantInstanceName)
	c.SetNamespace(MaaSSubscriptionNamespace)
	c.APIVersion = maasv1alpha1.GroupVersion.String()
	c.Kind = maasv1alpha1.TenantKind
	now := metav1.Now()
	if ready {
		c.Status.Conditions = []metav1.Condition{{
			Type:               status.ConditionTypeReady,
			Status:             metav1.ConditionTrue,
			Reason:             status.ReadyReason,
			Message:            "Component is ready",
			LastTransitionTime: now,
		}}
	} else {
		c.Status.Conditions = []metav1.Condition{{
			Type:               status.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             status.NotReadyReason,
			Message:            "Component is not ready",
			LastTransitionTime: now,
		}}
	}

	return c
}

func ptr[T any](v T) *T { return &v }

// TestMaasParametersConfigMapSetsPayloadProcessingNamespace verifies that the
// maas-parameters ConfigMap includes payload-processing-namespace set to the
// resolved application namespace, not the gateway namespace. This is critical
// because the EnvoyFilter cluster_name FQDN must reference the namespace where
// the payload-processing service actually runs (RHOAIENG-59726).
func TestMaasParametersConfigMapSetsPayloadProcessingNamespace(t *testing.T) {
	g := NewWithT(t)

	manifestsRoot := findManifestsRoot(t)

	cm, err := maasParametersConfigMapFromParamsEnv(manifestsRoot, testApplicationsNamespace, map[string]string{"app": "test"})
	g.Expect(err).ShouldNot(HaveOccurred())

	data, ok := cm.Object["data"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "ConfigMap must have a data section")

	g.Expect(data).To(HaveKeyWithValue("app-namespace", testApplicationsNamespace),
		"app-namespace must be the resolved application namespace")
	g.Expect(data).To(HaveKeyWithValue("payload-processing-namespace", testApplicationsNamespace),
		"payload-processing-namespace must match the application namespace, not the gateway namespace")

	g.Expect(data["payload-processing-namespace"]).ToNot(Equal(DefaultGatewayNamespace),
		"payload-processing-namespace must not be the gateway namespace")
}

// TestApplyImageOverridesFromParams verifies that the Option B image override
// pipeline (kustomize build → ImageTagTransformerPlugin) correctly replaces the
// default :latest image with a pinned image reference from params.env.
// This is the exact flow that runs in CI via AppendOperatorInstallManifests.
func TestApplyImageOverridesFromParams(t *testing.T) {
	g := NewWithT(t)

	manifestsRoot := findManifestsRoot(t)
	kPath := filepath.Join(manifestsRoot, "maas", "base", "maas-controller", "default")

	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fs := filesys.MakeFsOnDisk()
	resMap, err := k.Run(fs, kPath)
	g.Expect(err).ShouldNot(HaveOccurred(), "kustomize build should succeed")

	// Before override: the images transformer renders quay.io/opendatahub/maas-controller:latest
	depBefore := findDeploymentImage(g, resMap)
	g.Expect(depBefore).To(Equal("quay.io/opendatahub/maas-controller:latest"),
		"kustomize images transformer should produce the default :latest image")

	// Write a temporary params.env with a pinned digest to simulate what
	// ApplyParams does when RELATED_IMAGE_* env vars are set in CI.
	pinnedImage := "registry.redhat.io/rhoai/odh-maas-controller-rhel9@sha256:abc123def456"
	tmpDir := t.TempDir()
	paramsContent := "maas-controller-image=" + pinnedImage + "\n"
	err = os.WriteFile(filepath.Join(tmpDir, "params.env"), []byte(paramsContent), 0o600)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = applyImageOverridesFromParams(resMap, filepath.Join(tmpDir, "params.env"))
	g.Expect(err).ShouldNot(HaveOccurred(), "applyImageOverridesFromParams should succeed")

	// After override: the image must be the pinned digest, not :latest.
	depAfter := findDeploymentImage(g, resMap)
	g.Expect(depAfter).To(Equal(pinnedImage),
		"ImageTagTransformerPlugin must replace the :latest image with the pinned digest from params.env")
}

// TestApplyImageOverridesFromParams_TagFormat verifies tag-style overrides (repo:tag).
func TestApplyImageOverridesFromParams_TagFormat(t *testing.T) {
	g := NewWithT(t)

	manifestsRoot := findManifestsRoot(t)
	kPath := filepath.Join(manifestsRoot, "maas", "base", "maas-controller", "default")

	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	resMap, err := k.Run(filesys.MakeFsOnDisk(), kPath)
	g.Expect(err).ShouldNot(HaveOccurred())

	pinnedImage := "quay.io/myorg/maas-controller:v1.2.3"
	tmpDir := t.TempDir()
	err = os.WriteFile(filepath.Join(tmpDir, "params.env"),
		[]byte("maas-controller-image="+pinnedImage+"\n"), 0o600)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = applyImageOverridesFromParams(resMap, filepath.Join(tmpDir, "params.env"))
	g.Expect(err).ShouldNot(HaveOccurred())

	depAfter := findDeploymentImage(g, resMap)
	g.Expect(depAfter).To(Equal(pinnedImage))
}

// TestApplyImageOverridesFromParams_NoOverride verifies no-op when params.env
// has no maas-controller-image key.
func TestApplyImageOverridesFromParams_NoOverride(t *testing.T) {
	g := NewWithT(t)

	manifestsRoot := findManifestsRoot(t)
	kPath := filepath.Join(manifestsRoot, "maas", "base", "maas-controller", "default")

	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	resMap, err := k.Run(filesys.MakeFsOnDisk(), kPath)
	g.Expect(err).ShouldNot(HaveOccurred())

	tmpDir := t.TempDir()
	err = os.WriteFile(filepath.Join(tmpDir, "params.env"),
		[]byte("some-other-key=somevalue\n"), 0o600)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = applyImageOverridesFromParams(resMap, filepath.Join(tmpDir, "params.env"))
	g.Expect(err).ShouldNot(HaveOccurred())

	depAfter := findDeploymentImage(g, resMap)
	g.Expect(depAfter).To(Equal("quay.io/opendatahub/maas-controller:latest"),
		"image should remain unchanged when params.env has no controller image key")
}

// findManifestsRoot walks up from the test file to locate the opt/manifests directory.
func findManifestsRoot(t *testing.T) string {
	t.Helper()
	// Start from the module root (go test runs from the package directory)
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot get working directory: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "opt", "manifests")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("opt/manifests not found; skipping (manifests not downloaded)")
		}
		dir = parent
	}
}

// findDeploymentImage extracts the image of the "manager" container from the
// maas-controller Deployment in the rendered resmap.
func findDeploymentImage(g Gomega, rm resmap.ResMap) string {
	for _, r := range rm.Resources() {
		if r.GetKind() != "Deployment" || r.GetName() != "maas-controller" {
			continue
		}
		m, err := r.Map()
		g.Expect(err).ShouldNot(HaveOccurred())

		containers, found := nestedSlice(m, "spec", "template", "spec", "containers")
		g.Expect(found).To(BeTrue(), "containers field must exist")

		for _, c := range containers {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if cm["name"] == "manager" {
				img, ok := cm["image"].(string)
				g.Expect(ok).To(BeTrue(), "image must be a string")
				return img
			}
		}
	}
	g.Expect(false).To(BeTrue(), "Deployment maas-controller with container manager not found in resMap")
	return ""
}

func nestedSlice(obj map[string]any, fields ...string) ([]any, bool) {
	var current any = obj
	for _, f := range fields {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = m[f]
		if !ok {
			return nil, false
		}
	}
	s, ok := current.([]any)
	return s, ok
}
