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
	"flag"
	"os"

	addonv1alpha1 "github.com/openshift/addon-operator/apis/addons/v1alpha1"
	ocappsv1 "github.com/openshift/api/apps/v1" //nolint:importas //reason: conflicts with appsv1 "k8s.io/api/apps/v1"
	buildv1 "github.com/openshift/api/build/v1"
	imagev1 "github.com/openshift/api/image/v1"
	oauthv1 "github.com/openshift/api/oauth/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	userv1 "github.com/openshift/api/user/v1"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/certconfigmapgenerator"
	dscctrl "github.com/opendatahub-io/opendatahub-operator/v2/controllers/datasciencecluster"
	dscictrl "github.com/opendatahub-io/opendatahub-operator/v2/controllers/dscinitialization"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/secretgenerator"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/webhook"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/logger"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
)

const controllerNum = 4 // we should keep this updated if we have new controllers to add

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() { //nolint:gochecknoinits
	// +kubebuilder:scaffold:scheme
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(dsciv1.AddToScheme(scheme))
	utilruntime.Must(dscv1.AddToScheme(scheme))
	utilruntime.Must(featurev1.AddToScheme(scheme))
	utilruntime.Must(networkingv1.AddToScheme(scheme))
	utilruntime.Must(addonv1alpha1.AddToScheme(scheme))
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
	utilruntime.Must(apiregistrationv1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
	utilruntime.Must(operatorv1.Install(scheme)) // here also add configv1.Install(scheme) no need add configv1 explicitly
}

func main() { //nolint:funlen,maintidx
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var dscApplicationsNamespace string
	var dscMonitoringNamespace string
	var operatorName string
	var logmode string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&dscApplicationsNamespace, "dsc-applications-namespace", "redhat-ods-applications", "The namespace where data science cluster"+
		"applications will be deployed")
	flag.StringVar(&dscMonitoringNamespace, "dsc-monitoring-namespace", "redhat-ods-monitoring", "The namespace where data science cluster"+
		"monitoring stack will be deployed")
	flag.StringVar(&operatorName, "operator-name", "opendatahub", "The name of the operator")
	flag.StringVar(&logmode, "log-mode", "", "Log mode ('', prod, devel), default to ''")

	flag.Parse()

	ctrl.SetLogger(logger.ConfigLoggers(logmode))

	// root context
	ctx := ctrl.SetupSignalHandler()
	// Create new uncached client to run initial setup
	setupCfg, err := config.GetConfig()
	if err != nil {
		setupLog.Error(err, "error getting config for setup")
		os.Exit(1)
	}
	// uplift default limiataions
	setupCfg.QPS = rest.DefaultQPS * controllerNum     // 5 * 4 controllers
	setupCfg.Burst = rest.DefaultBurst * controllerNum // 10 * 4 controllers

	setupClient, err := client.New(setupCfg, client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "error getting client for setup")
		os.Exit(1)
	}
	// Get operator platform
	release, err := cluster.GetRelease(ctx, setupClient)
	if err != nil {
		setupLog.Error(err, "error getting release")
		os.Exit(1)
	}
	platform := release.Name
	setupLog.Info("running on", "platform", platform)

	secretCache := createSecretCacheConfig(platform)
	deploymentCache := createDeploymentCacheConfig(platform)
	cacheOptions := cache.Options{
		Scheme: scheme,
		ByObject: map[client.Object]cache.ByObject{
			// all CRD: mainly for pipeline v1 teckon and v2 argo and dashboard's own CRD
			&apiextensionsv1.CustomResourceDefinition{}: {},
			// Cannot find a label on various screts, so we need to watch all secrets
			// this include, monitoring, dashboard, trustcabundle default cert etc for these NS
			&corev1.Secret{}: {
				Namespaces: secretCache,
			},
			// it is hard to find a label can be used for both trustCAbundle configmap and inferenceservice-config and deletionCM
			&corev1.ConfigMap{}: {},
			// TODO: we can limit scope of namespace if we find a way to only get list of DSProject
			// also need for monitoring, trustcabundle
			&corev1.Namespace{}: {},
			// For catsrc (avoid frequently check cluster type)
			&ofapiv1alpha1.CatalogSource{}: {
				Field: fields.Set{"metadata.name": "addon-managed-odh-catalog"}.AsSelector(),
			},
			// For domain to get OpenshiftIngress and default cert
			&operatorv1.IngressController{}: {
				Field: fields.Set{"metadata.name": "default"}.AsSelector(),
			},
			// for prometheus and black-box deployment and ones we owns
			&appsv1.Deployment{}: {Namespaces: deploymentCache},
		},
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{ // single pod does not need to have LeaderElection
		Scheme:  scheme,
		Metrics: ctrlmetrics.Options{BindAddress: metricsAddr},
		WebhookServer: ctrlwebhook.NewServer(ctrlwebhook.Options{
			Port: 9443,
			// TLSOpts: , // TODO: it was not set in the old code
		}),
		HealthProbeBindAddress: probeAddr,
		Cache:                  cacheOptions,
		LeaderElection:         enableLeaderElection,
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
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	webhook.Init(mgr)

	if err = (&dscictrl.DSCInitializationReconciler{
		Client:                mgr.GetClient(),
		Scheme:                mgr.GetScheme(),
		Log:                   logger.LogWithLevel(ctrl.Log.WithName(operatorName).WithName("controllers").WithName("DSCInitialization"), logmode),
		Recorder:              mgr.GetEventRecorderFor("dscinitialization-controller"),
		ApplicationsNamespace: dscApplicationsNamespace,
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DSCInitiatlization")
		os.Exit(1)
	}

	if err = (&dscctrl.DataScienceClusterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Log:    logger.LogWithLevel(ctrl.Log.WithName(operatorName).WithName("controllers").WithName("DataScienceCluster"), logmode),
		DataScienceCluster: &dscctrl.DataScienceClusterConfig{
			DSCISpec: &dsciv1.DSCInitializationSpec{
				ApplicationsNamespace: dscApplicationsNamespace,
			},
		},
		Recorder: mgr.GetEventRecorderFor("datasciencecluster-controller"),
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DataScienceCluster")
		os.Exit(1)
	}

	if err = (&secretgenerator.SecretGeneratorReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Log:    logger.LogWithLevel(ctrl.Log.WithName(operatorName).WithName("controllers").WithName("SecretGenerator"), logmode),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "SecretGenerator")
		os.Exit(1)
	}

	if err = (&certconfigmapgenerator.CertConfigmapGeneratorReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Log:    logger.LogWithLevel(ctrl.Log.WithName(operatorName).WithName("controllers").WithName("CertConfigmapGenerator"), logmode),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CertConfigmapGenerator")
		os.Exit(1)
	}

	// get old release version before we create default DSCI CR
	oldReleaseVersion, _ := upgrade.GetDeployedRelease(ctx, setupClient)

	// Check if user opted for disabling DSC configuration
	disableDSCConfig, existDSCConfig := os.LookupEnv("DISABLE_DSC_CONFIG")
	if existDSCConfig && disableDSCConfig != "false" {
		setupLog.Info("DSCI auto creation is disabled")
	} else {
		var createDefaultDSCIFunc manager.RunnableFunc = func(ctx context.Context) error {
			err := upgrade.CreateDefaultDSCI(ctx, setupClient, platform, dscApplicationsNamespace, dscMonitoringNamespace)
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
	// Cleanup resources from previous v2 releases
	var cleanExistingResourceFunc manager.RunnableFunc = func(ctx context.Context) error {
		if err = upgrade.CleanupExistingResource(ctx, setupClient, platform, dscApplicationsNamespace, dscMonitoringNamespace, oldReleaseVersion); err != nil {
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

func createSecretCacheConfig(platform cluster.Platform) map[string]cache.Config {
	namespaceConfigs := map[string]cache.Config{
		"istio-system":      {}, // for both knative-serving-cert and default-modelregistry-cert,as an easy workarond, to watch all in this namespace for now
		"openshift-ingress": {},
	}
	switch platform {
	case cluster.ManagedRhoai:
		namespaceConfigs["redhat-ods-monitoring"] = cache.Config{}
		namespaceConfigs["redhat-ods-applications"] = cache.Config{}
		operatorNs, err := cluster.GetOperatorNamespace()
		if err != nil {
			operatorNs = "redhat-ods-operator" // fall back case
		}
		namespaceConfigs[operatorNs] = cache.Config{}
	case cluster.SelfManagedRhoai:
		namespaceConfigs["redhat-ods-applications"] = cache.Config{}
	default:
		namespaceConfigs["opendatahub"] = cache.Config{}
	}
	return namespaceConfigs
}

func createDeploymentCacheConfig(platform cluster.Platform) map[string]cache.Config {
	namespaceConfigs := map[string]cache.Config{}
	switch platform {
	case cluster.ManagedRhoai: // no need workbench NS, only SFS no Deployment
		namespaceConfigs["redhat-ods-monitoring"] = cache.Config{}
		namespaceConfigs["redhat-ods-applications"] = cache.Config{}
	case cluster.SelfManagedRhoai:
		namespaceConfigs["redhat-ods-applications"] = cache.Config{}
	default:
		namespaceConfigs["opendatahub"] = cache.Config{}
	}

	return namespaceConfigs
}
