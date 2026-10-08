package dsc

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega" //nolint:staticcheck // Matchers use Gomega's test DSL.
)

// These are fresh-install retirement cases. They do not depend on the old
// TrainingOperator or LlamaStackOperator module being bundled in the image.
func (m WebhookSuite) runRetiredOperator(
	t *testing.T,
	field string,
	setManagementState func(*dscv2.Components, operatorv1.ManagementState),
) {
	t.Helper()

	for _, testCase := range []struct {
		name  string
		state operatorv1.ManagementState
	}{
		{name: "explicit_removed", state: operatorv1.Removed},
		{name: "default"},
	} {
		t.Run("v2_v3/"+testCase.name, func(t *testing.T) {
			w := m.newScenario(t)
			object := &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name}}
			if testCase.state != "" {
				setManagementState(&object.Spec.Components, testCase.state)
			}
			w.Apply(object).Should(Succeed())

			w.Get(gvk.DataScienceClusterV3, dscKey).Should(
				jq.Match(`(.spec.components // {}) | has("%s") | not`, field),
				"v3 object must not contain spec.components.%s", field,
			)
			w.Get(gvk.DataScienceClusterV3, dscKey).Should(
				jq.Match(`(.status.components // {}) | has("%s") | not`, field),
				"v3 object must not contain status.components.%s", field,
			)
			w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
				jq.Match(`.spec.components.%s.managementState == "%s"`, field, operatorv1.Removed),
				jq.Match(`.status.components.%s.managementState == "%s"`, field, operatorv1.Removed),
			))
		})
	}

	t.Run("v3_v2", func(t *testing.T) {
		w := m.newScenario(t)
		w.Apply(&dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name}}).Should(Succeed())

		w.Get(gvk.DataScienceClusterV3, dscKey).Should(
			jq.Match(`(.spec.components // {}) | has("%s") | not`, field),
			"v3 object must not contain spec.components.%s", field,
		)
		w.Get(gvk.DataScienceClusterV3, dscKey).Should(
			jq.Match(`(.status.components // {}) | has("%s") | not`, field),
			"v3 object must not contain status.components.%s", field,
		)
		w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
			jq.Match(`.spec.components.%s.managementState == "%s"`, field, operatorv1.Removed),
			jq.Match(`.status.components.%s.managementState == "%s"`, field, operatorv1.Removed),
		))
	})

	t.Run("reject_managed_create", func(t *testing.T) {
		w := m.newScenario(t)
		g := NewWithT(t)
		object := &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name}}
		setManagementState(&object.Spec.Components, operatorv1.Managed)
		err := w.Apply(object).Get()
		g.Expect(k8serr.IsForbidden(err)).To(BeTrue(), "a new v2 %s=Managed request must be rejected: %v", field, err)
	})

	t.Run("reject_reenable_but_allow_unrelated_update", func(t *testing.T) {
		w := m.newScenario(t)
		g := NewWithT(t)
		removed := &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name}}
		setManagementState(&removed.Spec.Components, operatorv1.Removed)
		w.Apply(removed).Should(Succeed())

		managed := &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name}}
		setManagementState(&managed.Spec.Components, operatorv1.Managed)
		err := w.Apply(managed).Get()

		g.Expect(k8serr.IsForbidden(err)).To(BeTrue(), "v2 %s Removed-to-Managed must be rejected: %v", field, err)

		w.Apply(&dscv2.DataScienceCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:   dscKey.Name,
				Labels: map[string]string{"unrelated-update": "allowed"},
			},
		}).Should(Succeed())

		w.Get(gvk.DataScienceClusterV2, dscKey).Should(
			matchers.HaveLabel("unrelated-update", "allowed"),
		)
		w.Get(gvk.DataScienceClusterV3, dscKey).Should(
			jq.Match(`(.spec.components // {}) | has("%s") | not`, field),
			"v3 object must not contain spec.components.%s", field,
		)
		w.Get(gvk.DataScienceClusterV3, dscKey).Should(
			jq.Match(`(.status.components // {}) | has("%s") | not`, field),
			"v3 object must not contain status.components.%s", field,
		)
		w.Get(gvk.DataScienceClusterV2, dscKey).Should(And(
			jq.Match(`.spec.components.%s.managementState == "%s"`, field, operatorv1.Removed),
			jq.Match(`.status.components.%s.managementState == "%s"`, field, operatorv1.Removed),
		))
	})
}
