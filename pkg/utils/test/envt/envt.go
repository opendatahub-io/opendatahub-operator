package envt

import (
	"crypto/tls"
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
	"sigs.k8s.io/controller-runtime/pkg/manager"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
)

type OptionFn func(in *EnvT)

// RegisterWebhooksFn is a function that registers webhooks with a manager.
type RegisterWebhooksFn func(manager.Manager) error

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

// WithManager enables creation of a controller-runtime manager in the test environment.
// Optionally accepts a manager.Options struct for custom configuration.
func WithManager(opts ...manager.Options) OptionFn {
	return func(in *EnvT) {
		in.withManager = true
		if len(opts) > 0 {
			in.managerOpts = &opts[0]
		}
	}
}

// WithRegisterWebhooks registers webhook setup functions to be called on the manager.
func WithRegisterWebhooks(funcs ...RegisterWebhooksFn) OptionFn {
	return func(in *EnvT) {
		in.registerWebhooks = append([]RegisterWebhooksFn{}, funcs...)
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

	result.Env = envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme: result.s,
			Paths: []string{
				filepath.Join(result.root, "config", "crd", "bases"),
			},
			ErrorIfPathMissing: true,
			CleanUpAfterUse:    false,
		},
	}

	// If webhooks are registered, configure the webhook server
	if len(result.registerWebhooks) > 0 {
		result.Env.WebhookInstallOptions = envtest.WebhookInstallOptions{
			Paths: []string{
				filepath.Join(result.root, "config", "webhook"),
			},
		}
	}

	// Start the envtest environment.
	cfg, err := result.Env.Start()
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

	// Create the manager if requested or if webhooks are registered
	needManager := result.withManager || len(result.registerWebhooks) > 0
	if needManager {
		// Prepare manager options, using any custom options provided by the user.
		mgrOpts := manager.Options{}
		if result.managerOpts != nil {
			mgrOpts = *result.managerOpts
		}

		// Ensure the manager uses the correct scheme for all registered types.
		if mgrOpts.Scheme == nil {
			mgrOpts.Scheme = result.s
		}

		// After envtest is started, retrieve the webhook server options (host, port, cert dir)
		// that were dynamically allocated. These must be used to configure the manager's webhook server.
		webhookInstallOptions := &result.Env.WebhookInstallOptions

		// If the manager's WebhookServer is not already set, create one using the
		// host, port, and cert dir from envtest. This ensures the webhook server
		// is reachable by the test client and uses the correct certificates.
		if mgrOpts.WebhookServer == nil {
			mgrOpts.WebhookServer = ctrlwebhook.NewServer(ctrlwebhook.Options{
				Port:    webhookInstallOptions.LocalServingPort,
				Host:    webhookInstallOptions.LocalServingHost,
				CertDir: webhookInstallOptions.LocalServingCertDir,
				TLSOpts: []func(*tls.Config){func(config *tls.Config) { config.MinVersion = tls.VersionTLS12 }},
			})
		}

		// Disable the metrics endpoint for tests unless explicitly set, to avoid port conflicts
		// and unnecessary metrics serving during test runs.
		if mgrOpts.Metrics.BindAddress == "" {
			mgrOpts.Metrics.BindAddress = "0"
			mgrOpts.Metrics.CertDir = webhookInstallOptions.LocalServingCertDir
		}

		// Now create the controller-runtime manager with the correct options.
		// This manager will use the webhook server configured above, ensuring that
		// webhooks are reachable and use the correct certificates for the test environment.
		mgr, err := manager.New(result.cfg, mgrOpts)
		if err != nil {
			return nil, fmt.Errorf("failed to create manager: %w", err)
		}
		result.mgr = mgr
		for _, reg := range result.registerWebhooks {
			if err := reg(mgr); err != nil {
				return nil, fmt.Errorf("failed to register webhooks: %w", err)
			}
		}
	}

	return &result, nil
}

type EnvT struct {
	root             string
	withManager      bool
	managerOpts      *manager.Options
	registerWebhooks []RegisterWebhooksFn
	s                *runtime.Scheme
	Env              envtest.Environment
	cfg              *rest.Config
	cli              client.Client
	discoveryClient  discovery.DiscoveryInterface
	dynamicClient    dynamic.Interface
	mgr              manager.Manager
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
	return et.Env.Stop()
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

//nolint:ireturn
func (et *EnvT) Manager() manager.Manager {
	return et.mgr
}
