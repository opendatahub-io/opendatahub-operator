//nolint:testpackage // Exercises package-private prerequisite gating directly.
package modules

import (
	"context"
	"testing"

	semver "github.com/blang/semver/v4"
	ofversion "github.com/operator-framework/api/pkg/lib/version"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	conditionstest "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/monitor"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

// prereqModuleStub is a provisioningModuleStub that also declares cross-operator
// prerequisites via the ModulePrerequisite interface.
type prereqModuleStub struct {
	provisioningModuleStub

	requiredAPIs []monitor.RequiredAPI
}

func (s prereqModuleStub) GetRequiredAPIs() []monitor.RequiredAPI { return s.requiredAPIs }

// establishedCRD returns a CRD object that CheckRequiredAPIs treats as usable:
// it serves v1 and reports Established=True.
func establishedCRD(name string) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group:    "cert-manager.io",
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{Name: "v1", Served: true}},
		},
		Status: apiextensionsv1.CustomResourceDefinitionStatus{
			Conditions: []apiextensionsv1.CustomResourceDefinitionCondition{{
				Type:   apiextensionsv1.Established,
				Status: apiextensionsv1.ConditionTrue,
			}},
		},
	}
}

func certManagerRequiredAPIs() []monitor.RequiredAPI {
	return []monitor.RequiredAPI{
		{CRDName: gvk.CertManagerIssuerCRDName, GVK: gvk.CertManagerIssuer},
		{CRDName: gvk.CertManagerCertificateCRDName, GVK: gvk.CertManagerCertificate},
	}
}

// TestProvisionModulesSkipsModuleWhenPrerequisiteMissing verifies that a module
// declaring a missing prerequisite is not provisioned: no operator manifests and
// no env injection are appended, so the deploy action never creates resources
// that reference the absent API (e.g. a webhook Deployment mounting a Secret that
// cert-manager can never issue).
func TestProvisionModulesSkipsModuleWhenPrerequisiteMissing(t *testing.T) {
	withTestRegistry(t)

	handler := prereqModuleStub{
		provisioningModuleStub: provisioningModuleStub{moduleName: testProvisioningModuleName, enabled: true},
		requiredAPIs:           certManagerRequiredAPIs(),
	}
	DefaultRegistry().Add(handler, WithRunlevel(dag.RL(20)))
	provision.Add(handler.GetName(), provision.KindModule, dag.RL(20))

	dsc := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: testDSCName, UID: "uid-1"}}
	dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: testDSCIName}}
	dsci.Spec.ApplicationsNamespace = testApplicationsNamespace

	// No cert-manager CRDs registered on the cluster.
	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dsci))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Release:    common.Release{Name: common.Platform("Open Data Hub"), Version: ofversion.OperatorVersion{Version: semver.MustParse(testProvisioningVersion)}},
		Conditions: conditionstest.NewManager(dsc, status.ConditionTypeModulesReady),
	}

	if err := provisionModules(context.Background(), rr); err != nil {
		t.Fatalf("provision modules: %v", err)
	}

	if len(rr.Manifests) != 0 {
		t.Fatalf("expected no module manifests when prerequisite missing, got %d", len(rr.Manifests))
	}
	if len(rr.HelmCharts) != 0 {
		t.Fatalf("expected no helm charts when prerequisite missing, got %d", len(rr.HelmCharts))
	}
	if mei := types.GetModuleEnvInjection(rr); mei != nil && len(mei.PerModuleImages) != 0 {
		t.Fatalf("expected no module env injection when prerequisite missing, got %#v", mei)
	}
}

