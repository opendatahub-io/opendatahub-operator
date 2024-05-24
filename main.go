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
	ocappsv1 "github.com/openshift/api/apps/v1"
	ocbuildv1 "github.com/openshift/api/build/v1"
	ocimgv1 "github.com/openshift/api/image/v1"
	ocv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	ocuserv1 "github.com/openshift/api/user/v1"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	admv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	authv1 "k8s.io/api/rbac/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	kfdefv1 "github.com/opendatahub-io/opendatahub-operator/apis/kfdef.apps.kubeflow.org/v1"
	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/certconfigmapgenerator"
	datascienceclustercontrollers "github.com/opendatahub-io/opendatahub-operator/v2/controllers/datasciencecluster"
	dscicontr "github.com/opendatahub-io/opendatahub-operator/v2/controllers/dscinitialization"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/secretgenerator"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/webhook"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/logger"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() { //nolint:gochecknoinits
	//+kubebuilder:scaffold:scheme
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(dsci.AddToScheme(scheme))
	utilruntime.Must(dsc.AddToScheme(scheme))
	utilruntime.Must(featurev1.AddToScheme(scheme))
	utilruntime.Must(netv1.AddToScheme(scheme))
	utilruntime.Must(addonv1alpha1.AddToScheme(scheme))
	utilruntime.Must(authv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(apiextv1.AddToScheme(scheme))
	utilruntime.Must(routev1.Install(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(ocv1.Install(scheme))
	utilruntime.Must(ofapiv1alpha1.AddToScheme(scheme))
	utilruntime.Must(ocuserv1.Install(scheme))
	utilruntime.Must(ofapiv2.AddToScheme(scheme))
	utilruntime.Must(kfdefv1.AddToScheme(scheme))
	utilruntime.Must(ocappsv1.AddToScheme(scheme))
	utilruntime.Must(ocbuildv1.AddToScheme(scheme))
	utilruntime.Must(ocimgv1.AddToScheme(scheme))
	utilruntime.Must(apiextv1.AddToScheme(scheme))
	utilruntime.Must(admv1.AddToScheme(scheme))
	utilruntime.Must(apiregistrationv1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
}

func main() { //nolint:funlen
	var metricsAddr string
	var probeAddr string
	var dscApplicationsNamespace string
	var dscMonitoringNamespace string
	var operatorName string
	var logmode string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&dscApplicationsNamespace, "dsc-applications-namespace", "opendatahub", "The namespace where data science cluster"+
		"applications will be deployed")
	flag.StringVar(&dscMonitoringNamespace, "dsc-monitoring-namespace", "opendatahub", "The namespace where data science cluster"+
		"monitoring stack will be deployed")
	flag.StringVar(&operatorName, "operator-name", "opendatahub", "The name of the operator")
	flag.StringVar(&logmode, "log-mode", "", "Log mode ('', prod, devel), default to ''")

	flag.Parse()

	ctrl.SetLogger(logger.ConfigLoggers(logmode))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{ // single pod does not need to have LeaderElection
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	(&webhook.OpenDataHubWebhook{}).SetupWithManager(mgr)

	if err = (&dscicontr.DSCInitializationReconciler{
		Client:                mgr.GetClient(),
		Scheme:                mgr.GetScheme(),
		Log:                   logger.LogWithLevel(ctrl.Log.WithName(operatorName).WithName("controllers").WithName("DSCInitialization"), logmode),
		Recorder:              mgr.GetEventRecorderFor("dscinitialization-controller"),
		ApplicationsNamespace: dscApplicationsNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DSCInitiatlization")
		os.Exit(1)
	}

	if err = (&datascienceclustercontrollers.DataScienceClusterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Log:    logger.LogWithLevel(ctrl.Log.WithName(operatorName).WithName("controllers").WithName("DataScienceCluster"), logmode),
		DataScienceCluster: &datascienceclustercontrollers.DataScienceClusterConfig{
			DSCISpec: &dsci.DSCInitializationSpec{
				ApplicationsNamespace: dscApplicationsNamespace,
			},
		},
		Recorder: mgr.GetEventRecorderFor("datasciencecluster-controller"),
	}).SetupWithManager(mgr); err != nil {
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
	// Get operator platform
	platform, err := cluster.GetPlatform(setupClient)
	if err != nil {
		setupLog.Error(err, "error getting platform")
		os.Exit(1)
	}
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

	// Cleanup resources from previous v2 releases
	var cleanExistingResourceFunc manager.RunnableFunc = func(ctx context.Context) error {
		if err = upgrade.CleanupExistingResource(ctx, setupClient, platform, dscApplicationsNamespace, dscMonitoringNamespace); err != nil {
			setupLog.Error(err, "unable to perform cleanup")
		}
		return err
	}

	// Create default DSC CR for managed RHODS
	if platform == cluster.ManagedRhods {
		if err := upgrade.CreateDefaultDSC(context.TODO(), setupClient); err != nil {
			setupLog.Error(err, "unable to create default DSC CR by the operator")
			os.Exit(1)
		}
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
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
