//nolint:testpackage // Reuses handler fixtures shared with handler_test.go.
package workbenches

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"

	. "github.com/onsi/gomega"
)

// crdPath is the module CRD's location within its local downloaded chart
// assets (see make get-manifests).
const crdPath = "crd/workbenches.crd.yaml"

// workbenchesSuite drives the Workbenches projection cases through the shared
// runner; each case is one method registered as a t.Run, building its DSC
// context explicitly from newDSC and amending only the fields under test.
// BuildModuleCR always emits managementState and mlflowEnabled explicitly and
// derives gatewayDomain, ingresses and platform from the module CR config.
// Both RHOAI release identities map to the module enum SelfManagedRhoai;
// unknown releases fall back to OpenDataHub. The deprecated notebooksV1
// stanza is unknown to the module schema, so any leak is rejected by strict
// admission.
type workbenchesSuite struct {
	*projection.Runner
}

func (s workbenchesSuite) minimalManagedSelectionWithNilConfig(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.mlflowEnabled == false`),
		jq.Match(`.spec.workbenchNamespace == "my-workbenches"`),
		jq.Match(`.spec.workbenchesV2.managementState == %q`, operatorv1.Removed),
		// Orchestrator-only fields stay unprojected without a config.
		jq.Match(`.spec.gatewayDomain == null`),
		jq.Match(`.spec.platform == null`),
	)))
}

func (s workbenchesSuite) populatedConfigWithGateway(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: newModuleCRConfig(),
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.mlflowEnabled == false`),
		jq.Match(`.spec.workbenchNamespace == "my-workbenches"`),
		jq.Match(`.spec.workbenchesV2.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.gatewayDomain == "apps.example.com"`),
		jq.Match(`.spec.platform == "OpenDataHub"`),
		jq.Match(`.spec.notebooksV1 == null`),
	)))
}

func (s workbenchesSuite) mlflowEnabled(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: newModuleCRConfig(),
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.mlflowEnabled == true`),
	)))
}

func (s workbenchesSuite) workbenchesV2Managed(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Workbenches.WorkbenchesV2.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: newModuleCRConfig(),
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.workbenchesV2.managementState == %q`, operatorv1.Managed),
	)))
}

func (s workbenchesSuite) selfManagedRhoaiPlatformMapping(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{
			GatewayConfig: &serviceApi.GatewayConfig{
				Status: serviceApi.GatewayConfigStatus{Domain: "apps.example.com"},
			},
			Release: common.Release{Name: cluster.SelfManagedRhoai},
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.platform == "SelfManagedRhoai"`),
	)))
}

func (s workbenchesSuite) managedRhoaiPlatformMapping(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{
			GatewayConfig: &serviceApi.GatewayConfig{
				Status: serviceApi.GatewayConfigStatus{Domain: "apps.example.com"},
			},
			Release: common.Release{Name: cluster.ManagedRhoai},
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.platform == "SelfManagedRhoai"`),
	)))
}

func (s workbenchesSuite) unknownReleaseFallsBackToODH(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{
			GatewayConfig: &serviceApi.GatewayConfig{
				Status: serviceApi.GatewayConfigStatus{Domain: "apps.example.com"},
			},
			Release: common.Release{Name: "unknown-platform"},
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.platform == "OpenDataHub"`),
	)))
}

func (s workbenchesSuite) removedSelection(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Workbenches.ManagementState = operatorv1.Removed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.managementState == %q`, operatorv1.Removed),
	)))
}

// withOpenShiftCluster pins the cluster type for the duration of a suite
// case, restoring the previous value afterwards. The default gateway name
// and namespace projected into spec.ingresses are cluster-type dependent.
func withOpenShiftCluster(t *testing.T) {
	t.Helper()
	previous := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(previous) })
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
}

// ingressesFromGatewayStatus ports main's TestBuildModuleCR_ProjectsIngresses:
// the default ingress leads the list, additional ingresses follow in status
// order, entries without a GatewayRef are dropped, and the deprecated
// additionalIngressNames field is not projected.
func (s workbenchesSuite) ingressesFromGatewayStatus(t *testing.T) {
	withOpenShiftCluster(t)

	cfg := newModuleCRConfig()
	cfg.GatewayConfig.Status.AdditionalIngresses = []serviceApi.AdditionalIngressStatus{
		{
			Name: "beta", Hostname: "beta.example.com",
			GatewayRef: serviceApi.GatewayReference{Name: "beta-parent", Namespace: gateway.GatewayNamespace},
			Conditions: []common.Condition{{Type: "Ready", Status: "False"}},
		},
		{Name: "alpha", Hostname: "alpha.example.com", GatewayRef: serviceApi.GatewayReference{Name: "alpha", Namespace: gateway.GatewayNamespace}},
		{Name: "pending", Hostname: "pending.example.com"},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: newDSC(operatorv1.Managed),
		},
		Cfg: cfg,
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.additionalIngressNames == null`),
		jq.Match(`.spec.ingresses == [
			{name: %q, gatewayName: %q, gatewayNamespace: %q, hostname: "apps.example.com", isDefault: true},
			{name: "beta", gatewayName: "beta-parent", gatewayNamespace: %q, hostname: "beta.example.com"},
			{name: "alpha", gatewayName: "alpha", gatewayNamespace: %q, hostname: "alpha.example.com"}
		]`,
			gateway.DefaultGatewayName, gateway.DefaultGatewayName, gateway.GatewayNamespace,
			gateway.GatewayNamespace, gateway.GatewayNamespace),
	)))
}

// defaultIngressOnly covers the no-additional-ingresses projection: the
// default gateway entry is still emitted.
func (s workbenchesSuite) defaultIngressOnly(t *testing.T) {
	withOpenShiftCluster(t)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: newDSC(operatorv1.Managed),
		},
		Cfg: newModuleCRConfig(),
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.ingresses == [
			{name: %q, gatewayName: %q, gatewayNamespace: %q, hostname: "apps.example.com", isDefault: true}
		]`, gateway.DefaultGatewayName, gateway.DefaultGatewayName, gateway.GatewayNamespace),
	)))
}

