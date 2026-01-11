package envt

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
)

type OptionFn func(in *EnvT)

// RegisterWebhooksFn is a function that registers webhooks with a manager.
type RegisterWebhooksFn func(manager.Manager) error

// RegisterControllersFn is a function that registers controllers with a manager.
type RegisterControllersFn func(manager.Manager) error

// createManager sets up and configures the controller-runtime manager.
func (et *EnvT) createManager() error {
	// Prepare manager options, using any custom options provided by the user.
	mgrOpts := manager.Options{}
	if et.managerOpts != nil {
		mgrOpts = *et.managerOpts
	}

	// Ensure the manager uses the correct scheme for all registered types.
	if mgrOpts.Scheme == nil {
		mgrOpts.Scheme = et.s
	}

	// After envtest is started, retrieve the webhook server options (host, port, cert dir)
	// that were dynamically allocated. These must be used to configure the manager's webhook server.
	webhookInstallOptions := &et.Env.WebhookInstallOptions

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
	mgr, err := manager.New(et.cfg, mgrOpts)
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}
	et.mgr = mgr
	for _, reg := range et.registerWebhooks {
		if err := reg(mgr); err != nil {
			return fmt.Errorf("failed to register webhooks: %w", err)
		}
	}
	// Register controllers
	for _, reg := range et.registerControllers {
		if err := reg(mgr); err != nil {
			return fmt.Errorf("failed to register controllers: %w", err)
		}
	}

	return nil
}

// WithScheme sets a custom runtime.Scheme for the test environment.
// Use this to register additional types or override the default scheme.
func WithScheme(value *runtime.Scheme) OptionFn {
	return func(in *EnvT) {
		in.s = value
	}
}

// WithProjectRoot sets the project root directory for the test environment.
// Useful for customizing where CRDs and webhook configs are loaded from.
func WithProjectRoot(elem ...string) OptionFn {
	return func(in *EnvT) {
		in.root = filepath.Join(elem...)
	}
}

// WithManager enables creation of a controller-runtime manager in the test environment.
// Optionally accepts a manager.Options struct for custom configuration.
// If not provided, sensible defaults are used for testing.
func WithManager(opts ...manager.Options) OptionFn {
	return func(in *EnvT) {
		in.withManager = true
		if len(opts) > 0 {
			in.managerOpts = &opts[0]
		}
	}
}

// WithRegisterWebhooks registers one or more webhook setup functions to be called on the manager.
// Each function should register webhooks with the provided manager.
func WithRegisterWebhooks(funcs ...RegisterWebhooksFn) OptionFn {
	return func(in *EnvT) {
		in.registerWebhooks = append(in.registerWebhooks, funcs...)
	}
}

// WithRegisterControllers registers one or more controllers setup functions to be called on the manager.
// Each function should register controllers with the provided manager.
func WithRegisterControllers(funcs ...RegisterControllersFn) OptionFn {
	return func(in *EnvT) {
		in.registerControllers = append(in.registerControllers, funcs...)
	}
}

// New creates and configures a new EnvT test environment.
// Applies all provided OptionFn options, sets up CRDs, webhooks, and the envtest environment.
// Returns the configured EnvT, or an error if setup fails.
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
		ErrorIfCRDPathMissing: true,
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
	needManager := result.withManager || len(result.registerWebhooks) > 0 || len(result.registerControllers) > 0
	if needManager {
		if err := result.createManager(); err != nil {
			return nil, err
		}
	}

	return &result, nil
}

type EnvT struct {
	root                string
	withManager         bool
	managerOpts         *manager.Options
	registerWebhooks    []RegisterWebhooksFn
	registerControllers []RegisterControllersFn
	s                   *runtime.Scheme
	Env                 envtest.Environment
	cfg                 *rest.Config
	cli                 client.Client
	discoveryClient     discovery.DiscoveryInterface
	dynamicClient       dynamic.Interface
	mgr                 manager.Manager
}

// Scheme returns the runtime.Scheme used by the test environment.
func (et *EnvT) Scheme() *runtime.Scheme {
	return et.s
}

// Config returns the Kubernetes REST config for the test environment.
func (et *EnvT) Config() *rest.Config {
	return et.cfg
}

// Client returns the controller-runtime client for the test environment.
func (et *EnvT) Client() client.Client {
	return et.cli
}

// DiscoveryClient returns the Kubernetes discovery client for the test environment.
func (et *EnvT) DiscoveryClient() discovery.DiscoveryInterface {
	return et.discoveryClient
}

// DynamicClient returns the dynamic client for the test environment.
func (et *EnvT) DynamicClient() dynamic.Interface {
	return et.dynamicClient
}

// Stop stops the envtest environment.
// Note: If a manager was started, its context should be cancelled before calling Stop().
func (et *EnvT) Stop() error {
	// If et.mgr != nil, ensure its context is cancelled elsewhere before calling this.
	return et.Env.Stop()
}

// ProjectRoot returns the root directory of the project as used by the test environment.
func (et *EnvT) ProjectRoot() string {
	return et.root
}

// ReadFile reads a file from the project root, joining all provided path elements.
// Returns the file contents or an error if reading fails.
func (et *EnvT) ReadFile(elem ...string) ([]byte, error) {
	fp := filepath.Join(et.root, filepath.Join(elem...))

	content, err := os.ReadFile(fp)
	if err != nil {
		return nil, fmt.Errorf("unable to read file %s: %w", fp, err)
	}

	return content, nil
}

// Manager returns the controller-runtime manager for the test environment, if one was created.

func (et *EnvT) Manager() manager.Manager {
	return et.mgr
}

// WaitForWebhookServer waits until the webhook server managed by this EnvT is ready by dialing the port using TLS.
//
// Parameters:
//   - ctx: A non-nil context passed to the underlying Dialer; the context is used to cancel the polling loop.
//
// Returns:
//   - error: If the server is not ready within the timeout or a connection error occurs.
func (et *EnvT) WaitForWebhookServer(ctx context.Context) error {
	host := et.Env.WebhookInstallOptions.LocalServingHost
	port := et.Env.WebhookInstallOptions.LocalServingPort
	if host == "" || port == 0 {
		return fmt.Errorf("webhook server host/port not set (host=%q, port=%d)", host, port)
	}

	addrPort := fmt.Sprintf("%s:%d", host, port)

	// setup ticker for polling the webhook server
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// setup dialer
	tlsDialer := &tls.Dialer{
		Config: &tls.Config{
			InsecureSkipVerify: true, // #nosec G402
			MinVersion:         tls.VersionTLS12,
		},
		NetDialer: &net.Dialer{Timeout: 1 * time.Second},
	}

	// keep trying until the context is cancelled or the webhook server is ready
	for {
		conn, err := tlsDialer.DialContext(ctx, "tcp", addrPort)
		if err == nil {
			return conn.Close()
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("webhook server not ready (%v) before context cancelled: %w", err.Error(), ctx.Err())
		case <-ticker.C:
			continue
		}
	}
}

// BypassHandler wraps a handler and allows bypassing validation based on a custom function.
type BypassHandler struct {
	Delegate   admission.Handler
	BypassFunc func(req admission.Request) bool
}

// Handle implements the admission.Handler interface for BypassHandler.
func (h *BypassHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	if h.BypassFunc != nil && h.BypassFunc(req) {
		return admission.Allowed("Bypass allowed for test resource")
	}
	return h.Delegate.Handle(ctx, req)
}
