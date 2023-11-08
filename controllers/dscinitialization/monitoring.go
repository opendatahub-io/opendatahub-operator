package dscinitialization

import (
	"context"
	b64 "encoding/base64"
	"fmt"
	"path/filepath"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
)

var (
	ComponentName           = "monitoring"
	alertManagerPath        = filepath.Join(deploy.DefaultManifestPath, ComponentName, "alertmanager")
	prometheusManifestsPath = filepath.Join(deploy.DefaultManifestPath, ComponentName, "prometheus", "base")
	prometheusConfigPath    = filepath.Join(deploy.DefaultManifestPath, ComponentName, "prometheus", "apps")
	NameConsoleLink         = "console"
	NamespaceConsoleLink    = "openshift-console"
)

// only when reconcile on DSCI CR, initial set to true
// if reconcile from monitoring, initial set to false, skip blackbox and rolebinding.
func (r *DSCInitializationReconciler) configureManagedMonitoring(ctx context.Context, dscInit *dsci.DSCInitialization, initial string) error {
	if initial == "init" {
		// configure Blackbox exporter
		if err := configureBlackboxExporter(ctx, dscInit, r); err != nil {
			return fmt.Errorf("error in configureBlackboxExporter: %w", err)
		}
	}
	if initial == "revertbackup" {
		err := common.MatchLineInFile(filepath.Join(prometheusConfigPath, "prometheus-configs.yaml"),
			map[string]string{
				"*.rules: ": "",
			})
		if err != nil {
			r.Log.Error(err, "error to remove previous enabled component rules")

			return err
		}
	}

	// configure Alertmanager
	if err := configureAlertManager(ctx, dscInit, r); err != nil {
		return fmt.Errorf("error in configureAlertManager: %w", err)
	}

	// configure Prometheus
	if err := configurePrometheus(ctx, dscInit, r); err != nil {
		return fmt.Errorf("error in configurePrometheus: %w", err)
	}

	if initial == "init" {
		err := cluster.UpdatePodSecurityRolebinding(r.Client, dscInit.Spec.Monitoring.Namespace, "redhat-ods-monitoring")
		if err != nil {
			return fmt.Errorf("error to update monitoring security rolebinding: %w", err)
		}
	}

	r.Log.Info("Success: finish config managed monitoring stack!")

	return nil
}

func configureAlertManager(ctx context.Context, dsciInit *dsci.DSCInitialization, r *DSCInitializationReconciler) error {
	// Get Deadmansnitch secret
	deadmansnitchSecret, err := r.waitForManagedSecret(ctx, "redhat-rhods-deadmanssnitch", dsciInit.Spec.Monitoring.Namespace)
	if err != nil {
		r.Log.Error(err, "error getting deadmansnitch secret from namespace "+dsciInit.Spec.Monitoring.Namespace)

		return err
	}
	// r.Log.Info("Success: got deadmansnitch secret")

	// Get PagerDuty Secret
	pagerDutySecret, err := r.waitForManagedSecret(ctx, "redhat-rhods-pagerduty", dsciInit.Spec.Monitoring.Namespace)
	if err != nil {
		r.Log.Error(err, "error getting pagerduty secret from namespace "+dsciInit.Spec.Monitoring.Namespace)

		return err
	}
	// r.Log.Info("Success: got pagerduty secret")

	// Get Smtp Secret
	smtpSecret, err := r.waitForManagedSecret(ctx, "redhat-rhods-smtp", dsciInit.Spec.Monitoring.Namespace)
	if err != nil {
		r.Log.Error(err, "error getting smtp secret from namespace "+dsciInit.Spec.Monitoring.Namespace)

		return err
	}
	// r.Log.Info("Success: got smtp secret")

	// Replace variables in alertmanager configmap for the initial time
	// TODO: Following variables can later be exposed by the API
	err = common.ReplaceInFile(filepath.Join(alertManagerPath, "alertmanager-configs.yaml"),
		map[string]string{
			"<snitch_url>":      string(deadmansnitchSecret.Data["SNITCH_URL"]),
			"<pagerduty_token>": string(pagerDutySecret.Data["PAGERDUTY_KEY"]),
			"<smtp_host>":       string(smtpSecret.Data["host"]),
			"<smtp_port>":       string(smtpSecret.Data["port"]),
			"<smtp_username>":   string(smtpSecret.Data["username"]),
			"<smtp_password>":   string(smtpSecret.Data["password"]),
			"@devshift.net":     "@rhmw.io",
		})
	if err != nil {
		r.Log.Error(err, "error to inject data to alertmanager-configs.yaml")

		return err
	}
	// r.Log.Info("Success: inject alertmanage-configs.yaml")

	operatorNs, err := upgrade.GetOperatorNamespace()
	if err != nil {
		r.Log.Error(err, "error getting operator namespace for smtp secret")

		return err
	}
	// Get SMTP receiver email secret (assume operator namespace for managed service is not configurable)
	smtpEmailSecret, err := r.waitForManagedSecret(ctx, "addon-managed-odh-parameters", operatorNs)
	if err != nil {
		return fmt.Errorf("error getting smtp receiver email secret: %w", err)
	}
	// r.Log.Info("Success: got smpt email secret")
	// replace smtpEmailSecret in alertmanager-configs.yaml
	if err = common.MatchLineInFile(filepath.Join(alertManagerPath, "alertmanager-configs.yaml"),
		map[string]string{
			"- to: ": "- to: " + string(smtpEmailSecret.Data["notification-email"]),
		},
	); err != nil {
		r.Log.Error(err, "error to update with new notification-email")

		return err
	}
	// r.Log.Info("Success: update alertmanage-configs.yaml with email")
	err = deploy.DeployManifestsFromPath(r.Client, dsciInit, alertManagerPath, dsciInit.Spec.Monitoring.Namespace, "alertmanager", true)
	if err != nil {
		r.Log.Error(err, "error to deploy manifests", "path", alertManagerPath)

		return err
	}
	// r.Log.Info("Success: update alertmanager with manifests")

	// Create alertmanager-proxy secret
	if err := createMonitoringProxySecret(r.Client, "alertmanager-proxy", dsciInit); err != nil {
		r.Log.Error(err, "error to create secret alertmanager-proxy")

		return err
	}
	// r.Log.Info("Success: create alertmanager-proxy secret")
	return nil
}

