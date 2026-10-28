package controller

import (
	"context"
	"testing"

	frameworkapi "github.com/opendatahub-io/odh-platform-utilities/framework/api"
	"github.com/opendatahub-io/odh-platform-utilities/framework/controller/conditions"
	frameworktypes "github.com/opendatahub-io/odh-platform-utilities/framework/controller/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	syntheticv1alpha1 "github.com/opendatahub-io/opendatahub-operator/tests/e2e/syntheticmodule/api/v1alpha1"
)

func TestRenderOwnedConfigMap(t *testing.T) {
	t.Setenv(ApplicationsNamespaceEnv, "test-applications")

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	instance := &syntheticv1alpha1.SyntheticModule{
		Spec: syntheticv1alpha1.SyntheticModuleSpec{Message: "hello"},
	}
	rr := &frameworktypes.ReconciliationRequest{Client: client, Instance: instance}

	if err := renderOwnedConfigMap()(context.Background(), rr); err != nil {
		t.Fatalf("render owned ConfigMap: %v", err)
	}
	if len(rr.Resources) != 1 {
		t.Fatalf("expected one resource, got %d", len(rr.Resources))
	}
	resource := rr.Resources[0]
	if resource.GetName() != OwnedConfigMapName || resource.GetNamespace() != "test-applications" {
		t.Fatalf("unexpected resource identity %s/%s", resource.GetNamespace(), resource.GetName())
	}
	message, found, err := unstructuredString(resource.Object, "data", "message")
	if err != nil || !found || message != "hello" {
		t.Fatalf("unexpected projected message %q, found=%t, err=%v", message, found, err)
	}
}

func TestLoadPlatformVersionAndUpdateReleaseStatus(t *testing.T) {
	t.Setenv(ApplicationsNamespaceEnv, "test-applications")

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.ConfigMap{
		ObjectMeta: metav1ObjectMeta(PlatformConfigName, "test-applications"),
		Data:       map[string]string{"platformVersion": "3.1.4"},
	}).Build()
	instance := &syntheticv1alpha1.SyntheticModule{}
	rr := &frameworktypes.ReconciliationRequest{Client: client, Instance: instance}

	if err := loadPlatformVersion()(context.Background(), rr); err != nil {
		t.Fatalf("load platform version: %v", err)
	}
	if err := updateReleaseStatus(context.Background(), rr, true); err != nil {
		t.Fatalf("update release status: %v", err)
	}

	release := instance.Status.GetRelease(frameworkapi.ReleasePlatform)
	if release == nil || release.Version != "3.1.4" {
		t.Fatalf("unexpected platform release: %#v", release)
	}
}

func TestUpdateReleaseStatusSkipsUnhealthyReconcile(t *testing.T) {
	instance := &syntheticv1alpha1.SyntheticModule{}
	rr := &frameworktypes.ReconciliationRequest{Instance: instance}

	if err := updateReleaseStatus(context.Background(), rr, false); err != nil {
		t.Fatalf("update release status: %v", err)
	}
	if release := instance.Status.GetRelease(frameworkapi.ReleasePlatform); release != nil {
		t.Fatalf("expected no release, got %#v", release)
	}
}

func TestInjectFault(t *testing.T) {
	tests := []struct {
		name         string
		mode         string
		wantError    bool
		wantDegraded bool
	}{
		{name: "healthy", mode: "", wantError: false, wantDegraded: false},
		{name: "unknown is ignored", mode: "unknown", wantError: false, wantDegraded: false},
		{name: "ready false", mode: syntheticv1alpha1.FaultModeReadyFalse, wantError: true, wantDegraded: false},
		{name: "degraded", mode: syntheticv1alpha1.FaultModeDegraded, wantError: false, wantDegraded: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance := &syntheticv1alpha1.SyntheticModule{
				ObjectMeta: metav1ObjectMeta(syntheticv1alpha1.SyntheticModuleInstanceName, ""),
			}
			instance.Generation = 4
			if tt.mode != "" {
				instance.Annotations = map[string]string{syntheticv1alpha1.FaultAnnotationKey: tt.mode}
			}
			rr := &frameworktypes.ReconciliationRequest{
				Instance:   instance,
				Conditions: conditions.NewManager(instance, nil),
			}

			err := injectFault()(context.Background(), rr)
			if (err != nil) != tt.wantError {
				t.Fatalf("error = %v, wantError=%t", err, tt.wantError)
			}
			degraded := rr.Conditions.GetCondition(frameworkapi.ConditionTypeDegraded)
			if (degraded != nil && degraded.Status == metav1.ConditionTrue) != tt.wantDegraded {
				t.Fatalf("degraded condition = %#v, want true=%t", degraded, tt.wantDegraded)
			}
		})
	}
}

func TestUpdateReleaseStatusFaults(t *testing.T) {
	tests := []struct {
		name                   string
		mode                   string
		wantVersion            string
		wantObservedGeneration int64
	}{
		{name: "healthy", wantVersion: "3.1.4", wantObservedGeneration: 7},
		{name: "stale generation", mode: syntheticv1alpha1.FaultModeStaleGeneration, wantVersion: "3.1.4", wantObservedGeneration: 6},
		{name: "stale platform release", mode: syntheticv1alpha1.FaultModeStalePlatformRelease, wantVersion: faultReleaseVersion, wantObservedGeneration: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance := &syntheticv1alpha1.SyntheticModule{ObjectMeta: metav1.ObjectMeta{
				Generation:  7,
				Annotations: map[string]string{syntheticv1alpha1.FaultAnnotationKey: tt.mode},
			}}
			instance.Status.ObservedGeneration = instance.Generation
			rr := &frameworktypes.ReconciliationRequest{
				Instance:   instance,
				Extensions: map[string]any{"synthetic.platformVersion": "3.1.4"},
			}

			if err := updateReleaseStatus(context.Background(), rr, true); err != nil {
				t.Fatalf("update release status: %v", err)
			}
			release := instance.Status.GetRelease(frameworkapi.ReleasePlatform)
			if release == nil || release.Version != tt.wantVersion {
				t.Fatalf("platform release = %#v, want version %q", release, tt.wantVersion)
			}
			if instance.Status.ObservedGeneration != tt.wantObservedGeneration {
				t.Fatalf("observedGeneration = %d, want %d", instance.Status.ObservedGeneration, tt.wantObservedGeneration)
			}
		})
	}
}

func TestHoldFinalizerFault(t *testing.T) {
	instance := &syntheticv1alpha1.SyntheticModule{ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{syntheticv1alpha1.FaultAnnotationKey: syntheticv1alpha1.FaultModeHoldFinalizer},
	}}
	rr := &frameworktypes.ReconciliationRequest{Instance: instance}

	if err := finalize()(context.Background(), rr); err == nil {
		t.Fatal("expected hold-finalizer to return an error")
	}
	delete(instance.Annotations, syntheticv1alpha1.FaultAnnotationKey)
	if err := finalize()(context.Background(), rr); err != nil {
		t.Fatalf("expected finalization after annotation removal, got %v", err)
	}
}
