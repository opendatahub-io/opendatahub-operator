package deploy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"

	. "github.com/onsi/gomega"
)

// The RELATED_IMAGE_* entries in component image param maps may reference env
// vars that are not present in every bundle (e.g. entries shipped ahead of the
// CSV wiring). These tests pin the dormancy guarantee: ApplyParams must leave
// the seeded params.env value untouched when the env var is unset or empty.

func TestApplyParamsPreservesValueWhenEnvVarNotSet(t *testing.T) {
	g := NewWithT(t)

	dir := t.TempDir()
	paramsFile := filepath.Join(dir, "params.env")
	g.Expect(os.WriteFile(paramsFile, []byte("my-image=original\n"), 0o600)).Should(Succeed())

	err := deploy.ApplyParams(dir, "params.env", map[string]string{"my-image": "TEST_APPLY_PARAMS_UNSET_ENV_VAR"})
	g.Expect(err).ShouldNot(HaveOccurred())

	content, err := os.ReadFile(paramsFile)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(string(content)).Should(Equal("my-image=original\n"))
}

func TestApplyParamsPreservesValueWhenEnvVarEmpty(t *testing.T) {
	g := NewWithT(t)

	dir := t.TempDir()
	paramsFile := filepath.Join(dir, "params.env")
	g.Expect(os.WriteFile(paramsFile, []byte("my-image=original\n"), 0o600)).Should(Succeed())

	t.Setenv("TEST_APPLY_PARAMS_EMPTY_ENV_VAR", "")

	err := deploy.ApplyParams(dir, "params.env", map[string]string{"my-image": "TEST_APPLY_PARAMS_EMPTY_ENV_VAR"})
	g.Expect(err).ShouldNot(HaveOccurred())

	content, err := os.ReadFile(paramsFile)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(string(content)).Should(Equal("my-image=original\n"))
}
