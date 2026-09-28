package applier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testManagerYAML = `apiVersion: v1
kind: Namespace
metadata:
  name: system
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
spec:
  template:
    spec:
      containers:
      - name: manager
        env:
          - name: OPERATOR_NAMESPACE
            valueFrom:
              fieldRef:
                fieldPath: metadata.namespace
          - name: RELATED_IMAGE_DSP
            value: "old-value"
`

func writeManagerFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "manager.yaml")
	if err := os.WriteFile(path, []byte(testManagerYAML), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestApplyDeploy_UpdateExisting(t *testing.T) {
	managerPath := writeManagerFile(t)
	cfgPath := writeValidTestConfig(t)

	err := ApplyDeploy(DeployOptions{
		ConfigFile:  cfgPath,
		Platform:    "odh",
		ManagerFile: managerPath,
	})
	if err != nil {
		t.Fatalf("ApplyDeploy failed: %v", err)
	}

	data, err := os.ReadFile(managerPath)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "quay.io/opendatahub/dsp-operator@sha256:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc") {
		t.Error("expected updated DSP image in manager.yaml")
	}
	if strings.Contains(content, "old-value") {
		t.Error("old value should have been replaced")
	}
	// valueFrom env var should be preserved
	if !strings.Contains(content, "fieldRef") {
		t.Error("valueFrom env var should be preserved")
	}
	// Namespace doc should still be present
	if !strings.Contains(content, "kind: Namespace") {
		t.Error("Namespace document should be preserved")
	}
}

func TestApplyDeploy_AppendNew(t *testing.T) {
	managerYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
spec:
  template:
    spec:
      containers:
      - name: manager
        env:
          - name: OPERATOR_NAMESPACE
            value: "test-ns"
`
	dir := t.TempDir()
	managerPath := filepath.Join(dir, "manager.yaml")
	if err := os.WriteFile(managerPath, []byte(managerYAML), 0644); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeValidTestConfig(t)

	err := ApplyDeploy(DeployOptions{
		ConfigFile:  cfgPath,
		Platform:    "odh",
		ManagerFile: managerPath,
	})
	if err != nil {
		t.Fatalf("ApplyDeploy failed: %v", err)
	}

	data, err := os.ReadFile(managerPath)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "RELATED_IMAGE_DSP") {
		t.Error("expected RELATED_IMAGE_DSP appended to env")
	}
	if !strings.Contains(content, "OPERATOR_NAMESPACE") {
		t.Error("existing env var should be preserved")
	}
}

func TestApplyDeploy_NoDeployment(t *testing.T) {
	dir := t.TempDir()
	managerPath := filepath.Join(dir, "manager.yaml")
	if err := os.WriteFile(managerPath, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeValidTestConfig(t)

	err := ApplyDeploy(DeployOptions{
		ConfigFile:  cfgPath,
		Platform:    "odh",
		ManagerFile: managerPath,
	})
	if err == nil {
		t.Error("expected error when no Deployment found")
	}
}

func TestParseMultiDoc(t *testing.T) {
	docs, err := parseMultiDoc([]byte(testManagerYAML))
	if err != nil {
		t.Fatalf("parseMultiDoc failed: %v", err)
	}
	if len(docs) != 2 {
		t.Errorf("expected 2 documents, got %d", len(docs))
	}
}

func TestFindDeploymentDoc(t *testing.T) {
	docs, _ := parseMultiDoc([]byte(testManagerYAML))
	idx := findDeploymentDoc(docs)
	if idx != 1 {
		t.Errorf("expected Deployment at index 1, got %d", idx)
	}
}

func TestFindDeploymentDoc_NotFound(t *testing.T) {
	yaml := `---
apiVersion: v1
kind: Service
metadata:
  name: test-svc
`
	docs, err := parseMultiDoc([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	idx := findDeploymentDoc(docs)
	if idx != -1 {
		t.Errorf("expected -1 for no Deployment, got %d", idx)
	}
}

func TestFindManagerEnvNode_NoManagerContainer(t *testing.T) {
	yaml := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test
spec:
  template:
    spec:
      containers:
      - name: sidecar
        image: sidecar:latest
`
	docs, err := parseMultiDoc([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	root := docs[0].Content[0]
	_, err = findManagerEnvNode(root, "odh")
	if err == nil {
		t.Fatal("expected error when manager container not found")
	}
}

func TestApplyDeploy_RHOAI(t *testing.T) {
	rhoaiYAML := `apiVersion: v1
kind: Namespace
metadata:
  name: redhat-ods-operator
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rhods-operator
spec:
  template:
    spec:
      containers:
      - name: rhods-operator
        env:
          - name: OPERATOR_NAME
            value: "rhods-operator"
          - name: RELATED_IMAGE_DSP
            value: "old-value"
`
	dir := t.TempDir()
	managerPath := filepath.Join(dir, "manager.yaml")
	if err := os.WriteFile(managerPath, []byte(rhoaiYAML), 0644); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeValidTestConfig(t)

	err := ApplyDeploy(DeployOptions{
		ConfigFile:  cfgPath,
		Platform:    "rhoai",
		ManagerFile: managerPath,
	})
	if err != nil {
		t.Fatalf("ApplyDeploy failed for rhoai: %v", err)
	}

	data, err := os.ReadFile(managerPath)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "quay.io/rhoai/dsp-operator@sha256:aabbccdd11223344556677889900aabbccddeeff11223344556677889900aabb") {
		t.Error("expected updated RHOAI DSP image in manager.yaml")
	}
	if strings.Contains(content, "old-value") {
		t.Error("old value should have been replaced")
	}
	if !strings.Contains(content, "OPERATOR_NAME") {
		t.Error("existing env var OPERATOR_NAME should be preserved")
	}
}

func TestApplyDeploy_OpenDataHubNormalization(t *testing.T) {
	managerPath := writeManagerFile(t)
	cfgPath := writeValidTestConfig(t)

	err := ApplyDeploy(DeployOptions{
		ConfigFile:  cfgPath,
		Platform:    "OpenDataHub",
		ManagerFile: managerPath,
	})
	if err != nil {
		t.Fatalf("ApplyDeploy failed with platform OpenDataHub: %v", err)
	}

	data, err := os.ReadFile(managerPath)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "quay.io/opendatahub/dsp-operator@sha256:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc") {
		t.Error("expected updated ODH DSP image in manager.yaml")
	}
}

