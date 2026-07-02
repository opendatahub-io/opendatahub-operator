//nolint:testpackage // Verifies handler internals such as defaults and projected fields.
package mlflowoperator

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

const mlflowTestDSCName = "test-dsc"

func TestIsEnabled(t *testing.T) {
	handler := NewHandler()

	if handler.IsEnabled(nil) {
		t.Fatalf("expected nil platform context to disable module")
	}

	platform := &modules.PlatformContext{
		DSC: &dscv2.DataScienceCluster{
			Spec: dscv2.DataScienceClusterSpec{},
		},
	}
	if handler.IsEnabled(platform) {
		t.Fatalf("expected Removed MLflowOperator to be disabled")
	}

	platform.DSC.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed
	if !handler.IsEnabled(platform) {
		t.Fatalf("expected Managed MLflowOperator to enable module")
	}
}

func TestGetOperatorManifests(t *testing.T) {
	handler := NewHandler()

	odhManifests := handler.GetOperatorManifests(&modules.PlatformContext{
		Release: common.Release{Name: cluster.OpenDataHub},
	})
	if len(odhManifests.Manifests) != 1 || odhManifests.Manifests[0].SourcePath != "overlays/odh" {
		t.Fatalf("expected ODH overlay, got %#v", odhManifests.Manifests)
	}

	rhoaiManifests := handler.GetOperatorManifests(&modules.PlatformContext{
		Release: common.Release{Name: cluster.SelfManagedRhoai},
	})
	if len(rhoaiManifests.Manifests) != 1 || rhoaiManifests.Manifests[0].SourcePath != "overlays/rhoai" {
		t.Fatalf("expected RHOAI overlay, got %#v", rhoaiManifests.Manifests)
	}

	managedRhoaiManifests := handler.GetOperatorManifests(&modules.PlatformContext{
		Release: common.Release{Name: cluster.ManagedRhoai},
	})
	if len(managedRhoaiManifests.Manifests) != 1 || managedRhoaiManifests.Manifests[0].SourcePath != "overlays/rhoai" {
		t.Fatalf("expected managed RHOAI overlay, got %#v", managedRhoaiManifests.Manifests)
	}
}

func TestBuildModuleCR(t *testing.T) {
	handler := NewHandler()

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: mlflowTestDSCName},
	}
	dsc.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed

	moduleCR, err := handler.BuildModuleCR(t.Context(), nil, &modules.PlatformContext{
		ApplicationsNamespace: "redhat-ods-applications",
		GatewayDomain:         "gateway.apps.example.com",
		Release:               common.Release{Name: cluster.SelfManagedRhoai},
		DSC:                   dsc,
	})
	if err != nil {
		t.Fatalf("build module CR: %v", err)
	}

	if moduleCR.GetName() != crName {
		t.Fatalf("expected CR name %q, got %q", crName, moduleCR.GetName())
	}

	spec, ok := moduleCR.Object["spec"].(map[string]any)
	if !ok {
		t.Fatalf("expected unstructured spec map, got %#v", moduleCR.Object["spec"])
	}
	if _, found := spec["applicationsNamespace"]; found {
		t.Fatalf("expected applications namespace to stay deployment-scoped, got %#v", spec["applicationsNamespace"])
	}
	if spec["gatewayName"] != defaultGatewayName {
		t.Fatalf("expected gatewayName %q, got %#v", defaultGatewayName, spec["gatewayName"])
	}
	if spec["sectionTitle"] != "OpenShift Self Managed Services" {
		t.Fatalf("expected RHOAI section title, got %#v", spec["sectionTitle"])
	}

	gateway, ok := spec["gateway"].(map[string]any)
	if !ok || gateway["domain"] != "gateway.apps.example.com" {
		t.Fatalf("expected gateway domain projection, got %#v", spec["gateway"])
	}
}

func TestUpdateDSCComponentStatus(t *testing.T) {
	g := NewWithT(t)
	handler := NewHandler()

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: mlflowTestDSCName},
	}
	dsc.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed

	module := &componentApi.MLflowOperator{}
	module.SetGroupVersionKind(gvk.MLflowOperator)
	module.SetName(componentApi.MLflowOperatorInstanceName)
	module.Status.Conditions = []common.Condition{{
		Type:    status.ConditionTypeReady,
		Status:  metav1.ConditionTrue,
		Reason:  status.ReadyReason,
		Message: "Component is ready",
	}}

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, module))
	g.Expect(err).ShouldNot(HaveOccurred())

	_, err = handler.UpdateDSCComponentStatus(t.Context(), &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Conditions: conditions.NewManager(dsc, status.ConditionTypeModulesReady),
	}, &modules.PlatformContext{DSC: dsc})
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(dsc).Should(WithTransform(json.Marshal, And(
		jq.Match(`.status.components.mlflowoperator.managementState == "%s"`, operatorv1.Managed),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, readyConditionType, metav1.ConditionTrue),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, readyConditionType, status.ReadyReason),
	)))
}

func TestUpdateDSCComponentStatusNormalizesEmptyManagementState(t *testing.T) {
	g := NewWithT(t)
	handler := NewHandler()

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: mlflowTestDSCName},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc))
	g.Expect(err).ShouldNot(HaveOccurred())

	_, err = handler.UpdateDSCComponentStatus(t.Context(), &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Conditions: conditions.NewManager(dsc, status.ConditionTypeModulesReady),
	}, &modules.PlatformContext{DSC: dsc})
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(dsc).Should(WithTransform(json.Marshal, And(
		jq.Match(`.status.components.mlflowoperator.managementState == "%s"`, operatorv1.Removed),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, readyConditionType, operatorv1.Removed),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .severity == "%s"`, readyConditionType, common.ConditionSeverityInfo),
	)))
}

func TestUpdateDSCComponentStatusPropagatesGetErrors(t *testing.T) {
	g := NewWithT(t)
	handler := NewHandler()

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dsc"},
	}
	dsc.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dsc),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
				if _, ok := obj.(*componentApi.MLflowOperator); ok {
					return errors.New("boom")
				}
				return nil
			},
		}),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	_, err = handler.UpdateDSCComponentStatus(t.Context(), &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Conditions: conditions.NewManager(dsc, status.ConditionTypeModulesReady),
	}, &modules.PlatformContext{DSC: dsc})
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("boom"))
}
