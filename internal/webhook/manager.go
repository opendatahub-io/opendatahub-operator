//go:build !nowebhook

package webhook

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kueuewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/kueue"
	gvk "github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// Webhooks for validating:
// - kubeflow.org/v1: pytorchjobs, notebooks
// - ray.io/v1: rayjobs, rayclusters
// - serving.kserve.io/v1beta1: inferenceservices

// Webhook service and configuration names.
const (
	AdmissionReviewVersion      = "v1"
	WebhookManagerName          = "WebhookManager"
	ValidatingWebhookConfigName = "kueuelabels-validator.opendatahub.io"
)

// Webhook specific names.
const (
	KserveKueuelabelsValidatorName   = "kserve-kueuelabels-validator.opendatahub.io"
	KubeflowKueuelabelsValidatorName = "kubeflow-kueuelabels-validator.opendatahub.io"
	RayKueuelabelsValidatorName      = "ray-kueuelabels-validator.opendatahub.io"
)

// Webhook specific labels.
const (
	// KueueManagedLabelKey indicates a namespace is managed by Kueue.
	KueueManagedLabelKey = "kueue.openshift.io/managed"
	// KueueLegacyManagedLabelKey is the legacy label key used to indicate a namespace is managed by Kueue.
	KueueLegacyManagedLabelKey = "kueue-managed"
)

// Envtest mode environment variables.
const (
	EnvtestWebhookLocalPort    = "ENVTEST_WEBHOOK_LOCAL_PORT"
	EnvtestWebhookLocalCertDir = "ENVTEST_WEBHOOK_LOCAL_CERT_DIR"
)

// clientConfigCacheTTL defines how long the clientConfig is cached.
const clientConfigCacheTTL = 5 * time.Minute

// Pre-compiled constants to reduce allocations.
var (
	sideEffectsNone   = admissionregistrationv1.SideEffectClassNone
	failurePolicyFail = admissionregistrationv1.Fail
	createUpdateOps   = []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update}
	admissionVersions = []string{AdmissionReviewVersion}
	trueValues        = []string{"true"}
	labelSelectorOpIn = metav1.LabelSelectorOpIn
	// TODO: Remove this once we have a better way to fetch clientConfig.
	// Include webhook names to fetch clientConfig.
	IncludeWebhookNames = []string{"validating-webhook-configuration", "opendatahub.io"}
	// Exclude webhook names to fetch clientConfig.
	ExcludeWebhookNames = []string{"validating.odh-model-controller.opendatahub.io"}
)

// envVars holds cached environment variable values.
type envVars struct {
	localPort     string
	localCertDir  string
	isEnvtestMode bool
}

// getEnvVars returns current environment variables.
func getEnvVars() envVars {
	localPort := os.Getenv(EnvtestWebhookLocalPort)
	localCertDir := os.Getenv(EnvtestWebhookLocalCertDir)
	return envVars{
		localPort:     localPort,
		localCertDir:  localCertDir,
		isEnvtestMode: localPort != "",
	}
}

// webhookSpec defines the specification for a single webhook.
type webhookSpec struct {
	name      string
	apiGroups []string
	versions  []string
	resources []string
}

// kueueWebhookSpecs contains all the webhook specifications for Kueue validation.
var kueueWebhookSpecs = []webhookSpec{
	{
		name:      KserveKueuelabelsValidatorName,
		apiGroups: []string{gvk.InferenceServices.Group},
		versions:  []string{gvk.InferenceServices.Version},
		resources: []string{"inferenceservices"},
	},
	{
		name:      KubeflowKueuelabelsValidatorName,
		apiGroups: []string{"kubeflow.org"},
		versions:  []string{"v1"},
		resources: []string{"pytorchjobs", "notebooks"},
	},
	{
		name:      RayKueuelabelsValidatorName,
		apiGroups: []string{"ray.io"},
		versions:  []string{"v1"},
		resources: []string{"rayjobs", "rayclusters"},
	},
}

// clientConfigCache provides a simple in-memory cache for clientConfig to reduce API calls.
type clientConfigCache struct {
	mu     sync.RWMutex
	config *admissionregistrationv1.WebhookClientConfig
	expiry time.Time
}

