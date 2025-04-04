package testf

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcfg "sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
)

const (
	DefaultPollInterval = 1 * time.Second
	DefaultTimeout      = 2 * time.Minute
)

type testContextOpts struct {
	ctx       context.Context
	cfg       *rest.Config
	client    client.Client
	scheme    *runtime.Scheme
	withTOpts []WithTOpts
}

type TestContextOpt func(testContext *testContextOpts)

func WithClient(value client.Client) TestContextOpt {
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
		s, err := scheme.New()
		if err != nil {
			return nil, errors.New("unable to create default scheme")
		}

		tc.scheme = s
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

		ctrlCli, err := client.New(clientCfg, client.Options{Scheme: tc.scheme})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize custom client: %w", err)
		}

		tc.client = ctrlCli
	}

	return &tc, nil
}

type TestContext struct {
	ctx    context.Context
	client client.Client
	scheme *runtime.Scheme

	withTOpts []WithTOpts
}

func (tc *TestContext) Context() context.Context {
	return tc.ctx
}

func (tc *TestContext) Client() client.Client {
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
