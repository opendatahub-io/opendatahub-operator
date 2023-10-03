package dscinitialization

import (
	"context"
	b64 "encoding/base64"
	"fmt"

	"path/filepath"
	"strings"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// "k8s.io/apimachinery/pkg/runtime"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
// if reconcile from monitoring, initial set to false, skip blackbox and rolebinding
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
	r.Log.Info("Success: got alertmanager route")

	// Get alertmanager configmap
	alertManagerConfigMap := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "alertmanager",
	}, alertManagerConfigMap)
	if err != nil {
		r.Log.Error(err, "error to get alertmanager configmap")
		return err
	}
	r.Log.Info("Success: got alertmanager CM")

	alertmanagerData, err := getMonitoringData(alertManagerConfigMap.Data["alertmanager.yml"])
	if err != nil {
		r.Log.Error(err, "error to get alertmanager data from alertmanager.yaml")
		return err
	}
	r.Log.Info("Success: read alertmanager data from alertmanage.yaml from CM")

	prometheusManifestsPath := filepath.Join(deploy.DefaultManifestPath, "monitoring", "prometheus")

	// Deploy prometheus CM first
	basePath := filepath.Join(prometheusManifestsPath, "base")
	err = deploy.DeployManifestsFromPath(
		r.Client, dsciInit,
		basePath,
		dsciInit.Spec.Monitoring.Namespace,
		"prometheus",
		dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed)
	if err != nil {
		r.Log.Error(err, "error to deploy manifests", "path", basePath)
		return err
	}
	r.Log.Info("Success: deploy prometheus CM")

	// Get prometheus configmap
	prometheusConfigMap := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "prometheus",
	}, prometheusConfigMap)
	if err != nil {
		r.Log.Error(err, "error to get prometheus configmap")
		return err
	}
	r.Log.Info("Success: got prometheus CM")

	// Get prometheus data
	prometheusData, err := getMonitoringData(fmt.Sprint(prometheusConfigMap.Data))
	if err != nil {
		r.Log.Error(err, "error to get prometheus data")
		return err
	}
	r.Log.Info("Success: read prometheus data from prometheus.yaml from CM")

	// Update prometheus manifests
	err = common.ReplaceStringsInFile(filepath.Join(prometheusManifestsPath, "prometheus.yaml"),
		map[string]string{
			"<set_alertmanager_host>":    alertmanagerRoute.Spec.Host,
			"<alertmanager_config_hash>": alertmanagerData,
			"<prometheus_config_hash>":   prometheusData,
		})
	if err != nil {
		r.Log.Error(err, "error to inject data to prometheus.yaml manifests")
		return err
	}

	err = common.ReplaceStringsInFile(filepath.Join(prometheusManifestsPath, "prometheus-viewer-rolebinding.yaml"),
		map[string]string{
			"<odh_monitoring_project>": dsciInit.Spec.Monitoring.Namespace,
		})
	if err != nil {
		r.Log.Error(err, "error to inject data to prometheus-viewer-rolebinding.yaml")
		return err
	}

	// Deploy manifests
	err = deploy.DeployManifestsFromPath(
		r.Client,
		dsciInit,
		prometheusManifestsPath,
		dsciInit.Spec.Monitoring.Namespace,
		"prometheus",
		dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed)
	if err != nil {
		r.Log.Error(err, "error to deploy manifests", "path", prometheusManifestsPath)
		return err
	}

	// Create proxy secret
	if err := createMonitoringProxySecret(r.Client, "prometheus-proxy", dsciInit); err != nil {
		return err
	}
	return nil
}

