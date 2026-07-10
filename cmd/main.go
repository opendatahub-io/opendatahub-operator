/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
	ocappsv1 "github.com/openshift/api/apps/v1" //nolint:importas //reason: conflicts with appsv1 "k8s.io/api/apps/v1"
	buildv1 "github.com/openshift/api/build/v1"
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	imagev1 "github.com/openshift/api/image/v1"
	oauthv1 "github.com/openshift/api/oauth/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	securityv1 "github.com/openshift/api/security/v1"
	templatev1 "github.com/openshift/api/template/v1"
	userv1 "github.com/openshift/api/user/v1"
	tlspkg "github.com/openshift/controller-runtime-common/pkg/tls"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/spf13/viper"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	crtlmanager "sigs.k8s.io/controller-runtime/pkg/manager"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	infrav1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/feastoperator"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/mlflowoperator"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelcontroller"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelsasservice"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/ogx"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/ray"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/sparkoperator"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/trainer"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/trainingoperator"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/trustyai"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/workbenches"
	dscctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/datasciencecluster"
	dscictrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/dscinitialization"
	mr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	aigatewayModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aigateway"
	mcplifecycleoperatorModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/mcplifecycleoperator"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/auth"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/certconfigmapgenerator"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/monitoring"
	sr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/setup"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/bootstrap"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/logger"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/manager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/flags"
)

// intermediateCiphers is the Mozilla Intermediate cipher set for non-OpenShift fallback.
// This restricts TLS 1.2 to strong AEAD ciphers when no cluster TLS profile is available.
var intermediateCiphers = []uint16{
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
}

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	existingComponents = map[string]cr.ComponentHandler{
		componentApi.DashboardComponentName:            dashboard.NewHandler(),
		componentApi.DataSciencePipelinesComponentName: datasciencepipelines.NewHandler(),
		componentApi.FeastOperatorComponentName:        feastoperator.NewHandler(),
		componentApi.KserveComponentName:               kserve.NewHandler(),
		componentApi.KueueComponentName:                kueue.NewHandler(),
		componentApi.OGXComponentName:                  ogx.NewHandler(),
		componentApi.MLflowOperatorComponentName:       mlflowoperator.NewHandler(),
		componentApi.ModelControllerComponentName:      modelcontroller.NewHandler(),
		componentApi.ModelRegistryComponentName:        modelregistry.NewHandler(),
		componentApi.ModelsAsServiceComponentName:      modelsasservice.NewHandler(),
		componentApi.RayComponentName:                  ray.NewHandler(),
		componentApi.SparkOperatorComponentName:        sparkoperator.NewHandler(),
		componentApi.TrainerComponentName:              trainer.NewHandler(),
		componentApi.TrainingOperatorComponentName:     trainingoperator.NewHandler(),
		componentApi.TrustyAIComponentName:             trustyai.NewHandler(),
		componentApi.WorkbenchesComponentName:          workbenches.NewHandler(),
	}

	// Component runlevel assignments.
	//
	// 20 — core AI/ML, all independent; gateway deps are on GatewayConfig
	//      service (separate lifecycle), not on other components.
	// 31 — serving stack co-deployed together (kserve, modelcontroller, kueue).
	// 32 — independent extensions, no KServe dependency.
	// 33 — components that require KServe to be Ready.
	componentRunlevels = map[string]dag.Runlevel{
		componentApi.DashboardComponentName:            dag.RL(20),
		componentApi.DataSciencePipelinesComponentName: dag.RL(20),
		componentApi.ModelRegistryComponentName:        dag.RL(20),
		componentApi.RayComponentName:                  dag.RL(20),
		componentApi.TrainerComponentName:              dag.RL(20),
		componentApi.TrainingOperatorComponentName:     dag.RL(20),
		componentApi.WorkbenchesComponentName:          dag.RL(20),

		componentApi.KserveComponentName:          dag.RL(31),
		componentApi.KueueComponentName:           dag.RL(31),
		componentApi.ModelControllerComponentName: dag.RL(31),

		componentApi.FeastOperatorComponentName:  dag.RL(32),
		componentApi.MLflowOperatorComponentName: dag.RL(32),
		componentApi.OGXComponentName:            dag.RL(32),
		componentApi.SparkOperatorComponentName:  dag.RL(32),

		componentApi.ModelsAsServiceComponentName: dag.RL(33),
		componentApi.TrustyAIComponentName:        dag.RL(33),
	}

	existingServices = map[string]sr.ServiceHandler{
		serviceApi.AuthServiceName:         auth.NewHandler(),
		certconfigmapgenerator.ServiceName: certconfigmapgenerator.NewHandler(),
		serviceApi.GatewayServiceName:      gateway.NewHandler(),
		serviceApi.MonitoringServiceName:   monitoring.NewHandler(),
		setup.ServiceName:                  setup.NewHandler(),
	}

	existingModules = map[string]mr.ModuleHandler{
		// serviceApi.MonitoringServiceName: monitoringModule.NewHandler(),
		componentApi.AIGatewayComponentName:            aigatewayModule.NewHandler(),
		componentApi.MCPLifecycleOperatorComponentName: mcplifecycleoperatorModule.NewHandler(),
	}

	moduleRunlevels = map[string]dag.Runlevel{
		componentApi.AIGatewayComponentName:            dag.RL(32),
		componentApi.MCPLifecycleOperatorComponentName: dag.RL(20),
	}
)

