package gates_test

import (
	"strings"
	"testing"

	controllergates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	upgradegates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates"

	. "github.com/onsi/gomega"
)

func TestRegisteredChecksCoverInTreeGateKeys(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	registeredChecks := upgradegates.Registered()
	gateEntries, err := controllergates.LoadInTreeGates(provision.UpgradeGateVersion)
	g.Expect(err).ToNot(HaveOccurred())

	versionPrefix := "ack-" + provision.UpgradeGateVersion + "-"

	for key := range gateEntries {
		component := strings.TrimPrefix(key, versionPrefix)
		g.Expect(component).ToNot(Equal(key), "expected gate key %q to start with %q", key, versionPrefix)
		g.Expect(registeredChecks).To(HaveKey(component), "every in-tree gate key must have an explicit registered check")
	}
}
