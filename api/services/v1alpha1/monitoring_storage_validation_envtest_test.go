package v1alpha1_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestMonitoringStorageValidationEnvtest exercises the generated DSCI CRD's
// validation against an API server, including storage defaults applied there.
func TestMonitoringStorageValidationEnvtest(t *testing.T) {
	projectDir, err := envtestutil.FindProjectRoot()
	if err != nil {
		t.Fatal(err)
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join(projectDir, "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testEnv.Stop(); err != nil {
			t.Error(err)
		}
	})

	scheme := runtime.NewScheme()
	if err := dsciv2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}

	storage := func(secret string) *serviceApi.LokiStorageConfig {
		return &serviceApi.LokiStorageConfig{Type: "s3", SecretName: secret}
	}

	for i, tc := range []struct {
		name        string
		logs        *serviceApi.LokiStorageConfig
		usageLogs   *serviceApi.LokiStorageConfig
		wantInvalid bool
	}{
		{name: "neither configured"},
		{name: "logs only", logs: storage("logs-s3")},
		{name: "usage logs only", usageLogs: storage("usage-s3")},
		{name: "matching storage", logs: storage("shared-s3"), usageLogs: storage("shared-s3")},
		{
			name:      "defaulted fields match explicit defaults",
			logs:      storage("shared-s3"),
			usageLogs: &serviceApi.LokiStorageConfig{Type: "s3", SecretName: "shared-s3", CredentialMode: "static", StorageClassName: "gp3-csi"},
		},
		{name: "different secrets", logs: storage("logs-s3"), usageLogs: storage("usage-s3"), wantInvalid: true},
		{
			name:        "different credential modes",
			logs:        storage("shared-s3"),
			usageLogs:   &serviceApi.LokiStorageConfig{Type: "s3", SecretName: "shared-s3", CredentialMode: "token"},
			wantInvalid: true,
		},
		{
			name:        "different storage classes",
			logs:        storage("shared-s3"),
			usageLogs:   &serviceApi.LokiStorageConfig{Type: "s3", SecretName: "shared-s3", StorageClassName: "other-csi"},
			wantInvalid: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dsci := &dsciv2.DSCInitialization{
				ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("monitoring-storage-%d", i)},
			}
			if tc.logs != nil {
				dsci.Spec.Monitoring.Logs = &serviceApi.Logs{Storage: tc.logs}
			}
			if tc.usageLogs != nil {
				dsci.Spec.Monitoring.UsageLogs = &serviceApi.UsageLogs{Storage: tc.usageLogs}
			}

			err := k8sClient.Create(t.Context(), dsci)
			if tc.wantInvalid {
				if !k8serr.IsInvalid(err) {
					t.Fatalf("expected invalid storage configuration, got %v", err)
				}
				if !strings.Contains(err.Error(), "logs.storage and usageLogs.storage must match") {
					t.Fatalf("expected shared LokiStack validation message, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("expected storage configuration to be accepted: %v", err)
			}
		})
	}
}