func configurePrometheus(ctx context.Context, dsciInit *dsci.DSCInitialization, r *DSCInitializationReconciler) error { //nolint:funlen
	// Update rolebinding-viewer
	if err := common.ReplaceInFile(filepath.Join(prometheusManifestsPath, "prometheus-rolebinding-viewer.yaml"),
		map[string]string{
			"<odh_monitoring_project>": dsciInit.Spec.Monitoring.Namespace,
		}); err != nil {
		r.Log.Error(err, "error to inject data to prometheus-rolebinding-viewer.yaml")

		return err
	}

	// Update prometheus-config for dashboard, dsp and workbench
	consolelinkDomain, err := common.GetDomain(r.Client, NameConsoleLink, NamespaceConsoleLink)
	if err != nil {
		return fmt.Errorf("error getting console route URL : %v", err)
	} else {
		if err = common.ReplaceInFile(filepath.Join(prometheusConfigPath, "prometheus-configs.yaml"),
			map[string]string{
				"<odh_application_namespace>": dsciInit.Spec.ApplicationsNamespace,
				"<odh_monitoring_project>":    dsciInit.Spec.Monitoring.Namespace,
				"<console_domain>":            consolelinkDomain,
			}); err != nil {
			r.Log.Error(err, "error to inject data to prometheus-configs.yaml")

			return err
		}
	}

	// Deploy prometheus manifests from prometheus/apps
	if err = deploy.DeployManifestsFromPath(
		r.Client,
		dsciInit,
		prometheusConfigPath,
		dsciInit.Spec.Monitoring.Namespace,
		"prometheus",
		dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed); err != nil {
		r.Log.Error(err, "error to deploy manifests for prometheus configs", "path", prometheusConfigPath)

		return err
	}

	// Get prometheus configmap
	prometheusConfigMap := &corev1.ConfigMap{}
	if err = r.Client.Get(ctx, client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "prometheus",
	}, prometheusConfigMap); err != nil {
		r.Log.Error(err, "error to get configmap 'prometheus'")

		return err
	}

	// Get encoded prometheus data from configmap 'prometheus'
	prometheusData, err := common.GetMonitoringData(fmt.Sprint(prometheusConfigMap.Data))
	if err != nil {
		r.Log.Error(err, "error to get prometheus data")

		return err
	}

	// Get alertmanager host
	alertmanagerRoute := &routev1.Route{}
	if err = r.Client.Get(ctx, client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "alertmanager",
	}, alertmanagerRoute); err != nil {
		r.Log.Error(err, "error to get alertmanager route")

		return err
	}

	// Get alertmanager configmap
	alertManagerConfigMap := &corev1.ConfigMap{}
	if err = r.Client.Get(ctx, client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "alertmanager",
	}, alertManagerConfigMap); err != nil {
		r.Log.Error(err, "error to get configmap 'alertmanager'")

		return err
	}

	alertmanagerData, err := common.GetMonitoringData(alertManagerConfigMap.Data["alertmanager.yml"])
	if err != nil {
		r.Log.Error(err, "error to get encoded alertmanager data from alertmanager.yml")

		return err
	}

	// Update prometheus deployment with alertmanager and prometheus data
	if err = common.ReplaceInFile(filepath.Join(prometheusManifestsPath, "prometheus-deployment.yaml"),
		map[string]string{
			"<set_alertmanager_host>": alertmanagerRoute.Spec.Host,
		}); err != nil {
		r.Log.Error(err, "error to inject set_alertmanager_host to prometheus-deployment.yaml")

		return err
	}

	if err = common.MatchLineInFile(filepath.Join(prometheusManifestsPath, "prometheus-deployment.yaml"),
		map[string]string{
			"alertmanager: ": "alertmanager: " + alertmanagerData,
			"prometheus: ":   "prometheus: " + prometheusData,
		}); err != nil {
		r.Log.Error(err, "error to update annotations in prometheus-deployment.yaml")

		return err
	}

	// final apply prometheus manifests including prometheus deployment
	err = deploy.DeployManifestsFromPath(r.Client, dsciInit, prometheusManifestsPath, dsciInit.Spec.Monitoring.Namespace, "prometheus", true)
	if err != nil {
		r.Log.Error(err, "error to deploy manifests for prometheus", "path", prometheusManifestsPath)

		return err
	}

	// Create prometheus-proxy secret
	err = createMonitoringProxySecret(r.Client, "prometheus-proxy", dsciInit)

	return err
}

