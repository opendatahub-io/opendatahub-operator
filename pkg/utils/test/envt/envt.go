package envt

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"k8s.io/apimachinery/pkg/runtime"
	ctrlCli "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
)

type OptionFn func(in *EnvT)

func WithScheme(value *runtime.Scheme) OptionFn {
	return func(in *EnvT) {
		in.s = value
	}
}

func WithProjectRoot(value string, elem ...string) OptionFn {
	return func(in *EnvT) {
		r := value
		if l := len(elem); l != 0 {
			i := make([]string, 0, l+1)
			i = append(i, value)
			i = append(i, elem...)

			r = path.Join(i...)
		}

		in.root = r
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

	envTestClient, err := ctrlCli.New(cfg, ctrlCli.Options{Scheme: result.s})
	if err != nil {
		return nil, fmt.Errorf("unable to creaste envtest client: %w", err)
	}

	cli, err := client.NewFromConfig(cfg, envTestClient)
	if err != nil {
		return nil, fmt.Errorf("unable to creaste client: %w", err)
	}

	result.cli = cli

	return &result, nil
}

type EnvT struct {
	root string
	s    *runtime.Scheme
	e    envtest.Environment
	cli  *client.Client
}

func (et *EnvT) Scheme() *runtime.Scheme {
	return et.s
}

func (et *EnvT) Client() *client.Client {
	return et.cli
}

func (et *EnvT) Stop() error {
	return et.e.Stop()
}

func (et *EnvT) ProjectRoot() string {
	return et.root
}

func (et *EnvT) ReadFile(value string, elem ...string) ([]byte, error) {
	r := value
	if l := len(elem); l != 0 {
		i := make([]string, 0, l+1)
		i = append(i, value)
		i = append(i, elem...)

		r = path.Join(i...)
	}

	fp := filepath.Join(et.root, r)

	content, err := os.ReadFile(fp)
	if err != nil {
		return nil, fmt.Errorf("unable to read file %s: %w", fp, err)
	}

	return content, nil
}
