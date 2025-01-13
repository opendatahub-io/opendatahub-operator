package testf

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/onsi/gomega"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrlcli "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcfg "sigs.k8s.io/controller-runtime/pkg/client/config"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	odhcli "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
)

const (
	DefaultPollInterval = 1 * time.Second
	DefaultTimeout      = 2 * time.Minute
)

var (
	DefaultAddToSchemes = []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme,
		routev1.AddToScheme,
		apiextv1.AddToScheme,
		dsciv1.AddToScheme,
		dscv1.AddToScheme,
		featurev1.AddToScheme,
		monitoringv1.AddToScheme,
		ofapi.AddToScheme,
		operatorv1.AddToScheme,
		componentApi.AddToScheme,
	}
)

type testContextOpts struct {
	ctx       context.Context
	cfg       *rest.Config
	client    *odhcli.Client
	scheme    *runtime.Scheme
	withTOpts []WithTOpts
}

type TestContextOpt func(testContext *testContextOpts)

func WithClient(value *odhcli.Client) TestContextOpt {
	return func(tc *testContextOpts) {
		tc.client = value
	}
}

func WithRestConfig(value *rest.Config) TestContextOpt {
	return func(tc *testContextOpts) {
		tc.cfg = value
	}
}

func WithScheme(value *runtime.Scheme) TestContextOpt {
	return func(tc *testContextOpts) {
		tc.scheme = value
	}
}

//nolint:fatcontext
func WitContext(value context.Context) TestContextOpt {
	return func(tc *testContextOpts) {
		tc.ctx = value
	}
}

func WithTOptions(opts ...WithTOpts) TestContextOpt {
	return func(tc *testContextOpts) {
		tc.withTOpts = append(tc.withTOpts, opts...)
	}
}

func NewTestContext(opts ...TestContextOpt) (*TestContext, error) {
	tco := testContextOpts{}
	for _, opt := range opts {
		opt(&tco)
	}

	tc := TestContext{
		ctx:       tco.ctx,
		scheme:    tco.scheme,
		client:    tco.client,
		withTOpts: tco.withTOpts,
	}

	if tc.ctx == nil {
		tc.ctx = context.Background()
	}

	if tc.scheme == nil {
		tc.scheme = runtime.NewScheme()
		for _, at := range DefaultAddToSchemes {
			if err := at(tc.scheme); err != nil {
				return nil, err
			}
		}
	}

	if tc.client == nil {
		clientCfg := tco.cfg
		if clientCfg == nil {
			cfg, err := ctrlcfg.GetConfig()
			if err != nil {
				return nil, fmt.Errorf("error creating the config object %w", err)
			}

			clientCfg = cfg
		}

		ctrlCli, err := ctrlcli.New(clientCfg, ctrlcli.Options{Scheme: tc.scheme})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize custom client: %w", err)
		}

		odhCli, err := odhcli.NewFromConfig(clientCfg, ctrlCli)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize odh client: %w", err)
		}

		tc.client = odhCli
	}

	return &tc, nil
}

type TestContext struct {
	ctx    context.Context
	client *odhcli.Client
	scheme *runtime.Scheme

	withTOpts []WithTOpts
}

func (tc *TestContext) Context() context.Context {
	return tc.ctx
}

func (tc *TestContext) Client() *odhcli.Client {
	return tc.client
}

func (tc *TestContext) Scheme() *runtime.Scheme {
	return tc.client.Scheme()
}

func (tc *TestContext) NewWithT(t *testing.T, opts ...WithTOpts) *WithT {
	t.Helper()

	g := gomega.NewWithT(t)
	g.SetDefaultEventuallyTimeout(DefaultTimeout)
	g.SetDefaultEventuallyPollingInterval(DefaultPollInterval)
	g.SetDefaultConsistentlyDuration(DefaultTimeout)
	g.SetDefaultConsistentlyPollingInterval(DefaultPollInterval)

	answer := WithT{
		ctx:    tc.ctx,
		client: tc.client,
		WithT:  g,
	}

	for _, opt := range tc.withTOpts {
		opt(&answer)
	}

	for _, opt := range opts {
		opt(&answer)
	}

	return &answer
}
