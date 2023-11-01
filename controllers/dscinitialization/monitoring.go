package dscinitialization

import (
	"context"
	"crypto/sha256"
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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

func configurePrometheus(ctx context.Context, dsciInit *dsci.DSCInitialization, r *DSCInitializationReconciler) error {
	// Get alertmanager host
	alertmanagerRoute := &routev1.Route{}
	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "alertmanager",
	}, alertmanagerRoute)
	if err != nil {
		r.Log.Error(err, "error to get alertmanager route")
		return err
	}
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

	// configure Blackbox exporter
	if err := configureBlackboxExporter(r.Client, dscInit); err != nil {
		return fmt.Errorf("error in configureBlackboxExporter: %w", err)
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
	err := deploy.DeployManifestsFromPath(
		r.Client,
		dsciInit,
		segmentPath,
		dsciInit.Spec.Monitoring.Namespace,
		"segment-io",
		dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed)
	if err != nil {
		r.Log.Error(err, "error to deploy manifests under /opt/manifests/monitoring/segment")
		return err
	}
	// configure monitoring base
	monitoringBasePath := filepath.Join(deploy.DefaultManifestPath, "monitoring", "base")
	err = deploy.DeployManifestsFromPath(
		r.Client,
		dsciInit,
		monitoringBasePath,
		dsciInit.Spec.Monitoring.Namespace,
		"monitoring-base",
		dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed)
	if err != nil {
		r.Log.Error(err, "error to deploy manifests under /opt/manifests/monitoring/base")
		return err
	}
	return nil
}