func configureBlackboxExporter(ctx context.Context, dsciInit *dsci.DSCInitialization, r *DSCInitializationReconciler) error {
	consoleRoute := &routev1.Route{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: "console", Namespace: "openshift-console"}, consoleRoute)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return err
		}
	}

	blackBoxPath := filepath.Join(deploy.DefaultManifestPath, "monitoring", "blackbox-exporter")
	if apierrs.IsNotFound(err) || strings.Contains(consoleRoute.Spec.Host, "redhat.com") {
		if err := deploy.DeployManifestsFromPath(r.Client,
			dsciInit,
			filepath.Join(blackBoxPath, "internal"),
			dsciInit.Spec.Monitoring.Namespace,
			"blackbox-exporter",
			dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed); err != nil {
			r.Log.Error(err, "error to deploy manifests: "+err.Error())

			return err
		}
	} else {
		if err := deploy.DeployManifestsFromPath(r.Client,
			dsciInit,
			filepath.Join(blackBoxPath, "external"),
			dsciInit.Spec.Monitoring.Namespace,
			"blackbox-exporter",
			dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed); err != nil {
			r.Log.Error(err, "error to deploy manifests: "+err.Error())

			return err
		}
	}

	return nil
}

func createMonitoringProxySecret(cli client.Client, name string, dsciInit *dsci.DSCInitialization) error {
	sessionSecret, err := GenerateRandomHex(32)
	if err != nil {
		return err
	}

	desiredProxySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: dsciInit.Spec.Monitoring.Namespace,
		},
		Data: map[string][]byte{
			"session_secret": []byte(b64.StdEncoding.EncodeToString(sessionSecret)),
		},
	}

	foundProxySecret := &corev1.Secret{}
	err = cli.Get(context.TODO(), client.ObjectKey{Name: name, Namespace: dsciInit.Spec.Monitoring.Namespace}, foundProxySecret)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// Set Controller reference
			err = ctrl.SetControllerReference(dsciInit, desiredProxySecret, cli.Scheme())
			if err != nil {
				return err
			}
			err = cli.Create(context.TODO(), desiredProxySecret)
			if err != nil && !apierrs.IsAlreadyExists(err) {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}

func (r *DSCInitializationReconciler) configureCommonMonitoring(dsciInit *dsci.DSCInitialization) error {
	// configure segment.io
	segmentPath := filepath.Join(deploy.DefaultManifestPath, "monitoring", "segment")
	if err := deploy.DeployManifestsFromPath(
		r.Client,
		dsciInit,
		segmentPath,
		dsciInit.Spec.ApplicationsNamespace,
		"segment-io",
		dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed); err != nil {
		r.Log.Error(err, "error to deploy manifests under "+segmentPath)

		return err
	}

	// configure monitoring base
	err := common.ReplaceInFile(filepath.Join(deploy.DefaultManifestPath, "monitoring", "base", "rhods-servicemonitor.yaml"),
		map[string]string{
			"<odh_monitoring_project>": dsciInit.Spec.Monitoring.Namespace,
		})
	if err != nil {
		r.Log.Error(err, "error to inject namespace to common monitoring")

		return err
	}
	monitoringBasePath := filepath.Join(deploy.DefaultManifestPath, "monitoring", "base")
	if err = deploy.DeployManifestsFromPath(
		r.Client,
		dsciInit,
		monitoringBasePath,
		dsciInit.Spec.Monitoring.Namespace,
		"monitoring-base",
		dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed); err != nil {
		r.Log.Error(err, "error to deploy manifests under "+monitoringBasePath)

		return err
	}

	return nil
}
