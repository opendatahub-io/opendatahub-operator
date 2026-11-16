//nolint:testpackage // Needs package access for BaseHandler helper coverage.
package modules

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

const (
	baseTestModuleName         = "test-module"
	baseTestModuleCRName       = "default-test-module"
	baseTestModuleKind         = "TestModule"
	baseTestModuleGroup        = "test.opendatahub.io"
	baseTestModuleVersion      = "v1alpha1"
	baseTestConfigMapKind      = "ConfigMap"
	baseTestDeploymentName     = "test-module-controller-manager"
	baseTestControllerImageEnv = "RELATED_IMAGE_TEST_MODULE_CONTROLLER"
	baseTestRelatedImageEnv    = "RELATED_IMAGE_TEST_MODULE"
	baseTestPlatformVersion    = "2.30.0"
	baseTestConfigMapName      = "test-module-config"
	baseTestOverlayODH         = "overlays/odh"
	baseTestKeyAPIVersion      = "apiVersion"
	baseTestKeyKind            = "kind"
	baseTestKeyMetadata        = "metadata"
	baseTestKeyName            = "name"
	baseTestKeyNamespace       = "namespace"
	baseTestKeyConditions      = "conditions"
	baseTestKeyConditionType   = "type"
	baseTestKeyConditionStatus = "status"
	baseTestConditionReadyType = "Ready"
	baseTestConditionTrue      = "True"
	baseTestRequiredEnvToggle  = "ENABLE_TEST_MODULE_CONTROLLER"
	cleanupInitContainerName   = "copy-manifests"
)

func TestBaseHandlerAccessorsAndStatusHelpers(t *testing.T) {
	handler := &BaseHandler{
		Config: ModuleConfig{
			Name:              baseTestModuleName,
			CRName:            baseTestModuleCRName,
			GVK:               schema.GroupVersionKind{Group: baseTestModuleGroup, Version: baseTestModuleVersion, Kind: baseTestModuleKind},
			ContainerName:     "custom-manager",
			DeploymentName:    baseTestDeploymentName,
			ControllerImage:   baseTestControllerImageEnv,
			InitContainerName: cleanupInitContainerName,
			RelatedImages:     []string{baseTestRelatedImageEnv},
			ExtraEnv:          map[string]string{baseTestRequiredEnvToggle: "true"},
		},
	}

	if got := handler.GetName(); got != baseTestModuleName {
		t.Fatalf("expected name %q, got %q", baseTestModuleName, got)
	}
	if got := handler.GetContainerName(); got != "custom-manager" {
		t.Fatalf("expected container name %q, got %q", "custom-manager", got)
	}
	if got := handler.GetDeploymentName(); got != baseTestDeploymentName {
		t.Fatalf("expected deployment name %q, got %q", baseTestDeploymentName, got)
	}
	if got := handler.GetControllerImage(); got != baseTestControllerImageEnv {
		t.Fatalf("expected controller image env %q, got %q", baseTestControllerImageEnv, got)
	}
	if got := handler.GetInitContainerName(); got != cleanupInitContainerName {
		t.Fatalf("expected init container name %q, got %q", cleanupInitContainerName, got)
	}
	if len(handler.GetRelatedImages()) != 1 || handler.GetRelatedImages()[0] != baseTestRelatedImageEnv {
		t.Fatalf("expected related image env vars to round-trip, got %#v", handler.GetRelatedImages())
	}

	extraEnv := handler.GetExtraEnv()
	if extraEnv[baseTestRequiredEnvToggle] != "true" {
		t.Fatalf("expected copied extra env, got %#v", extraEnv)
	}
	extraEnv[baseTestRequiredEnvToggle] = "false"
	if handler.GetExtraEnv()[baseTestRequiredEnvToggle] != "true" {
		t.Fatalf("expected GetExtraEnv to return a defensive copy")
	}
}

func TestBaseHandlerGetModuleStatusAndCRLifecycle(t *testing.T) {
	moduleGVK := schema.GroupVersionKind{Group: baseTestModuleGroup, Version: baseTestModuleVersion, Kind: baseTestModuleKind}
	handler := &BaseHandler{
		Config: ModuleConfig{
			Name:   baseTestModuleName,
			CRName: baseTestModuleCRName,
			GVK:    moduleGVK,
		},
	}

	module := &unstructured.Unstructured{}
	module.SetGroupVersionKind(moduleGVK)
	module.SetName(baseTestModuleCRName)
	module.SetGeneration(7)
	module.Object["status"] = map[string]any{
		"observedGeneration": int64(7),
		baseTestKeyConditions: []any{
			map[string]any{
				baseTestKeyConditionType:   baseTestConditionReadyType,
				baseTestKeyConditionStatus: baseTestConditionTrue,
				"reason":                   baseTestConditionReadyType,
				"message":                  "module ready",
				"observedGeneration":       int64(7),
			},
		},
		"releases": []any{
			map[string]any{baseTestKeyName: "platform", "version": baseTestPlatformVersion},
		},
	}

	cli, err := fakeclient.New(
		fakeclient.WithObjects(module),
		fakeclient.WithGVKs(fakeclient.GVKMapping{GVK: moduleGVK, Scope: meta.RESTScopeRoot}),
	)
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	status, err := handler.GetModuleStatus(context.Background(), cli)
	if err != nil {
		t.Fatalf("get module status: %v", err)
	}
	if status.ObservedGeneration != 7 || status.Generation != 7 {
		t.Fatalf("expected generation/observedGeneration to round-trip, got %#v", status)
	}
	if status.ReleaseVersion != baseTestPlatformVersion {
		t.Fatalf("expected platform release version %q, got %q", baseTestPlatformVersion, status.ReleaseVersion)
	}

	crState, err := handler.GetModuleCRState(context.Background(), cli)
	if err != nil {
		t.Fatalf("get module CR state: %v", err)
	}
	if crState != CRStateAlive {
		t.Fatalf("expected CRStateAlive, got %v", crState)
	}

	if err := handler.DeleteModuleCR(context.Background(), cli); err != nil {
		t.Fatalf("delete module CR: %v", err)
	}
	crState, err = handler.GetModuleCRState(context.Background(), cli)
	if err != nil {
		t.Fatalf("get module CR state after delete: %v", err)
	}
	if crState != CRStateAbsent {
		t.Fatalf("expected CRStateAbsent after delete, got %v", crState)
	}
}
