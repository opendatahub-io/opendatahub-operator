package dsc

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"testing"
	"time"

	"github.com/onsi/gomega"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
)

func (m KserveSuite) run(t *testing.T) {
	t.Run("v2_v3_drops_wva", m.v2ToV3DropsWVA)
	t.Run("v3_v2_projects_wva_removed", m.v3ToV2ProjectsWVAsRemoved)
	t.Run("v2_admission_rejects_wva_managed", m.v2AdmissionRejectsWVAManaged)
	t.Run("v3_rejects_invalid_luks_resource_id", m.v3RejectsInvalidLUKSResourceID)
	t.Run("v3_rejects_invalid_audit_profile", m.v3RejectsInvalidAuditProfile)
	t.Run("v3_v2_round_trip_preserves_confidential_containers", m.v3V2RoundTripPreservesConfidentialContainers)
	t.Run("v3_v2_round_trip_preserves_audit_profile", m.v3V2RoundTripPreservesAuditProfile)
}

// v3RejectsInvalidLUKSResourceID verifies the CRD schema rejects a
// confidentialContainers.luksResourceId that is not a kbs:/// URI.
func (m KserveSuite) v3RejectsInvalidLUKSResourceID(t *testing.T) {
	w := m.newScenario(t)
	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Kserve: componentApi.DSCKserve{
					KserveCommonSpec: componentApi.KserveCommonSpec{
						ConfidentialContainers: &componentApi.ConfidentialContainersConfig{
							LUKSResourceID: "not-a-kbs-uri",
						},
					},
				},
			},
		},
	}).Should(
		gomega.MatchError(gomega.ContainSubstring("luksResourceId")),
	)
}

// v3RejectsInvalidAuditProfile verifies the CRD schema rejects an
// auditLoggingProfile outside the Removed/Metadata enum.
func (m KserveSuite) v3RejectsInvalidAuditProfile(t *testing.T) {
	w := m.newScenario(t)
	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Kserve: componentApi.DSCKserve{
					KserveCommonSpec: componentApi.KserveCommonSpec{
						AuditLoggingProfile: componentApi.AuditProfile("Verbose"),
					},
				},
			},
		},
	}).Should(
		gomega.MatchError(gomega.ContainSubstring("auditLoggingProfile")),
	)
}

func (m KserveSuite) v2AdmissionRejectsWVAManaged(t *testing.T) {
	w := m.newScenario(t)
	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						WVA: componentApi.WVASpec{ManagementState: operatorv1.Managed},
					},
				},
			},
		},
	}).Should(
		gomega.MatchError(gomega.ContainSubstring("wva")),
	)
}

func (m KserveSuite) v2ToV3DropsWVA(t *testing.T) {
	w := m.newScenario(t)
	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserveV2{
					KserveCommonSpecV2: componentApi.KserveCommonSpecV2{
						WVA: componentApi.WVASpec{ManagementState: operatorv1.Removed},
					},
				},
			},
		},
	}).Should(gomega.Succeed())

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(gomega.And(
		jq.Match(`(.spec.components.kserve | has("wva")) == false`),
	))
	w.Get(gvk.DataScienceClusterV2, dscKey).Should(
		jq.Match(`.spec.components.kserve.wva.managementState == "Removed"`),
	)
}

func (m KserveSuite) v3ToV2ProjectsWVAsRemoved(t *testing.T) {
	w := m.newScenario(t)
	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Removed},
				},
			},
		},
	}).Should(gomega.Succeed())

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(
		jq.Match(`.spec.components.kserve.wva.managementState == "Removed"`),
	)
	w.Get(gvk.DataScienceClusterV3, dscKey).Should(gomega.And(
		jq.Match(`(.spec.components.kserve | has("wva")) == false`),
	))
}

