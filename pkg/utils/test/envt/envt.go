package envt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
)

type OptionFn func(in *EnvT)

func WithScheme(value *runtime.Scheme) OptionFn {
	return func(in *EnvT) {
		in.s = value
	}
}

func WithProjectRoot(elem ...string) OptionFn {
	return func(in *EnvT) {
		in.root = filepath.Join(elem...)
	}
}

func New(opts ...OptionFn) (*EnvT, error) {
	result := EnvT{}

	for _, opt := range opts {
		opt(&result)
	}

	if result.s == nil {
		s, err := scheme.New()
		if err != nil {
			return nil, errors.New("unable to create default scheme")
		}

		result.s = s
	}

	if result.root == "" {
		root, err := envtestutil.FindProjectRoot()
		if err != nil {
			return nil, fmt.Errorf("unable to determine project root: %w", err)
		}

		result.root = root
	}

	result.e = envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme: result.s,
			Paths: []string{
				filepath.Join(result.root, "config", "crd", "bases"),
			},
			ErrorIfPathMissing: true,
			CleanUpAfterUse:    false,
		},
	}

	cfg, err := result.e.Start()
	if err != nil {
		return nil, fmt.Errorf("unable to start envtest: %w", err)
	}

	envTestClient, err := client.New(cfg, client.Options{Scheme: result.s})
	if err != nil {
		return nil, fmt.Errorf("unable to create envtest client: %w", err)
	}
	discoveryCli, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to construct a Discovery client: %w", err)
	}
	dynamicCli, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to construct a Dynamic client: %w", err)
	}

	result.cfg = cfg
	result.cli = envTestClient
	result.discoveryClient = discoveryCli
	result.dynamicClient = dynamicCli

	return &result, nil
}

type EnvT struct {
	root            string
	s               *runtime.Scheme
	e               envtest.Environment
	cfg             *rest.Config
	cli             client.Client
	discoveryClient discovery.DiscoveryInterface
	dynamicClient   dynamic.Interface
}

func (et *EnvT) Scheme() *runtime.Scheme {
	return et.s
}

func (et *EnvT) Config() *rest.Config {
	return et.cfg
}

func (et *EnvT) Client() client.Client {
	return et.cli
}

func (et *EnvT) DiscoveryClient() discovery.DiscoveryInterface {
	return et.discoveryClient
}

func (et *EnvT) DynamicClient() dynamic.Interface {
	return et.dynamicClient
}

func (et *EnvT) Stop() error {
	return et.e.Stop()
}

func (et *EnvT) ProjectRoot() string {
	return et.root
}

func (et *EnvT) ReadFile(elem ...string) ([]byte, error) {
	fp := filepath.Join(et.root, filepath.Join(elem...))

	content, err := os.ReadFile(fp)
	if err != nil {
		return nil, fmt.Errorf("unable to read file %s: %w", fp, err)
	}

	return content, nil
}