// noGatewayConfigKeepsEmptyIngresses covers a config without gateway status:
// gatewayDomain is emitted empty and ingresses stays an empty list (which the
// module CRD documents as preserving the notebook controller's fallback).
func (s workbenchesSuite) noGatewayConfigKeepsEmptyIngresses(t *testing.T) {
	withOpenShiftCluster(t)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: newDSC(operatorv1.Managed),
		},
		Cfg: &modules.ModuleCRConfig{
			Release: common.Release{Name: cluster.OpenDataHub},
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.gatewayDomain == ""`),
		jq.Match(`.spec.ingresses == []`),
	)))
}

// xksDefaultIngress covers the vanilla Kubernetes gateway coordinates: the
// default entry points at the XKS gateway name and namespace.
func (s workbenchesSuite) xksDefaultIngress(t *testing.T) {
	previous := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(previous) })
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: newDSC(operatorv1.Managed),
		},
		Cfg: newModuleCRConfig(),
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.ingresses == [
			{name: %q, gatewayName: %q, gatewayNamespace: %q, hostname: "apps.example.com", isDefault: true}
		]`, gateway.XKSDefaultGatewayName, gateway.XKSDefaultGatewayName, gateway.XKSGatewayNamespace),
	)))
}

// TestBuildModuleCRProjection validates Workbenches projections against the
// real chart CRD from the local downloaded assets. It consolidates the
// successful BuildModuleCR cases and the two former offline schema-validation
// scenarios (which shared identical inputs) from handler_test.go, replacing
// the vendored-schema loader with real API-server admission. The ingress
// cases port main's TestBuildModuleCR_ProjectsIngresses.
func TestBuildModuleCRProjection(t *testing.T) {
	h := NewHandler()
	s := workbenchesSuite{
		Runner: projection.NewRunner(t, h, crdPath),
	}

	t.Run("minimal managed selection with nil config", s.minimalManagedSelectionWithNilConfig)
	t.Run("populated config with gateway", s.populatedConfigWithGateway)
	t.Run("mlflow enabled", s.mlflowEnabled)
	t.Run("workbenches v2 managed", s.workbenchesV2Managed)
	t.Run("self managed rhoai platform mapping", s.selfManagedRhoaiPlatformMapping)
	t.Run("managed rhoai platform mapping", s.managedRhoaiPlatformMapping)
	t.Run("unknown release falls back to odh", s.unknownReleaseFallsBackToODH)
	t.Run("removed selection", s.removedSelection)
	t.Run("ingresses from gateway status", s.ingressesFromGatewayStatus)
	t.Run("default ingress only", s.defaultIngressOnly)
	t.Run("no gateway config keeps empty ingresses", s.noGatewayConfigKeepsEmptyIngresses)
	t.Run("xks default ingress", s.xksDefaultIngress)
}

// TestBuildModuleCR_IngressesSupportDeepCopy ports main's
// TestBuildModuleCR_IngressesSupportDeepCopy: the deployment cache deep-copies
// the unstructured CR before hashing it, so the ingresses projection must
// survive DeepCopy without panicking. It needs no admission and stays here to
// keep the ingresses coverage together.
func TestBuildModuleCR_IngressesSupportDeepCopy(t *testing.T) {
	withAdditionalIngress := newModuleCRConfig()
	withAdditionalIngress.GatewayConfig.Status.AdditionalIngresses = []serviceApi.AdditionalIngressStatus{
		{Name: "alpha", Hostname: "alpha.example.com", GatewayRef: serviceApi.GatewayReference{Name: "alpha", Namespace: gateway.GatewayNamespace}},
	}

	for _, tc := range []struct {
		name string
		cfg  *modules.ModuleCRConfig
	}{
		{name: "additional ingress", cfg: withAdditionalIngress},
		{name: "default ingress only", cfg: newModuleCRConfig()},
		{name: "no gateway config", cfg: &modules.ModuleCRConfig{}},
		{name: "no module config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			moduleCR, err := NewHandler().BuildModuleCR(context.Background(), nil, &modules.DSCContext{DSC: newDSC(operatorv1.Managed)}, tc.cfg)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(func() { moduleCR.DeepCopy() }).NotTo(Panic())
		})
	}
}
