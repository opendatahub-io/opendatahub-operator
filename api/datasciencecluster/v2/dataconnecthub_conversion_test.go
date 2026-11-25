package v2_test

import (
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
)

func TestDataConnectHubRoundTrip(t *testing.T) {
	for _, state := range []operatorv1.ManagementState{"", operatorv1.Managed, operatorv1.Removed} {
		t.Run(string(state), func(t *testing.T) {
			src := &dscv3.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"example.com/user": "retained"}}}
			src.Spec.Components.Data.DataConnectHub = componentApi.DSCDataConnectHub{
				Namespace:            "custom-dch-infra",
				ManagementSpec:       common.ManagementSpec{ManagementState: state},
				Gateway:              &componentApi.DataConnectHubGateway{Name: "gateway", Namespace: "ingress"},
				TokenReviewAudiences: []string{"https://issuer.example"},
			}
			before := src.DeepCopy()
			var old dscv2.DataScienceCluster
			require.NoError(t, old.ConvertFrom(src))
			require.Contains(t, old.Annotations, dscv2.DataConnectHubV3StateAnnotation)
			var restored dscv3.DataScienceCluster
			require.NoError(t, old.ConvertTo(&restored))
			require.Equal(t, src.Spec.Components.Data.DataConnectHub, restored.Spec.Components.Data.DataConnectHub)
			require.Equal(t, before, src, "conversion must not mutate its source")
			require.Equal(t, "retained", restored.Annotations["example.com/user"])
			require.NotContains(t, restored.Annotations, dscv2.DataConnectHubV3StateAnnotation)
		})
	}
}

func TestDataConnectHubInvalidConversionMarkers(t *testing.T) {
	for _, value := range []string{
		`{}`, `null`, `broken`, `{"managementState":"Unmanaged"}`,
		`{"managementState":"Managed","gateway":{"name":"gateway"}}`,
		`{"managementState":"Managed","gateway":{"name":" ","namespace":"ingress"}}`,
		`{"managementState":"Managed","gateway":{"name":"gateway","namespace":"\t\n"}}`,
		`{"managementState":"Managed","gateway":{"name":"\u00a0","namespace":"ingress"}}`,
		`{"managementState":"Managed","tokenReviewAudiences":[""]}`,
		`{"managementState":"Managed","tokenReviewAudiences":[" "]}`,
		`{"managementState":"Managed","tokenReviewAudiences":["\u3000"]}`,
		`{"managementState":"Managed","tokenReviewAudiences":["same","same"]}`,
		`{"managementState":"Managed","tokenReviewAudiences":["` + strings.Repeat("a", 257) + `"]}`,
	} {
		t.Run(value, func(t *testing.T) {
			src := &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				dscv2.DataConnectHubV3StateAnnotation: value,
			}}}
			var dst dscv3.DataScienceCluster
			require.Error(t, src.ConvertTo(&dst))
		})
	}
}

func TestDataConnectHubOmissionClearsStaleMarker(t *testing.T) {
	src := &dscv3.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		dscv2.DataConnectHubV3StateAnnotation: `{"managementState":"Managed"}`,
	}}}
	var old dscv2.DataScienceCluster
	require.NoError(t, old.ConvertFrom(src))
	require.NotContains(t, old.Annotations, dscv2.DataConnectHubV3StateAnnotation)
	var restored dscv3.DataScienceCluster
	require.NoError(t, old.ConvertTo(&restored))
	require.Empty(t, restored.Spec.Components.Data.DataConnectHub.ManagementState)
}