// globalClientConfigCache is a global cache instance for clientConfig.
var globalClientConfigCache = &clientConfigCache{}

// isValid checks if the cached clientConfig is still valid
func (c *clientConfigCache) isValid() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config != nil &&
		time.Now().Before(c.expiry)
}

// get returns the cached clientConfig if valid.
func (c *clientConfigCache) get() *admissionregistrationv1.WebhookClientConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	// Return a copy to prevent external modifications
	if c.config != nil {
		// Perform a deep copy of the clientConfig
		copiedConfig := &admissionregistrationv1.WebhookClientConfig{
			CABundle: make([]byte, len(c.config.CABundle)),
			Service:  nil,
			URL:      nil,
		}
		copy(copiedConfig.CABundle, c.config.CABundle)
		if c.config.Service != nil {
			copiedConfig.Service = &admissionregistrationv1.ServiceReference{
				Name:      c.config.Service.Name,
				Namespace: c.config.Service.Namespace,
				Path:      c.config.Service.Path,
				Port:      c.config.Service.Port,
			}
		}
		if c.config.URL != nil {
			copiedConfig.URL = c.config.URL
		}
		return copiedConfig
	}
	return nil
}

// set stores a new clientConfig in the cache.
func (c *clientConfigCache) set(cfg *admissionregistrationv1.WebhookClientConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Store a copy to prevent external modifications
	c.config = &admissionregistrationv1.WebhookClientConfig{
		CABundle: make([]byte, len(cfg.CABundle)),
		Service:  nil,
		URL:      nil,
	}
	copy(c.config.CABundle, cfg.CABundle)
	if cfg.Service != nil {
		c.config.Service = &admissionregistrationv1.ServiceReference{
			Name:      cfg.Service.Name,
			Namespace: cfg.Service.Namespace,
			Path:      cfg.Service.Path,
			Port:      cfg.Service.Port,
		}
	}
	if cfg.URL != nil {
		c.config.URL = cfg.URL
	}
	c.expiry = time.Now().Add(clientConfigCacheTTL)
}

// WebhookOption is a function type for configuring ValidatingWebhook instances.
type WebhookOption func(*admissionregistrationv1.ValidatingWebhook)

// WithName sets the webhook name.
func WithName(name string) WebhookOption {
	return func(w *admissionregistrationv1.ValidatingWebhook) {
		w.Name = name
	}
}

// WithClientConfig sets the webhook client configuration.
func WithClientConfig(cfg *admissionregistrationv1.WebhookClientConfig) WebhookOption {
	return func(w *admissionregistrationv1.ValidatingWebhook) {
		w.ClientConfig = *cfg
	}
}

// WithRules sets the webhook rules for specific API resources.
func WithRules(apiGroups, versions, resources []string) WebhookOption {
	return func(w *admissionregistrationv1.ValidatingWebhook) {
		w.Rules = []admissionregistrationv1.RuleWithOperations{
			{
				Operations: createUpdateOps,
				Rule: admissionregistrationv1.Rule{
					APIGroups:   apiGroups,
					APIVersions: versions,
					Resources:   resources,
				},
			},
		}
	}
}

// WithNamespaceSelector sets the namespace selector for the webhook.
func WithNamespaceSelector(labelKey string) WebhookOption {
	return func(w *admissionregistrationv1.ValidatingWebhook) {
		w.NamespaceSelector = &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: labelKey, Operator: labelSelectorOpIn, Values: trueValues},
			},
		}
	}
}

// NewValidatingWebhook creates a ValidatingWebhook with the given options.
func NewValidatingWebhook(options ...WebhookOption) admissionregistrationv1.ValidatingWebhook {
	webhook := admissionregistrationv1.ValidatingWebhook{
		AdmissionReviewVersions: admissionVersions,
		FailurePolicy:           &failurePolicyFail,
		SideEffects:             &sideEffectsNone,
	}
	// Apply all options
	for _, option := range options {
		option(&webhook)
	}
	return webhook
}