func TestApplyDeploy_UnknownPlatform(t *testing.T) {
	managerPath := writeManagerFile(t)
	cfgPath := writeValidTestConfig(t)

	err := ApplyDeploy(DeployOptions{
		ConfigFile:  cfgPath,
		Platform:    "invalid-platform",
		ManagerFile: managerPath,
	})
	if err == nil {
		t.Fatal("expected error for invalid platform")
	}
	if !strings.Contains(err.Error(), "unknown platform") {
		t.Errorf("expected unknown platform in error, got: %v", err)
	}
}

func TestFindManagerEnvNode_RHOAI(t *testing.T) {
	yaml := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: rhods-operator
spec:
  template:
    spec:
      containers:
      - name: rhods-operator
        env:
          - name: OPERATOR_NAME
            value: "rhods-operator"
`
	docs, err := parseMultiDoc([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	root := docs[0].Content[0]
	envNode, err := findManagerEnvNode(root, "rhoai")
	if err != nil {
		t.Fatalf("expected to find env node for rhoai container: %v", err)
	}
	if envNode == nil {
		t.Fatal("expected non-nil envNode")
	}
}

func TestFindManagerEnvNode_UnknownPlatform(t *testing.T) {
	yaml := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test
spec:
  template:
    spec:
      containers:
      - name: manager
`
	docs, err := parseMultiDoc([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	root := docs[0].Content[0]
	_, err = findManagerEnvNode(root, "invalid")
	if err == nil {
		t.Fatal("expected error for unknown platform")
	}
	if !strings.Contains(err.Error(), "unknown platform") {
		t.Errorf("expected unknown platform error, got: %v", err)
	}
}
