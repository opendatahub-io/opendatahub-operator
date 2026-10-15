package data_test

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/data"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"

	. "github.com/onsi/gomega"
)

// crdPath is the module CRD's location within its local downloaded chart
// assets (see make get-manifests).
const crdPath = "templates/apiextensions.k8s.io_v1_customresourcedefinition.yaml"

// dataSuite drives the FeastOperator projection cases through the shared
// runner; each case is one method registered as a t.Run. BuildModuleCR
// ignores the DSC and config arguments and resolves OIDC from the platform
// client, so each case builds its fake client — carrying exactly the cluster
// authentication and GatewayConfig objects under test — explicitly in setup.
// The CRD requires spec.oidc.issuerURL (https URI pattern) when oidc is set.
type dataSuite struct {
	*projection.Runner
}

func (s dataSuite) noAuthenticationResource(t *testing.T) {
	client := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()

	s.Validate(t, projection.ModuleCRSpec{
		Client: client,
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.oidc == null`),
	)))
}

func (s dataSuite) nonOIDCAuthentication(t *testing.T) {
	client := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(
		&configv1.Authentication{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       configv1.AuthenticationSpec{Type: configv1.AuthenticationTypeIntegratedOAuth},
		},
	).Build()

	s.Validate(t, projection.ModuleCRSpec{
		Client: client,
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.oidc == null`),
	)))
}

func (s dataSuite) oidcWithoutGatewayConfig(t *testing.T) {
	client := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(
		&configv1.Authentication{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       configv1.AuthenticationSpec{Type: configv1.AuthenticationTypeOIDC},
		},
	).Build()

	s.Validate(t, projection.ModuleCRSpec{
		Client: client,
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.oidc == null`),
	)))
}

func (s dataSuite) oidcWithUnsetIssuer(t *testing.T) {
	client := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(
		&configv1.Authentication{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       configv1.AuthenticationSpec{Type: configv1.AuthenticationTypeOIDC},
		},
		&serviceApi.GatewayConfig{
			ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
			Spec: serviceApi.GatewayConfigSpec{
				OIDC: &serviceApi.OIDCConfig{IssuerURL: ""},
			},
		},
	).Build()

	s.Validate(t, projection.ModuleCRSpec{
		Client: client,
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.oidc == null`),
	)))
}

func (s dataSuite) oidcWithValidHTTPSIssuer(t *testing.T) {
	client := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(
		&configv1.Authentication{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       configv1.AuthenticationSpec{Type: configv1.AuthenticationTypeOIDC},
		},
		&serviceApi.GatewayConfig{
			ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
			Spec: serviceApi.GatewayConfigSpec{
				OIDC: &serviceApi.OIDCConfig{IssuerURL: "https://keycloak.example.com/realms/odh"},
			},
		},
	).Build()

	s.Validate(t, projection.ModuleCRSpec{
		Client: client,
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.oidc.issuerURL == "https://keycloak.example.com/realms/odh"`),
	)))
}

// TestBuildModuleCRProjection validates FeastOperator projections against the
// real chart CRD from the local downloaded assets.
func TestBuildModuleCRProjection(t *testing.T) {
	h := data.NewHandler()
	s := dataSuite{
		Runner: projection.NewRunner(t, h, crdPath),
	}

	t.Run("no authentication resource", s.noAuthenticationResource)
	t.Run("non-oidc authentication", s.nonOIDCAuthentication)
	t.Run("oidc without gateway config", s.oidcWithoutGatewayConfig)
	t.Run("oidc with unset issuer", s.oidcWithUnsetIssuer)
	t.Run("oidc with valid https issuer", s.oidcWithValidHTTPSIssuer)
}