func configureAlertManager(ctx context.Context, dsciInit *dsci.DSCInitialization, r *DSCInitializationReconciler) error {
	// Get Deadmansnitch secret
	deadmansnitchSecret, err := r.waitForManagedSecret(ctx, "redhat-rhods-deadmanssnitch", dsciInit.Spec.Monitoring.Namespace)
	if err != nil {
		r.Log.Error(err, "error getting deadmansnitch secret from namespace "+dsciInit.Spec.Monitoring.Namespace)
		return err
	}
	r.Log.Info("Success: got deadmansnitch secret")

	// Get PagerDuty Secret
	pagerDutySecret, err := r.waitForManagedSecret(ctx, "redhat-rhods-pagerduty", dsciInit.Spec.Monitoring.Namespace)
	if err != nil {
		r.Log.Error(err, "error getting pagerduty secret from namespace "+dsciInit.Spec.Monitoring.Namespace)
		return err
	}
	r.Log.Info("Success: got pagerduty secret")

	// Get Smtp Secret
	smtpSecret, err := r.waitForManagedSecret(ctx, "redhat-rhods-smtp", dsciInit.Spec.Monitoring.Namespace)
	if err != nil {
		r.Log.Error(err, "error getting smtp secret from namespace "+dsciInit.Spec.Monitoring.Namespace)
		return err
	}
	r.Log.Info("Success: got smtp secret")

	// Get SMTP receiver email secret (assume operator namespace for managed service is not configurable)
	smtpEmailSecret, err := r.waitForManagedSecret(ctx, "addon-managed-odh-parameters", "redhat-ods-operator")
	if err != nil {
		return fmt.Errorf("error getting smtp receiver email secret: %w", err)
	}
	r.Log.Info("Success: got smpt email secret")

	// Replace variables in alertmanager configmap
	// TODO: Following variables can later be exposed by the API
	err = common.ReplaceStringsInFile(filepath.Join(deploy.DefaultManifestPath, "monitoring", "alertmanager", "alertmanager-configs.yaml"),
		map[string]string{
			"<snitch_url>":      b64.StdEncoding.EncodeToString(deadmansnitchSecret.Data["SNITCH_URL"]),
			"<pagerduty_token>": b64.StdEncoding.EncodeToString(pagerDutySecret.Data["PAGERDUTY_KEY"]),
			"<smtp_host>":       b64.StdEncoding.EncodeToString(smtpSecret.Data["host"]),
			"<smtp_port>":       b64.StdEncoding.EncodeToString(smtpSecret.Data["port"]),
			"<smtp_username>":   b64.StdEncoding.EncodeToString(smtpSecret.Data["username"]),
			"<smtp_password>":   b64.StdEncoding.EncodeToString(smtpSecret.Data["password"]),
			"<user_emails>":     b64.StdEncoding.EncodeToString(smtpEmailSecret.Data["notification-email"]),
			"@devshift.net":     "@rhmw.io",
		})
	if err != nil {
		r.Log.Error(err, "error to inject data to alertmanager-configs.yaml")
		return err
	}
	r.Log.Info("Success: generate alertmanage config")

	alertManagerPath := filepath.Join(deploy.DefaultManifestPath, "monitoring", "alertmanager")
	err = deploy.DeployManifestsFromPath(
		r.Client,
		dsciInit,
		alertManagerPath,
		dsciInit.Spec.Monitoring.Namespace,
		"alertmanager",
		dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed)
	if err != nil {
		r.Log.Error(err, "error to deploy manifests", "path", alertManagerPath)
		return err
	}
	r.Log.Info("Success: deploy alertmanager manifests")

	// Create proxy secret
	if err := createMonitoringProxySecret(r.Client, "alertmanager-proxy", dsciInit); err != nil {
		r.Log.Error(err, "error to create secret alertmanager-proxy")
		return err
	}
	r.Log.Info("Success: create alertmanage secret")
	return nil
}

func configureBlackboxExporter(cli client.Client, dsciInit *dsci.DSCInitialization) error {
	consoleRoute := &routev1.Route{}
	err := cli.Get(context.TODO(), client.ObjectKey{Name: "console", Namespace: "openshift-console"}, consoleRoute)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return err
		}
	}


	blackBoxPath := filepath.Join(deploy.DefaultManifestPath, "monitoring", "blackbox-exporter")
	if apierrs.IsNotFound(err) || strings.Contains(consoleRoute.Spec.Host, "redhat.com") {
		err := deploy.DeployManifestsFromPath(
			cli,
			dsciInit,
			filepath.Join(blackBoxPath, "internal"),
			dsciInit.Spec.Monitoring.Namespace,
			"blackbox-exporter",
			dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed)
		if err != nil {
			return fmt.Errorf("error to deploy manifests: %w", err)
		}
	} else {
		err := deploy.DeployManifestsFromPath(
			cli, dsciInit,
			filepath.Join(blackBoxPath, "external"),
			dsciInit.Spec.Monitoring.Namespace,
			"blackbox-exporter",
			dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed)
		if err != nil {
			return fmt.Errorf("error to deploy manifests: %w", err)
		}
	}
	return nil
}

