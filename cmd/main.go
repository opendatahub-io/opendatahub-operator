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
	"errors"
	"fmt"
	"os"
	"strings"

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
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	infrastructurev1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	dscctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/datasciencecluster"
	dscictrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/dscinitialization"
	sr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/logger"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/flags"

	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/codeflare"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/datasciencepipelines"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/feastoperator"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/kserve"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/kueue"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/llamastackoperator"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelcontroller"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelmeshserving"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/ray"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/trainingoperator"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/trustyai"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/workbenches"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/auth"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/certconfigmapgenerator"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/monitoring"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/secretgenerator"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/servicemesh"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/setup"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() { //nolint:gochecknoinits
	utilruntime.Must(componentApi.AddToScheme(scheme))
	utilruntime.Must(serviceApi.AddToScheme(scheme))
	utilruntime.Must(infrastructurev1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(dsciv1.AddToScheme(scheme))
	utilruntime.Must(dscv1.AddToScheme(scheme))
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
}

func initComponents(_ context.Context, p common.Platform) error {
	return cr.ForEach(func(ch cr.ComponentHandler) error {
		return ch.Init(p)
	})
}

func initServices(_ context.Context, p common.Platform) error {
	return sr.ForEach(func(sh sr.ServiceHandler) error {
		return sh.Init(p)
	})
}

// Create a config struct with viper's mapstructure.
type OperatorConfig struct {
	MetricsAddr         string `mapstructure:"metrics-bind-address"`
	HealthProbeAddr     string `mapstructure:"health-probe-bind-address"`
	LeaderElection      bool   `mapstructure:"leader-elect"`
	MonitoringNamespace string `mapstructure:"dsc-monitoring-namespace"`
	LogMode             string `mapstructure:"log-mode"`
	PprofAddr           string `mapstructure:"pprof-bind-address"`

	// Zap logging configuration
	ZapDevel        bool   `mapstructure:"zap-devel"`
	ZapEncoder      string `mapstructure:"zap-encoder"`
	ZapLogLevel     string `mapstructure:"zap-log-level"`
	ZapStacktrace   string `mapstructure:"zap-stacktrace-level"`
	ZapTimeEncoding string `mapstructure:"zap-time-encoding"`
}

func LoadConfig() (*OperatorConfig, error) {
	var config OperatorConfig
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal operator manager config: %w", err)
	}
	return &config, nil
}

