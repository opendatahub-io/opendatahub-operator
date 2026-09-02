package gates_test

import (
	"testing"

	controllergates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	upgradegates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates"

	. "github.com/onsi/gomega"
)

func TestRegisteredChecksCoverInTreeGateKeys(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	registeredChecks := upgradegates.Registered()
	gateEntries, err := controllergates.LoadInTreeGates()
	g.Expect(err).ToNot(HaveOccurred())

	for key := range gateEntries {
		component, matches := controllergates.MatchGateKey(key, "3.5.0")
		g.Expect(matches).To(BeTrue(), "expected gate key %q to match the 3.5 gate scope", key)
		g.Expect(registeredChecks).To(HaveKey(component), "every in-tree gate key must have an explicit registered check")
	}
}