func init() { //nolint:gochecknoinits
	utilruntime.Must(componentApi.AddToScheme(scheme))
	utilruntime.Must(serviceApi.AddToScheme(scheme))
	utilruntime.Must(infrav1alpha1.AddToScheme(scheme))
	utilruntime.Must(infrav1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(dsciv1.AddToScheme(scheme))
	utilruntime.Must(dsciv2.AddToScheme(scheme))
	utilruntime.Must(dscv1.AddToScheme(scheme))
	utilruntime.Must(dscv2.AddToScheme(scheme))
	utilruntime.Must(featurev1.AddToScheme(scheme))
	utilruntime.Must(networkingv1.AddToScheme(scheme))
	utilruntime.Must(rbacv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(routev1.Install(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(oauthv1.Install(scheme))
	utilruntime.Must(ofapiv1alpha1.AddToScheme(scheme))
	utilruntime.Must(userv1.Install(scheme))
	utilruntime.Must(ofapiv2.AddToScheme(scheme))
	utilruntime.Must(ocappsv1.Install(scheme))
	utilruntime.Must(buildv1.Install(scheme))
	utilruntime.Must(imagev1.Install(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(admissionregistrationv1.AddToScheme(scheme))
	utilruntime.Must(promv1.AddToScheme(scheme))
	utilruntime.Must(operatorv1.Install(scheme))
	utilruntime.Must(consolev1.AddToScheme(scheme))
	utilruntime.Must(securityv1.Install(scheme))
	utilruntime.Must(templatev1.Install(scheme))
	utilruntime.Must(gwapiv1.Install(scheme))
	utilruntime.Must(maasv1alpha1.AddToScheme(scheme))
	utilruntime.Must(configv1alpha1.AddToScheme(scheme))
}

func initComponents(_ context.Context, p common.Platform, cfg operatorconfig.OperatorSettings) error {
	return cr.ForEach(func(ch cr.ComponentHandler) error {
		return ch.Init(p, cfg)
	})
}

func initServices(_ context.Context, p common.Platform) error {
	return sr.ForEach(func(sh sr.ServiceHandler) error {
		return sh.Init(p)
	})
}

func registerComponents() {
	for name, handler := range existingComponents {
		rl := dag.RL(99)
		if r, ok := componentRunlevels[name]; ok {
			rl = r
		}

		cr.Add(handler, cr.WithRunlevel(rl))
		provision.Add(name, provision.KindComponent, rl)

		if !flags.IsComponentEnabled(name) {
			cr.Disable(name)
			provision.Disable(name)
		}
	}
}

func registerServices() {
	for name, handler := range existingServices {
		sr.Add(handler)
		if !flags.IsServiceEnabled(name) {
			sr.Disable(name)
		}
	}
}

func registerModules() {
	for name, handler := range existingModules {
		rl := dag.RL(99)
		if r, ok := moduleRunlevels[name]; ok {
			rl = r
		}

		mr.Add(handler, mr.WithRunlevel(rl))
		provision.Add(name, provision.KindModule, rl)

		if !flags.IsModuleEnabled(name) {
			mr.Disable(name)
			provision.Disable(name)
		}
	}
}

func main() { //nolint:funlen,maintidx,gocyclo
	// Setup Viper
	viper.SetEnvPrefix("ODH_MANAGER")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	// Register component/service suppression flags (before pflag.Parse)
	if err := flags.RegisterComponentSuppressionFlags(slices.Collect(maps.Keys(existingComponents))); err != nil {
		fmt.Printf("Error registering component suppression flags: %s", err.Error())
		os.Exit(1)
	}
	if err := flags.RegisterServiceSuppressionFlags(slices.Collect(maps.Keys(existingServices))); err != nil {
		fmt.Printf("Error registering service suppression flags: %s", err.Error())
		os.Exit(1)
	}
	if err := flags.RegisterModuleSuppressionFlags(slices.Collect(maps.Keys(existingModules))); err != nil {
		fmt.Printf("Error registering module suppression flags: %s", err.Error())
		os.Exit(1)
	}

	if err := flags.RegisterDAGOrderingFlags(); err != nil {
		fmt.Printf("Error registering DAG ordering flags: %s", err.Error())
		os.Exit(1)
	}

	oconfig, err := operatorconfig.LoadConfig()
	if err != nil {
		fmt.Printf("Error loading configuration: %s", err.Error())
		os.Exit(1)
	}

	// Register handlers and apply suppression flags disabling the corresponding component/service/module
	registerComponents()
	registerServices()
	registerModules()

	ctrl.SetLogger(logger.NewLogger(oconfig.LogMode, oconfig.ZapOptions))

	// root context
	ctx := ctrl.SetupSignalHandler()
	ctx = logf.IntoContext(ctx, setupLog)

	// This client does not use the cache.
	setupClient, err := client.New(oconfig.RestConfig, client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "error getting client for setup")
		os.Exit(1)
	}

	err = cluster.Init(ctx, setupClient, oconfig.OperatorSettings)
	if err != nil {
		setupLog.Error(err, "unable to initialize cluster config")
		os.Exit(1)
	}

	if oconfig.MonitoringNamespace == "" {
		switch cluster.GetRelease().Name {
		case cluster.ManagedRhoai, cluster.SelfManagedRhoai:
			oconfig.MonitoringNamespace = cluster.DefaultMonitoringNamespaceRHOAI
		default:
			oconfig.MonitoringNamespace = cluster.DefaultMonitoringNamespaceODH
		}
	}

	// If RHAI_APPLICATIONS_NAMESPACE is explicitly configured (via env var or CLI flag),
	// it overrides the platform-detected namespace set during cluster.Init().
	rhaiNS := flags.GetRHAIApplicationsNamespace()
	cluster.SetRHAIApplicationNamespace(rhaiNS)

	// Validate RHAI_APPLICATIONS_NAMESPACE against DSCI enablement.
	// When DSCI is disabled (non-OpenShift) the namespace must be injected explicitly.
	// When DSCI is enabled (OpenShift) the namespace is managed by DSCI and must not be overridden here.
	switch {
	case !flags.IsDSCIEnabled() && rhaiNS == "":
		setupLog.Error(errors.New("RHAI_APPLICATIONS_NAMESPACE must be set when DSCI is disabled"), "invalid configuration")
		os.Exit(1)
	case flags.IsDSCIEnabled() && rhaiNS != "":
		setupLog.Error(fmt.Errorf(
			"RHAI_APPLICATIONS_NAMESPACE (%q) must not be set when DSCI is enabled; use DSCI spec.applicationsNamespace instead",
			rhaiNS,
		), "invalid configuration")
		os.Exit(1)
	}

	// Validate RHAI_VERSION: required on XKS (no OLM/CSV), must not be set on OpenShift.
	rhaiVersion := flags.GetRHAIVersion()
	release := cluster.GetRelease()
	platform := release.Name

	switch {
	case platform == cluster.XKS && rhaiVersion == "":
		setupLog.Error(errors.New("RHAI_VERSION must be set when platform is XKS"), "invalid configuration")
		os.Exit(1)
	case platform != cluster.XKS && rhaiVersion != "":
		setupLog.Error(fmt.Errorf(
			"RHAI_VERSION (%q) must not be set when platform is not XKS; version is detected from CSV",
			rhaiVersion,
		), "invalid configuration")
		os.Exit(1)
	}

	if err := initServices(ctx, platform); err != nil {
		setupLog.Error(err, "unable to init services")
		os.Exit(1)
	}

	if err := initComponents(ctx, platform, oconfig.OperatorSettings); err != nil {
		setupLog.Error(err, "unable to init components")
		os.Exit(1)
	}

	secretCache, err := createSecretCacheConfig(platform)
	if err != nil {
		setupLog.Error(err, "unable to get application namespace into cache")
		os.Exit(1)
	}

	oDHCache, err := createODHGeneralCacheConfig(platform)
	if err != nil {
		setupLog.Error(err, "unable to get application namespace into cache")
		os.Exit(1)
	}

	cacheOptions := cache.Options{
		Scheme: scheme,
		ByObject: map[client.Object]cache.ByObject{
			// Cannot find a label on various secrets, so we need to watch all secrets
			// this includes, monitoring, dashboard, trustcabundle default cert etc for these NS
			&corev1.Secret{}: {
				Namespaces: secretCache,
			},
			// it is hard to find a label can be used for both trustCAbundle configmap and inferenceservice-config and deletionCM
			&corev1.ConfigMap{}: {
				Namespaces: oDHCache,
			},
			// for prometheus and black-box deployment and ones we owns
			&appsv1.Deployment{}: {
				Namespaces: oDHCache,
			},
			&networkingv1.NetworkPolicy{}: {
				Namespaces: oDHCache,
			},
			&rbacv1.Role{}: {
				Namespaces: oDHCache,
			},
			&rbacv1.RoleBinding{}: {
				Namespaces: oDHCache,
			},
		},
		DefaultTransform: func(in any) (any, error) {
			// Nilcheck managed fields to avoid hitting https://github.com/kubernetes/kubernetes/issues/124337
			if obj, err := meta.Accessor(in); err == nil && obj.GetManagedFields() != nil {
				obj.SetManagedFields(nil)
			}

			return in, nil
		},
	}

	// OpenShift-specific cache filters: only register when running on OpenShift
	if cluster.GetClusterInfo().Type == cluster.ClusterTypeOpenShift {
		cacheOptions.ByObject[&operatorv1.IngressController{}] = cache.ByObject{
			Field: fields.Set{"metadata.name": "default"}.AsSelector(),
		}
		cacheOptions.ByObject[&configv1.Authentication{}] = cache.ByObject{
			Field: fields.Set{"metadata.name": cluster.ClusterAuthenticationObj}.AsSelector(),
		}
		cacheOptions.ByObject[&routev1.Route{}] = cache.ByObject{
			Namespaces: oDHCache,
		}
	}

	// Prometheus operator cache filters: only register when the API is available
	addCacheIfAvailable(setupClient, cacheOptions.ByObject, &promv1.PrometheusRule{}, gvk.PrometheusRule, cache.ByObject{Namespaces: oDHCache})
	addCacheIfAvailable(setupClient, cacheOptions.ByObject, &promv1.ServiceMonitor{}, gvk.ServiceMonitor, cache.ByObject{Namespaces: oDHCache})

	// Fetch the cluster TLS security profile for webhook and metrics servers
	tlsOpts, tlsProfile, hasOpenShiftConfigAPI := fetchTLSProfile(ctx, scheme, oconfig.RestConfig)

	ctrlMgr, err := ctrl.NewManager(oconfig.RestConfig, ctrl.Options{ // single pod does not need to have LeaderElection
		Scheme: scheme,
		// This is the default mapper provider, we define it to ensure it remains
		// consistent with controller-runtime updates. It is needed for the action dynamicownership.
		MapperProvider: apiutil.NewDynamicRESTMapper,
		Metrics:        ctrlmetrics.Options{BindAddress: oconfig.MetricsAddr, TLSOpts: tlsOpts},
		WebhookServer: ctrlwebhook.NewServer(ctrlwebhook.Options{
			Port:    9443,
			TLSOpts: tlsOpts,
		}),
		PprofBindAddress:       oconfig.PprofAddr,
		HealthProbeBindAddress: oconfig.HealthProbeAddr,
		Cache:                  cacheOptions,
		LeaderElection:         oconfig.LeaderElection,
		LeaderElectionID:       "07ed84f7.opendatahub.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{
					resources.GvkToUnstructured(gvk.OpenshiftIngress),
					&ofapiv1alpha1.Subscription{},
					&authorizationv1.SelfSubjectRulesReview{},
					&corev1.Pod{},
					&userv1.Group{},
					&ofapiv1alpha1.CatalogSource{},
				},
				// Set it to true so the cache-backed client reads unstructured objects
				// or lists from the cache instead of a live lookup.
				Unstructured: true,
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Wrap the manager to return the wrapped client from GetClient()
	mgr := manager.New(ctrlMgr, manager.WithManifestsBasePath(oconfig.ManifestsBasePath), manager.WithChartsBasePath(oconfig.ChartsBasePath))

	// Register all webhooks using the helper
	if err := webhook.RegisterAllWebhooks(mgr); err != nil {
		setupLog.Error(err, "unable to register webhooks")
		os.Exit(1)
	}

	if flags.IsDSCIEnabled() {
		if err = (&dscictrl.DSCInitializationReconciler{
			Client:           mgr.GetClient(),
			Scheme:           mgr.GetScheme(),
			Recorder:         mgr.GetEventRecorder("dscinitialization-controller"),
			OperatorSettings: oconfig.OperatorSettings,
		}).SetupWithManager(ctx, mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "DSCInitiatlization")
			os.Exit(1)
		}
	} else {
		setupLog.Info("DSCI controller is suppressed")
	}

	if flags.IsDSCEnabled() {
		if err = dscctrl.NewDataScienceClusterReconciler(ctx, mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "DataScienceCluster")
			os.Exit(1)
		}
	} else {
		setupLog.Info("DSC controller is suppressed")
	}

	if err = mr.NewModuleReconciler(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "modules")
		os.Exit(1)
	}

	// Initialize service reconcilers
	if err := CreateServiceReconcilers(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create service controllers")
		os.Exit(1)
	}

	// Initialize component reconcilers
	if err = CreateComponentReconcilers(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create component controllers")
		os.Exit(1)
	}

	// Combined sequential initialization to avoid race conditions between
	// cleanup and DSCI/DSC creation (RHOAIENG-48054).
	leaderInitCfg := bootstrap.LeaderElectionInitConfig{
		Platform:                  platform,
		ManifestsBasePath:         oconfig.ManifestsBasePath,
		MonitoringNamespace:       oconfig.MonitoringNamespace,
		DSCIEnabled:               flags.IsDSCIEnabled(),
		DSCEnabled:                flags.IsDSCEnabled(),
		CreateDefaultDSCIDisabled: oconfig.IsDSCICreationDisabled(),
	}
	leaderInitHooks := bootstrap.DefaultLeaderElectionInitHooks()
	initFunc := LeaderElectionRunnableFunc(func(ctx context.Context) error {
		return bootstrap.RunLeaderElectionInit(ctx, setupLog, setupClient, leaderInitCfg, leaderInitHooks)
	})

	err = mgr.Add(initFunc)
	if err != nil {
		setupLog.Error(err, "error scheduling initialization")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Register SecurityProfileWatcher on OpenShift: cancel context on TLS profile change so pod restarts
	mgrCtx := ctx
	if hasOpenShiftConfigAPI {
		var mgrCancel context.CancelFunc
		mgrCtx, mgrCancel = context.WithCancel(ctx)

		watcher := &tlspkg.SecurityProfileWatcher{
			Client:                mgr.GetClient(),
			InitialTLSProfileSpec: tlsProfile,
			OnProfileChange: func(_ context.Context, _, _ configv1.TLSProfileSpec) {
				setupLog.Info("TLS profile changed, initiating graceful shutdown to reload")
				mgrCancel()
			},
		}
		if err := watcher.SetupWithManager(mgr); err != nil {
			mgrCancel()
			setupLog.Error(err, "unable to register TLS security profile watcher")
			os.Exit(1)
		}
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(mgrCtx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

//nolint:ireturn
func LeaderElectionRunnableFunc(fn crtlmanager.RunnableFunc) crtlmanager.Runnable {
	return &LeaderElectionRunnableWrapper{Fn: fn}
}

type LeaderElectionRunnableWrapper struct {
	Fn crtlmanager.RunnableFunc
}

func (l *LeaderElectionRunnableWrapper) Start(ctx context.Context) error {
	return l.Fn(ctx)
}

func (l *LeaderElectionRunnableWrapper) NeedLeaderElection() bool {
	return true
}

func getCommonCache(platform common.Platform) (map[string]cache.Config, error) {
	namespaceConfigs := map[string]cache.Config{}

	// networkpolicy need operator namespace
	operatorNs, err := cluster.GetOperatorNamespace()
	if err != nil {
		return nil, err
	}

	namespaceConfigs[operatorNs] = cache.Config{}
	namespaceConfigs["redhat-ods-monitoring"] = cache.Config{}

	// Get application namespace from cluster config
	appNamespace := cluster.GetApplicationNamespace()
	namespaceConfigs[appNamespace] = cache.Config{}

	// Add console link namespace for managed RHOAI
	if platform == cluster.ManagedRhoai {
		namespaceConfigs[cluster.NamespaceConsoleLink] = cache.Config{}
	}

	return namespaceConfigs, nil
}

func createSecretCacheConfig(platform common.Platform) (map[string]cache.Config, error) {
	namespaceConfigs, err := getCommonCache(platform)
	if err != nil {
		return nil, err
	}

	namespaceConfigs["openshift-ingress"] = cache.Config{}

	return namespaceConfigs, nil
}

func createODHGeneralCacheConfig(platform common.Platform) (map[string]cache.Config, error) {
	namespaceConfigs, err := getCommonCache(platform)
	if err != nil {
		return nil, err
	}

	namespaceConfigs["openshift-operators"] = cache.Config{} // for dependent operators installed namespace
	namespaceConfigs["openshift-ingress"] = cache.Config{}   // for gateway auth proxy resources
	namespaceConfigs["models-as-a-service"] = cache.Config{} // for maas admin rolebinding
	namespaceConfigs["kuadrant-system"] = cache.Config{}     // for kuadrant admin rolebinding

	return namespaceConfigs, nil
}

// addCacheIfAvailable adds obj to the ByObject cache map only when its API is
// present on the cluster. This prevents startup failures on clusters that do
// not have the corresponding CRD installed (e.g. Prometheus operator on vanilla K8s).
func addCacheIfAvailable(cli client.Client, byObject map[client.Object]cache.ByObject, obj client.Object, g schema.GroupVersionKind, opts cache.ByObject) {
	if ok, _ := cluster.IsAPIAvailable(cli, g); ok {
		byObject[obj] = opts
	}
}

func fetchTLSProfile(ctx context.Context, scheme *runtime.Scheme, restCfg *rest.Config) ([]func(*tls.Config), configv1.TLSProfileSpec, bool) {
	var tlsOpts []func(*tls.Config)
	var profile configv1.TLSProfileSpec
	hasAPI := false
	nextProtos := []string{"h2", "http/1.1"}

	bootstrapClient, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "unable to create bootstrap client for TLS profile")
		os.Exit(1)
	}

	profile, err = tlspkg.FetchAPIServerTLSProfile(ctx, bootstrapClient)
	if err != nil {
		switch {
		case meta.IsNoMatchError(err):
			setupLog.Info("TLS profile not available, using hardened defaults (non-OpenShift cluster)")
		case k8serr.IsNotFound(err):
			setupLog.Info("APIServer resource not found, using hardened defaults")
		default:
			setupLog.Error(err, "unable to read APIServer TLS profile, refusing to start with unknown TLS posture")
			os.Exit(1)
		}
		tlsOpts = append(tlsOpts, func(c *tls.Config) {
			c.MinVersion = tls.VersionTLS12
			c.CipherSuites = intermediateCiphers
			c.NextProtos = nextProtos
		})
	} else {
		hasAPI = true
		tlsConfigFn, unsupportedCiphers := tlspkg.NewTLSConfigFromProfile(profile)
		if len(unsupportedCiphers) > 0 {
			setupLog.Info("some ciphers from TLS profile are not supported by Go", "unsupported", unsupportedCiphers)
		}
		tlsOpts = append(tlsOpts, tlsConfigFn, func(c *tls.Config) {
			c.NextProtos = nextProtos
		})
	}

	return tlsOpts, profile, hasAPI
}

func CreateComponentReconcilers(ctx context.Context, mgr *manager.Manager) error {
	l := logf.FromContext(ctx)

	return cr.ForEach(func(ch cr.ComponentHandler) error {
		l.Info("creating reconciler", "type", "component", "name", ch.GetName())
		if err := ch.NewComponentReconciler(ctx, mgr); err != nil {
			return fmt.Errorf("error creating %s component reconciler: %w", ch.GetName(), err)
		}

		return nil
	})
}

func CreateServiceReconcilers(ctx context.Context, mgr *manager.Manager) error {
	log := logf.FromContext(ctx)

	return sr.ForEach(func(sh sr.ServiceHandler) error {
		log.Info("creating reconciler", "type", "service", "name", sh.GetName())
		if err := sh.NewReconciler(ctx, mgr); err != nil {
			return fmt.Errorf("error creating %s service reconciler: %w", sh.GetName(), err)
		}
		return nil
	})
}