// v3V2RoundTripPreservesConfidentialContainers verifies that the v3-only
// confidentialContainers settings survive a v2 read-modify-write of the DSC,
// carried by the conversion-owned kserve-v3-state annotation.
func (m KserveSuite) v3V2RoundTripPreservesConfidentialContainers(t *testing.T) {
	w := m.newScenario(t)

	luksResourceID := "kbs:///conversion/luks-password/key"
	initData := gzipBase64(t, "[workload]\nworkload_path = \"/bin/trustme\"\n")

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Removed},
					KserveCommonSpec: componentApi.KserveCommonSpec{
						ConfidentialContainers: &componentApi.ConfidentialContainersConfig{
							LUKSResourceID: luksResourceID,
							InitData:       initData,
						},
					},
				},
			},
		},
	}).Should(gomega.Succeed())

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(gomega.And(
		jq.Match(`(.spec.components.kserve | has("confidentialContainers")) == false`),
		jq.Match(`.metadata.annotations["%s"] | fromjson | .confidentialContainers.luksResourceId == "%s"`,
			dscv2.KserveV3StateAnnotation, luksResourceID),
		jq.Match(`.metadata.annotations["%s"] | fromjson | .confidentialContainers.initData == "%s"`,
			dscv2.KserveV3StateAnnotation, initData),
	))

	m.updateUnrelatedFieldThroughV2(t, w)

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(gomega.And(
		jq.Match(`.spec.components.kserve.confidentialContainers.luksResourceId == "%s"`, luksResourceID),
		jq.Match(`.spec.components.kserve.confidentialContainers.initData == "%s"`, initData),
		jq.Match(`.spec.components.kserve.rawDeploymentServiceConfig == "Headed"`),
		jq.Match(`.metadata.annotations["%s"] == null`, dscv2.KserveV3StateAnnotation),
	))
}

// v3V2RoundTripPreservesAuditProfile verifies that the v3-only
// auditLoggingProfile survives a v2 read-modify-write of the DSC, carried by
// the conversion-owned kserve-v3-state annotation.
func (m KserveSuite) v3V2RoundTripPreservesAuditProfile(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Removed},
					KserveCommonSpec: componentApi.KserveCommonSpec{
						AuditLoggingProfile: componentApi.AuditProfileMetadata,
					},
				},
			},
		},
	}).Should(gomega.Succeed())

	w.Get(gvk.DataScienceClusterV2, dscKey).Should(gomega.And(
		jq.Match(`(.spec.components.kserve | has("auditLoggingProfile")) == false`),
		jq.Match(`.metadata.annotations["%s"] | fromjson | .auditLoggingProfile == "Metadata"`,
			dscv2.KserveV3StateAnnotation),
	))

	m.updateUnrelatedFieldThroughV2(t, w)

	w.Get(gvk.DataScienceClusterV3, dscKey).Should(gomega.And(
		jq.Match(`.spec.components.kserve.auditLoggingProfile == "Metadata"`),
		jq.Match(`.spec.components.kserve.rawDeploymentServiceConfig == "Headed"`),
		jq.Match(`.metadata.annotations["%s"] == null`, dscv2.KserveV3StateAnnotation),
	))
}

// updateUnrelatedFieldThroughV2 simulates a v2 client read-modify-write that
// changes an unrelated KServe field while the conversion marker rides along.
func (m KserveSuite) updateUnrelatedFieldThroughV2(t *testing.T, w *testf.WithT) {
	t.Helper()

	w.Eventually(func() error {
		updated := &dscv2.DataScienceCluster{}
		if err := w.Client().Get(t.Context(), dscKey, updated); err != nil {
			return err
		}
		updated.Spec.Components.Kserve.RawDeploymentServiceConfig = componentApi.KserveRawHeaded
		return w.Client().Update(t.Context(), updated)
	}).
		WithTimeout(2 * time.Minute).
		WithPolling(time.Second).
		Should(gomega.Succeed())
}

// gzipBase64 builds a valid confidentialContainers.initData payload: a TOML
// document compressed with gzip and then base64-encoded.
func gzipBase64(t *testing.T, toml string) string {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(toml)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}