// NewValidatingWebhookConfiguration defines the desired state of the ValidatingWebhookConfiguration.
func NewValidatingWebhookConfiguration(
	clientConfig *admissionregistrationv1.WebhookClientConfig,
) *admissionregistrationv1.ValidatingWebhookConfiguration {
	vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ValidatingWebhookConfiguration",
			APIVersion: "admissionregistration.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: ValidatingWebhookConfigName,
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{},
	}

	for _, spec := range kueueWebhookSpecs {
		webhook := NewValidatingWebhook(
			WithName(spec.name),
			WithClientConfig(clientConfig),
			WithRules(spec.apiGroups, spec.versions, spec.resources),
			WithNamespaceSelector(KueueManagedLabelKey),
		)
		vwc.Webhooks = append(vwc.Webhooks, webhook)
	}

	// Include legacy label webhooks
	for _, spec := range kueueWebhookSpecs {
		webhook := NewValidatingWebhook(
			WithName(spec.name+"-legacy"),
			WithClientConfig(clientConfig),
			WithRules(spec.apiGroups, spec.versions, spec.resources),
			WithNamespaceSelector(KueueLegacyManagedLabelKey),
		)
		vwc.Webhooks = append(vwc.Webhooks, webhook)
	}

	return vwc
}

// getClientConfigForEnvtest returns the client config for the envtest mode.
func GetClientConfigForEnvtest(localCertDir string, localPort string) *admissionregistrationv1.WebhookClientConfig {
	url := "https://" + net.JoinHostPort("localhost", localPort) + kueuewebhook.ValidateKueuePath
	caBundle := readEnvtestCert(localCertDir)

	return &admissionregistrationv1.WebhookClientConfig{
		URL:      &url,
		CABundle: caBundle,
	}
}

func readEnvtestCert(localCertDir string) []byte {
	certPath := filepath.Join(localCertDir, "tls.crt")
	caBundle, err := os.ReadFile(certPath)
	if err != nil {
		panic(fmt.Sprintf("failed to read webhook cert at %s: %v", certPath, err))
	}
	return caBundle
}

// getSourceClientConfig retrieves the source client config, either from envtest or from an existing ValidatingWebhookConfiguration.
func getSourceClientConfig(
	ctx context.Context, c client.Client, log logr.Logger,
) (*admissionregistrationv1.WebhookClientConfig, error) {
	env := getEnvVars()
	if env.isEnvtestMode {
		return GetClientConfigForEnvtest(env.localCertDir, env.localPort), nil
	}

	if globalClientConfigCache.isValid() {
		log.Info("Using cached clientConfig for webhook reconciliation")
		return globalClientConfigCache.get(), nil
	}

	existingVWCs, err := listValidatingWebhookConfigurations(ctx, c, log)
	if err != nil {
		return nil, err
	}

	return extractClientConfigFromVWCs(existingVWCs, log), nil
}

func listValidatingWebhookConfigurations(
	ctx context.Context, c client.Client, log logr.Logger,
) (*admissionregistrationv1.ValidatingWebhookConfigurationList, error) {
	vwcList := &admissionregistrationv1.ValidatingWebhookConfigurationList{}
	if err := c.List(ctx, vwcList); err != nil {
		log.Error(err, "Failed to list ValidatingWebhookConfigurations")
		return nil, err
	}
	return vwcList, nil
}

func extractClientConfigFromVWCs(
	vwcList *admissionregistrationv1.ValidatingWebhookConfigurationList,
	log logr.Logger,
) *admissionregistrationv1.WebhookClientConfig {
	for _, vwc := range vwcList.Items {
		// Skip webhook configurations that are in exclude lists or
		// not in include lists or the current webhook configuration.
		if !matchesAnySubstring(vwc.Name, IncludeWebhookNames) ||
			matchesAnySubstring(vwc.Name, ExcludeWebhookNames) ||
			vwc.Name == ValidatingWebhookConfigName {
			continue
		}

		log.Info("Found source ValidatingWebhookConfiguration", "name", vwc.Name)

		if len(vwc.Webhooks) == 0 {
			log.Info("Source ValidatingWebhookConfiguration has no webhooks", "name", vwc.Name)
			continue
		}

		clientConfig := vwc.Webhooks[0].ClientConfig
		if !hasValidClientConfig(clientConfig) {
			log.Info("Source ValidatingWebhookConfiguration has invalid or missing clientConfig", "name", vwc.Name)
			continue
		}

		adjustedClientConfig := buildAdjustedClientConfig(clientConfig)
		globalClientConfigCache.set(adjustedClientConfig)

		log.Info("Successfully extracted clientConfig from source webhook", "sourceWebhookName", vwc.Webhooks[0].Name)
		return adjustedClientConfig
	}

	return nil
}