func main() { //nolint:funlen,maintidx,gocyclo
	// Viper settings
	viper.SetEnvPrefix("ODH_MANAGER")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	// define flags and env vars
	if err := flags.AddOperatorFlagsAndEnvvars(viper.GetEnvPrefix()); err != nil {
		fmt.Printf("Error in adding flags or binding env vars: %s", err.Error())
		os.Exit(1)
	}

	// parse and bind flags
	pflag.Parse()
	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		fmt.Printf("Error in binding flags: %s", err.Error())
		os.Exit(1)
	}

	oconfig, err := LoadConfig()
	if err != nil {
		fmt.Printf("Error loading configuration: %s", err.Error())
		os.Exit(1)
	}

	// After getting the zap related configs an ad hoc flag set is created so the zap BindFlags mechanism can be reused
	zapFlagSet := flags.NewZapFlagSet()

	opts := zap.Options{}
	opts.BindFlags(zapFlagSet)

	err = flags.ParseZapFlags(zapFlagSet, oconfig.ZapDevel, oconfig.ZapEncoder, oconfig.ZapLogLevel, oconfig.ZapStacktrace, oconfig.ZapTimeEncoding)
	if err != nil {
		fmt.Printf("Error in parsing zap flags: %s", err.Error())
		os.Exit(1)
	}

	ctrl.SetLogger(logger.NewLogger(oconfig.LogMode, &opts))

	// root context
	ctx := ctrl.SetupSignalHandler()
	ctx = logf.IntoContext(ctx, setupLog)
	// Create new uncached client to run initial setup
	setupCfg, err := config.GetConfig()
	if err != nil {
		setupLog.Error(err, "error getting config for setup")
		os.Exit(1)
	}

	setupClient, err := client.New(setupCfg, client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "error getting client for setup")
		os.Exit(1)
	}

	err = cluster.Init(ctx, setupClient)
	if err != nil {
		setupLog.Error(err, "unable to initialize cluster config")
		os.Exit(1)
	}

	// Get operator platform
	release := cluster.GetRelease()
	platform := release.Name

	if err := initServices(ctx, platform); err != nil {
		setupLog.Error(err, "unable to init services")
		os.Exit(1)
	}

	if err := initComponents(ctx, platform); err != nil {
		setupLog.Error(err, "unable to init components")
		os.Exit(1)
	}

	// get old release version before we create default DSCI CR
	oldReleaseVersion, _ := upgrade.GetDeployedRelease(ctx, setupClient)

	secretCache, err := createSecretCacheConfig(ctx, setupClient, platform)
	if err != nil {
		setupLog.Error(err, "unable to get application namespace into cache")
		os.Exit(1)
	}

	oDHCache, err := createODHGeneralCacheConfig(ctx, setupClient, platform)
	if err != nil {
		setupLog.Error(err, "unable to get application namespace into cache")
		os.Exit(1)
	}

	cacheOptions := cache.Options{
		Scheme: scheme,
		ByObject: map[client.Object]cache.ByObject{
			// Cannot find a label on various screts, so we need to watch all secrets
			// this include, monitoring, dashboard, trustcabundle default cert etc for these NS
			&corev1.Secret{}: {
				Namespaces: secretCache,
			},
			// it is hard to find a label can be used for both trustCAbundle configmap and inferenceservice-config and deletionCM
			&corev1.ConfigMap{}: {
				Namespaces: oDHCache,
			},
			// For domain to get OpenshiftIngress and default cert
			&operatorv1.IngressController{}: {
				Field: fields.Set{"metadata.name": "default"}.AsSelector(),
			},
			// For authentication CR "cluster"
			&configv1.Authentication{}: {
				Field: fields.Set{"metadata.name": cluster.ClusterAuthenticationObj}.AsSelector(),
			},
			// for prometheus and black-box deployment and ones we owns
			&appsv1.Deployment{}: {
				Namespaces: oDHCache,
			},
			// kueue + monitoring need prometheusrules
			&promv1.PrometheusRule{}: {
				Namespaces: oDHCache,
			},
			&promv1.ServiceMonitor{}: {
				Namespaces: oDHCache,
			},
			&routev1.Route{}: {
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

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{ // single pod does not need to have LeaderElection
		Scheme:  scheme,
		Metrics: ctrlmetrics.Options{BindAddress: oconfig.MetricsAddr},
		WebhookServer: ctrlwebhook.NewServer(ctrlwebhook.Options{
			Port: 9443,
			// TLSOpts: , // TODO: it was not set in the old code
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
					resources.GvkToUnstructured(gvk.ServiceMeshControlPlane),
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

	// Register all webhooks using the helper
	if err := webhook.RegisterAllWebhooks(mgr); err != nil {
		setupLog.Error(err, "unable to register webhooks")
		os.Exit(1)
	}

	if err = (&dscictrl.DSCInitializationReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("dscinitialization-controller"),
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DSCInitiatlization")
		os.Exit(1)
	}

	if err = dscctrl.NewDataScienceClusterReconciler(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DataScienceCluster")
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

	// Check if user opted for disabling DSC configuration
	disableDSCConfig, existDSCConfig := os.LookupEnv("DISABLE_DSC_CONFIG")
	if existDSCConfig && disableDSCConfig != "false" {
		setupLog.Info("DSCI auto creation is disabled")
	} else {
		var createDefaultDSCIFunc manager.RunnableFunc = func(ctx context.Context) error {
			err := upgrade.CreateDefaultDSCI(ctx, setupClient, platform, oconfig.MonitoringNamespace)
			if err != nil {
				setupLog.Error(err, "unable to create initial setup for the operator")
			}
			return err
		}
		err := mgr.Add(createDefaultDSCIFunc)
		if err != nil {
			setupLog.Error(err, "error scheduling DSCI creation")
			os.Exit(1)
		}
	}

	// Create default DSC CR for managed RHOAI
	if platform == cluster.ManagedRhoai {
		var createDefaultDSCFunc manager.RunnableFunc = func(ctx context.Context) error {
			err := upgrade.CreateDefaultDSC(ctx, setupClient)
			if err != nil {
				setupLog.Error(err, "unable to create default DSC CR by the operator")
			}
			return err
		}
		err := mgr.Add(createDefaultDSCFunc)
		if err != nil {
			setupLog.Error(err, "error scheduling DSC creation")
			os.Exit(1)
		}
	}

	// TODO: to be removed: https://issues.redhat.com/browse/RHOAIENG-21080
	var patchODCFunc manager.RunnableFunc = func(ctx context.Context) error {
		if err := upgrade.PatchOdhDashboardConfig(ctx, setupClient, oldReleaseVersion, release); err != nil {
			setupLog.Error(err, "Unable to patch the odhdashboardconfig")
			return err
		}
		return nil
	}

	err = mgr.Add(patchODCFunc)
	if err != nil {
		setupLog.Error(err, "Error patching odhdashboardconfig")
	}

	// Cleanup resources from previous v2 releases
	var cleanExistingResourceFunc manager.RunnableFunc = func(ctx context.Context) error {
		if err = upgrade.CleanupExistingResource(ctx, setupClient, platform, oldReleaseVersion); err != nil {
			setupLog.Error(err, "unable to perform cleanup")
		}
		return err
	}

	err = mgr.Add(cleanExistingResourceFunc)
	if err != nil {
		setupLog.Error(err, "error remove deprecated resources from previous version")
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func getCommonCache(ctx context.Context, cli client.Client, platform common.Platform) (map[string]cache.Config, error) {
	namespaceConfigs := map[string]cache.Config{}

	// networkpolicy need operator namespace
	operatorNs, err := cluster.GetOperatorNamespace()
	if err != nil {
		return nil, err
	}

	namespaceConfigs[operatorNs] = cache.Config{}
	namespaceConfigs["redhat-ods-monitoring"] = cache.Config{}

	if platform == cluster.ManagedRhoai {
		namespaceConfigs["redhat-ods-applications"] = cache.Config{}
		namespaceConfigs[cluster.NamespaceConsoleLink] = cache.Config{}
		return namespaceConfigs, nil
	} else {
		// get the managed application's namespaces
		cNamespaceList := &corev1.NamespaceList{}
		labelSelector := client.MatchingLabels{
			labels.CustomizedAppNamespace: labels.True,
		}
		if err := cli.List(ctx, cNamespaceList, labelSelector); err != nil {
			return nil, err
		}

		switch len(cNamespaceList.Items) {
		case 0:
			if platform == cluster.SelfManagedRhoai {
				namespaceConfigs["redhat-ods-applications"] = cache.Config{}
			} else {
				namespaceConfigs["opendatahub"] = cache.Config{}
			}
			return namespaceConfigs, nil
		case 1:
			namespaceConfigs[cNamespaceList.Items[0].Name] = cache.Config{}
			return namespaceConfigs, nil
		default:
			return nil, errors.New("only support max. one namespace with label: opendatahub.io/application-namespace: true")
		}
	}
}

func createSecretCacheConfig(ctx context.Context, cli client.Client, platform common.Platform) (map[string]cache.Config, error) {
	namespaceConfigs, err := getCommonCache(ctx, cli, platform)
	if err != nil {
		return nil, err
	}

	namespaceConfigs["istio-system"] = cache.Config{} // for both knative-serving-cert and default-modelregistry-cert, as an easy workarond, to watch both in this namespace
	namespaceConfigs["openshift-ingress"] = cache.Config{}

	return namespaceConfigs, nil
}

func createODHGeneralCacheConfig(ctx context.Context, cli client.Client, platform common.Platform) (map[string]cache.Config, error) {
	namespaceConfigs, err := getCommonCache(ctx, cli, platform)
	if err != nil {
		return nil, err
	}

	namespaceConfigs["istio-system"] = cache.Config{}        // for serivcemonitor: data-science-smcp-pilot-monitor
	namespaceConfigs["openshift-operators"] = cache.Config{} // for dependent operators installed namespace

	return namespaceConfigs, nil
}

func CreateComponentReconcilers(ctx context.Context, mgr manager.Manager) error {
	l := logf.FromContext(ctx)

	return cr.ForEach(func(ch cr.ComponentHandler) error {
		l.Info("creating reconciler", "type", "component", "name", ch.GetName())
		if err := ch.NewComponentReconciler(ctx, mgr); err != nil {
			return fmt.Errorf("error creating %s component reconciler: %w", ch.GetName(), err)
		}

		return nil
	})
}

func CreateServiceReconcilers(ctx context.Context, mgr manager.Manager) error {
	log := logf.FromContext(ctx)

	return sr.ForEach(func(sh sr.ServiceHandler) error {
		log.Info("creating reconciler", "type", "service", "name", sh.GetName())
		if err := sh.NewReconciler(ctx, mgr); err != nil {
			return fmt.Errorf("error creating %s service reconciler: %w", sh.GetName(), err)
		}
		return nil
	})
}