func (r *DSCInitializationReconciler) configureManagedMonitoring(ctx context.Context, dscInit *dsci.DSCInitialization) error {
	// configure Alertmanager
	if err := configureAlertManager(ctx, dscInit, r); err != nil {
		return fmt.Errorf("error in configureAlertManager: %w", err)
	}

	// configure Prometheus
	if err := configurePrometheus(ctx, dscInit, r); err != nil {
		return fmt.Errorf("error in configurePrometheus: %w", err)
	}

	if initial == "init" {
		err := common.UpdatePodSecurityRolebinding(r.Client, []string{"redhat-ods-monitoring"}, dscInit.Spec.Monitoring.Namespace)
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
	err = common.ReplaceStringsInFile(filepath.Join(alertManagerPath, "alertmanager-configs.yaml"),
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

	// Get SMTP receiver email secret (assume operator namespace for managed service is not configurable)
	smtpEmailSecret, err := r.waitForManagedSecret(ctx, "addon-managed-odh-parameters", "redhat-ods-operator")
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

func configurePrometheus(ctx context.Context, dsciInit *dsci.DSCInitialization, r *DSCInitializationReconciler) error {
	// Update rolebinding-viewer
	err := common.ReplaceStringsInFile(filepath.Join(prometheusManifestsPath, "prometheus-rolebinding-viewer.yaml"),
		map[string]string{
			"<odh_monitoring_project>": dsciInit.Spec.Monitoring.Namespace,
		})
	if err != nil {
		r.Log.Error(err, "error to inject data to prometheus-rolebinding-viewer.yaml")
		return err
	}
	// Update prometheus-config for dashboard, dsp and workbench
	consolelinkDomain, err := common.GetDomain(r.Client, NameConsoleLink, NamespaceConsoleLink)
	if err != nil {
		return fmt.Errorf("error getting console route URL : %v", err)
	} else {
		err = common.ReplaceStringsInFile(filepath.Join(prometheusConfigPath, "prometheus-configs.yaml"),
			map[string]string{
				"<odh_application_namespace>": dsciInit.Spec.ApplicationsNamespace,
				"<odh_monitoring_project>":    dsciInit.Spec.Monitoring.Namespace,
				"<console_domain>":            consolelinkDomain,
			})
		if err != nil {
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
	// r.Log.Info("Success: create prometheus configmap 'prometheus'")

	// Get prometheus configmap
	prometheusConfigMap := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "prometheus",
	}, prometheusConfigMap)
	if err != nil {
		r.Log.Error(err, "error to get configmap 'prometheus'")
		return err
	}
	// r.Log.Info("Success: got prometheus configmap")

	// Get encoded prometheus data from configmap 'prometheus'
	prometheusData, err := common.GetMonitoringData(fmt.Sprint(prometheusConfigMap.Data))
	if err != nil {
		r.Log.Error(err, "error to get prometheus data")
		return err
	}
	// r.Log.Info("Success: read encoded prometheus data from prometheus.yml in configmap")

	// Get alertmanager host
	alertmanagerRoute := &routev1.Route{}
	err = r.Client.Get(ctx, client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "alertmanager",
	}, alertmanagerRoute)
	if err != nil {
		r.Log.Error(err, "error to get alertmanager route")
		return err
	}
	// r.Log.Info("Success: got alertmanager route")

	// Get alertmanager configmap
	alertManagerConfigMap := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "alertmanager",
	}, alertManagerConfigMap)
	if err != nil {
		r.Log.Error(err, "error to get configmap 'alertmanager'")
		return err
	}
	// r.Log.Info("Success: got configmap 'alertmanager'")

	alertmanagerData, err := common.GetMonitoringData(alertManagerConfigMap.Data["alertmanager.yml"])
	if err != nil {
		r.Log.Error(err, "error to get encoded alertmanager data from alertmanager.yml")
		return err
	}
	// r.Log.Info("Success: read alertmanager data from alertmanage.yml")

	// Update prometheus deployment with alertmanager and prometheus data
	err = common.ReplaceStringsInFile(filepath.Join(prometheusManifestsPath, "prometheus-deployment.yaml"),
		map[string]string{
			"<set_alertmanager_host>": alertmanagerRoute.Spec.Host,
		})
	if err != nil {
		r.Log.Error(err, "error to inject set_alertmanager_host to prometheus-deployment.yaml")
		return err
	}
	// r.Log.Info("Success: update set_alertmanager_host in prometheus-deployment.yaml")
	err = common.MatchLineInFile(filepath.Join(prometheusManifestsPath, "prometheus-deployment.yaml"),
		map[string]string{
			"alertmanager: ": "alertmanager: " + alertmanagerData,
			"prometheus: ":   "prometheus: " + prometheusData,
		})
	if err != nil {
		r.Log.Error(err, "error to update annotations in prometheus-deployment.yaml")
		return err
	}
	// r.Log.Info("Success: update annotations in prometheus-deployment.yaml")

	// final apply prometheus manifests including prometheus deployment
	err = deploy.DeployManifestsFromPath(r.Client, dsciInit, prometheusManifestsPath, dsciInit.Spec.Monitoring.Namespace, "prometheus", true)
	if err != nil {
		r.Log.Error(err, "error to deploy manifests for prometheus", "path", prometheusManifestsPath)
		return err
	}

	// Create prometheus-proxy secret
	if err := createMonitoringProxySecret(r.Client, "prometheus-proxy", dsciInit); err != nil {
		return err
	}
	// r.Log.Info("Success: create prometheus-proxy secret")
	return nil
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
			r.Log.Error(err, "error to deploy manifests: %w", err)
			return err
		}
	} else {
		if err := deploy.DeployManifestsFromPath(r.Client,
			dsciInit,
			filepath.Join(blackBoxPath, "external"),
			dsciInit.Spec.Monitoring.Namespace,
			"blackbox-exporter",
			dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed); err != nil {
			r.Log.Error(err, "error to deploy manifests: %w", err)
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

func getMonitoringData(data string) (string, error) {
	// Create a new SHA-256 hash object
	hash := sha256.New()

	// Write the input data to the hash object
	_, err := hash.Write([]byte(data))
	if err != nil {
		return "", err
	}

	// Get the computed hash sum
	hashSum := hash.Sum(nil)

	// Encode the hash sum to Base64
	encodedData := b64.StdEncoding.EncodeToString(hashSum)

	return encodedData, nil
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
	monitoringBasePath := filepath.Join(deploy.DefaultManifestPath, "monitoring", "base")
	if err := deploy.DeployManifestsFromPath(

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
