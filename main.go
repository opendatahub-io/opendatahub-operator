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
	"encoding/json"
	"flag"
	kfdefv1 "github.com/opendatahub-io/opendatahub-operator/apis/kfdef.apps.kubeflow.org/v1"
	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	datascienceclustercontrollers "github.com/opendatahub-io/opendatahub-operator/v2/controllers/datasciencecluster"
	dscicontr "github.com/opendatahub-io/opendatahub-operator/v2/controllers/dscinitialization"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/secretgenerator"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	addonv1alpha1 "github.com/openshift/addon-operator/apis/addons/v1alpha1"
	ocv1 "github.com/openshift/api/oauth/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	ocuserv1 "github.com/openshift/api/user/v1"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	authv1 "k8s.io/api/rbac/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	//+kubebuilder:scaffold:scheme
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(dsci.AddToScheme(scheme))
	utilruntime.Must(dsc.AddToScheme(scheme))
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
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var dscApplicationsNamespace string
	var dscMonitoringNamespace string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&dscApplicationsNamespace, "dsc-applications-namespace", "opendatahub", "The namespace where data science cluster"+
		"applications will be deployed")
	flag.StringVar(&dscMonitoringNamespace, "dsc-monitoring-namespace", "opendatahub", "The namespace where data science cluster"+
		"monitoring stack will be deployed")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
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

	if err = (&dscicontr.DSCInitializationReconciler{
		Client:                mgr.GetClient(),
		Scheme:                mgr.GetScheme(),
		Log:                   ctrl.Log.WithName("controllers").WithName("DSCInitialization"),
		Recorder:              mgr.GetEventRecorderFor("dscinitialization-controller"),
		ApplicationsNamespace: dscApplicationsNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DSCInitiatlization")
		os.Exit(1)
	}

	if err = (&datascienceclustercontrollers.DataScienceClusterReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		RestConfig: mgr.GetConfig(),
		Log:        ctrl.Log.WithName("controllers").WithName("DataScienceCluster"),
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
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "SecretGenerator")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder
	// Check if user opted for disabling DSC configuration
	_, disableDSCConfig := os.LookupEnv("DISABLE_DSC_CONFIG")
	if !disableDSCConfig {
		// Create DSCInitialization CR if it's not present
		c := mgr.GetClient()
		releaseDscInitialization := &dsci.DSCInitialization{
			TypeMeta: metav1.TypeMeta{
				Kind:       "DSCInitialization",
				APIVersion: "dscinitialization.opendatahub.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
			Spec: dsci.DSCInitializationSpec{
				ApplicationsNamespace: dscApplicationsNamespace,
				Monitoring: dsci.Monitoring{
					ManagementState: operatorv1.Managed,
					Namespace:       dscMonitoringNamespace,
				},
			},
		}
		err = c.Create(context.TODO(), releaseDscInitialization)
		switch {
		case err == nil:
			setupLog.Info("created DscInitialization resource")
		case errors.IsAlreadyExists(err):
			// Update if already exists
			setupLog.Info("DscInitialization resource already exists. Updating it.")
			data, err := json.Marshal(releaseDscInitialization)
			if err != nil {
				setupLog.Error(err, "failed to get DscInitialization custom resource data")
			}
			err = c.Patch(context.TODO(), releaseDscInitialization, client.RawPatch(types.ApplyPatchType, data),
				client.ForceOwnership, client.FieldOwner("opendatahub-operator"))
			if err != nil {
				setupLog.Error(err, "failed to update DscInitialization custom resource")
			}
		default:
			setupLog.Error(err, "failed to create DscInitialization custom resource")
			os.Exit(1)
		}
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
	platform, err := deploy.GetPlatform(setupClient)
	if err != nil {
		setupLog.Error(err, "error getting client for setup")
		os.Exit(1)
	}

	// Apply update from legacy operator
	if err = upgrade.UpdateFromLegacyVersion(setupClient, platform); err != nil {
		setupLog.Error(err, "unable to update from legacy operator version")
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

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