func hasValidClientConfig(clientConfig admissionregistrationv1.WebhookClientConfig) bool {
	return len(clientConfig.CABundle) > 0 || clientConfig.Service != nil || clientConfig.URL != nil
}

func buildAdjustedClientConfig(original admissionregistrationv1.WebhookClientConfig) *admissionregistrationv1.WebhookClientConfig {
	pathStr := kueuewebhook.ValidateKueuePath
	return &admissionregistrationv1.WebhookClientConfig{
		CABundle: original.CABundle,
		Service: func() *admissionregistrationv1.ServiceReference {
			if original.Service == nil {
				return nil
			}
			return &admissionregistrationv1.ServiceReference{
				Name:      original.Service.Name,
				Namespace: original.Service.Namespace,
				Path:      &pathStr,
				Port:      original.Service.Port,
			}
		}(),
		URL: original.URL,
	}
}

// ReconcileWebhooks manages the creation and update of MutatingWebhookConfiguration and ValidatingWebhookConfiguration resources.
// It takes the owner object (e.g., DSCInitialization instance) to set owner references for garbage collection.
func ReconcileWebhooks(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner metav1.Object) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithName(WebhookManagerName)

	// Get the source client config
	sourceClientConfig, err := getSourceClientConfig(ctx, c, log)
	if err != nil {
		return ctrl.Result{}, err
	}
	if sourceClientConfig == nil {
		log.Info(
			"No ValidatingWebhookConfiguration found with substring 'opendatahub' and a valid clientConfig. Requeuing...",
		)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Create the new ValidatingWebhookConfiguration using the extracted clientConfig
	newValidatingWebhookConfig := NewValidatingWebhookConfiguration(sourceClientConfig)

	// Set owner references
	if err := setOwnerReference(owner, newValidatingWebhookConfig, scheme, log); err != nil {
		return ctrl.Result{}, err
	}

	// Apply webhook configuration
	if err := applyWebhookConfiguration(ctx, c, newValidatingWebhookConfig, log); err != nil {
		return ctrl.Result{}, err
	}

	log.Info(
		"New webhook configuration reconciled successfully.",
		"name", ValidatingWebhookConfigName,
	)
	return ctrl.Result{}, nil
}

// setOwnerReference sets the owner reference on the webhook configuration.
func setOwnerReference(
	owner metav1.Object,
	config *admissionregistrationv1.ValidatingWebhookConfiguration,
	scheme *runtime.Scheme,
	log logr.Logger,
) error {
	if err := controllerutil.SetOwnerReference(owner, config, scheme); err != nil {
		log.Error(err, "Failed to set owner reference for ValidatingWebhookConfiguration")
		return err
	}
	return nil
}

// applyWebhookConfiguration applies the webhook configuration using server-side apply.
func applyWebhookConfiguration(ctx context.Context, c client.Client, config *admissionregistrationv1.ValidatingWebhookConfiguration, log logr.Logger) error {
	// Create the ValidatingWebhookConfiguration
	// Important: For SSA, you should pass a desired object without ResourceVersion or ManagedFields
	config.SetResourceVersion("")
	config.SetManagedFields(nil)
	applyOpts := []client.PatchOption{
		client.ForceOwnership,
		client.FieldOwner(WebhookManagerName),
	}
	if err := c.Patch(ctx, config, client.Apply, applyOpts...); err != nil {
		log.Error(err, "Failed to apply ValidatingWebhookConfiguration via SSA")
		return err
	}
	return nil
}

// matchesAnySubstring checks if the given name contains any of the provided substrings.
func matchesAnySubstring(name string, substrings []string) bool {
	for _, sub := range substrings {
		if strings.Contains(name, sub) {
			return true
		}
	}
	return false
}
