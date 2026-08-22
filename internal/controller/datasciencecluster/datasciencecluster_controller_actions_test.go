//nolint:testpackage
package datasciencecluster

import (
	"errors"
	"testing"

	"github.com/go-logr/logr/funcr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

func TestLogErrorsFormatting(t *testing.T) {
	g := NewWithT(t)
	dsc := newDSC()

	cr.DefaultRegistry().Add(&mockHandler{
		name:     "log-kv-test",
		enabled:  true,
		newCRErr: errors.New("test error"),
	})
	t.Cleanup(func() { cr.DefaultRegistry().Disable("log-kv-test") })

	var logged []string
	logger := funcr.New(func(prefix, args string) {
		logged = append(logged, args)
	}, funcr.Options{})
	ctx := logf.IntoContext(t.Context(), logger)

	rr := &types.ReconciliationRequest{
		Instance:   dsc,
		Conditions: conditions.NewManager(dsc, status.ConditionTypeComponentsReady),
	}

	err := provisionComponents(ctx, rr)

	g.Expect(err).To(HaveOccurred())
	g.Expect(logged).To(ContainElement(And(
		ContainSubstring(`"msg"="NewCRObject failed"`),
		ContainSubstring(`"name"="test-dsc"`),
		ContainSubstring(`"resourceKind"="DataScienceCluster"`),
		ContainSubstring(`"component"="log-kv-test"`),
		Not(ContainSubstring(`"namespace"`)),
	)))
}