// TestProvisionModulesProvisionsModuleWhenPrerequisiteMet verifies that once the
// declared prerequisites are present (CRDs served and Established), the module is
// provisioned normally.
func TestProvisionModulesProvisionsModuleWhenPrerequisiteMet(t *testing.T) {
	withTestRegistry(t)

	handler := prereqModuleStub{
		provisioningModuleStub: provisioningModuleStub{moduleName: testProvisioningModuleName, enabled: true},
		requiredAPIs:           certManagerRequiredAPIs(),
	}
	DefaultRegistry().Add(handler, WithRunlevel(dag.RL(20)))
	provision.Add(handler.GetName(), provision.KindModule, dag.RL(20))

	dsc := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: testDSCName, UID: "uid-1"}}
	dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: testDSCIName}}
	dsci.Spec.ApplicationsNamespace = testApplicationsNamespace

	cli, err := fakeclient.New(fakeclient.WithObjects(
		dsc, dsci,
		establishedCRD(gvk.CertManagerIssuerCRDName),
		establishedCRD(gvk.CertManagerCertificateCRDName),
	))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Release:    common.Release{Name: common.Platform("Open Data Hub"), Version: ofversion.OperatorVersion{Version: semver.MustParse(testProvisioningVersion)}},
		Conditions: conditionstest.NewManager(dsc, status.ConditionTypeModulesReady),
	}

	if err := provisionModules(context.Background(), rr); err != nil {
		t.Fatalf("provision modules: %v", err)
	}

	if len(rr.Manifests) != 1 {
		t.Fatalf("expected one module manifest when prerequisite met, got %d", len(rr.Manifests))
	}
}

// TestComputeModulesStatusReportsDependenciesNotReady verifies the status path
// reports an enabled module with a missing prerequisite as NotReady with the
// DependenciesNotReady reason, and marks the aggregate ModulesReady False — so
// the failure is visible rather than a vague "has not reported Ready yet".
func TestComputeModulesStatusReportsDependenciesNotReady(t *testing.T) {
	withTestRegistry(t)

	handler := prereqModuleStub{
		provisioningModuleStub: provisioningModuleStub{moduleName: testProvisioningModuleName, enabled: true},
		requiredAPIs:           certManagerRequiredAPIs(),
	}
	DefaultRegistry().Add(handler, WithRunlevel(dag.RL(20)))

	dsc := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: testDSCName}}
	dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: testDSCIName}}
	dsci.Spec.ApplicationsNamespace = testApplicationsNamespace

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dsci))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Release:    common.Release{Name: common.Platform("Open Data Hub"), Version: ofversion.OperatorVersion{Version: semver.MustParse(testProvisioningVersion)}},
		Conditions: conditionstest.NewManager(dsc, status.ConditionTypeModulesReady),
	}

	if err := ComputeModulesStatusDetailed(context.Background(), rr); err != nil {
		t.Fatalf("compute modules status: %v", err)
	}

	moduleCond := conditions.FindStatusCondition(dsc.GetStatus(), readyConditionTypeFor(handler))
	if moduleCond == nil {
		t.Fatalf("expected per-module condition %q to be set", readyConditionTypeFor(handler))
	}
	if moduleCond.Status != metav1.ConditionFalse || moduleCond.Reason != status.DependenciesNotReadyReason {
		t.Fatalf("expected module condition False/%q, got %#v", status.DependenciesNotReadyReason, moduleCond)
	}

	ready := conditions.FindStatusCondition(dsc.GetStatus(), status.ConditionTypeModulesReady)
	if ready == nil || ready.Status != metav1.ConditionFalse {
		t.Fatalf("expected ModulesReady=False, got %#v", ready)
	}
}

// TestCheckModulePrerequisitesNoInterface verifies handlers that do not declare
// prerequisites are always considered satisfied.
func TestCheckModulePrerequisitesNoInterface(t *testing.T) {
	cli, err := fakeclient.New()
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	met, reason, err := checkModulePrerequisites(context.Background(), cli,
		provisioningModuleStub{moduleName: testProvisioningModuleName, enabled: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !met || reason != "" {
		t.Fatalf("expected handler without prerequisites to be satisfied, got met=%v reason=%q", met, reason)
	}
}
