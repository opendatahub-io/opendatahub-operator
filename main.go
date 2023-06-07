/*
Copyright 2022.

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
	"flag"
	"github.com/opendatahub-io/opendatahub-operator/controllers/secretgenerator"
	ocv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	//operatorsv1alpha1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/o"
	apiserv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	ocappsv1 "github.com/openshift/api/apps/v1"
	ocbuildv1 "github.com/openshift/api/build/v1"
	ocimgv1 "github.com/openshift/api/image/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	admv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	awspluginskubefloworgv1alpha1 "github.com/opendatahub-io/opendatahub-operator/apis/aws.plugins.kubeflow.org/v1alpha1"
	gcppluginskubefloworgv1alpha1 "github.com/opendatahub-io/opendatahub-operator/apis/gcp.plugins.kubeflow.org/v1alpha1"
	kfconfigappskubefloworgv1alpha1 "github.com/opendatahub-io/opendatahub-operator/apis/kfconfig.apps.kubeflow.org/v1alpha1"
	kfdefappskubefloworgv1 "github.com/opendatahub-io/opendatahub-operator/apis/kfdef.apps.kubeflow.org/v1"
	kfupdateappskubefloworgv1alpha1 "github.com/opendatahub-io/opendatahub-operator/apis/kfupdate.apps.kubeflow.org/v1alpha1"
	kfdefappskubefloworg "github.com/opendatahub-io/opendatahub-operator/controllers/kfdef.apps.kubeflow.org"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(kfdefappskubefloworgv1.AddToScheme(scheme))
	utilruntime.Must(kfconfigappskubefloworgv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kfupdateappskubefloworgv1alpha1.AddToScheme(scheme))
	utilruntime.Must(awspluginskubefloworgv1alpha1.AddToScheme(scheme))
	utilruntime.Must(gcppluginskubefloworgv1alpha1.AddToScheme(scheme))
	utilruntime.Must(apiserv1.AddToScheme(scheme))
	utilruntime.Must(ocv1.AddToScheme(scheme))
	utilruntime.Must(ocappsv1.AddToScheme(scheme))
	utilruntime.Must(ocbuildv1.AddToScheme(scheme))
	utilruntime.Must(ocimgv1.AddToScheme(scheme))
	utilruntime.Must(admv1.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(v1.AddToScheme(scheme))
	utilruntime.Must(netv1.AddToScheme(scheme))
	utilruntime.Must(rbacv1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(ofapi.AddToScheme(scheme))
	//operatorsv1alpha1.AddToScheme,

	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
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
		LeaderElectionID:       "kfdef-controller",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this optiover,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&kfdefappskubefloworg.KfDefReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		RestConfig: mgr.GetConfig(),
		Recorder:   mgr.GetEventRecorderFor("kfdef-controller"),
		Log:        ctrl.Log.WithName("controllers").WithName("KfDef"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KfDef")
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

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager 123 !!!!")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
