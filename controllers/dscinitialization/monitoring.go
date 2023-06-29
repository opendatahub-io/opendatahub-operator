package dscinitialization

import (
	"context"
	"crypto/sha256"
	b64 "encoding/base64"
	"fmt"
	"strings"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsci "github.com/opendatahub-io/opendatahub-operator/apis/dscinitialization/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/pkg/deploy"
)

func configurePrometheus(dsciInit *dsci.DSCInitialization, r *DSCInitializationReconciler) error {
	// Get alertmanager host
	alertmanagerRoute := &routev1.Route{}
	err := r.Client.Get(context.TODO(), client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "alertmanager",
	}, alertmanagerRoute)

	if err != nil {
		return fmt.Errorf("error getting alertmanager host : %v", err)
	}

	alertManagerConfigMap := &corev1.ConfigMap{}
	err = r.Client.Get(context.TODO(), client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "alertmanager",
	}, alertManagerConfigMap)

	if err != nil {
		return fmt.Errorf("error getting alertmanager configmap : %v", err)
	}

	prometheusConfigMap := &corev1.ConfigMap{}
	err = r.Client.Get(context.TODO(), client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "prometheus",
	}, prometheusConfigMap)

	if err != nil {
		return fmt.Errorf("error getting prometheus configmap : %v", err)
	}

	alertmanagerData, err := getMonitoringData(alertManagerConfigMap.Data["alertmanager.yml"])
	if err != nil {
		return err
	}

	prometheusData, err := getMonitoringData(fmt.Sprint(prometheusConfigMap.Data))
	if err != nil {
		return err
	}

	// Update prometheus manifests
	err = ReplaceStringsInFile(deploy.DefaultManifestPath+"/monitoring/prometheus/prometheus.yaml", map[string]string{
		"<set_alertmanager_host>":    alertmanagerRoute.Spec.Host,
		"<alertmanager_config_hash>": alertmanagerData,
		"<prometheus_config_hash>":   prometheusData,
	})
	if err != nil {
		return err
	}

	err = ReplaceStringsInFile(deploy.DefaultManifestPath+"/monitoring/prometheus/prometheus-viewer-rolebinding.yaml", map[string]string{
		"<odh_monitoring_project>": dsciInit.Spec.Monitoring.Namespace,
	})

	if err != nil {
		return err
	}

	// Deploy manifests
	err = deploy.DeployManifestsFromPath(dsciInit, r.Client,
		deploy.DefaultManifestPath+"/monitoring/prometheus",
		dsciInit.Spec.Monitoring.Namespace, r.Scheme, dsciInit.Spec.Monitoring.Enabled)
	if err != nil {
		return err
	}

	// Create proxy secret
	if err := createMonitoringProxySecret("prometheus-proxy", dsciInit, r.Client, r.Scheme); err != nil {
		return err
	}
	return nil
}

func configureAlertManager(dsciInit *dsci.DSCInitialization, r *DSCInitializationReconciler) error {
	// Get Deadmansnitch secret
	deadmansnitchSecret, err := r.waitForManagedSecret("redhat-rhods-deadmanssnitch", dsciInit.Spec.Monitoring.Namespace)
	if err != nil {
		return fmt.Errorf("error getting deadmansnitch secret: %v", err)
	}

	// Get PagerDuty Secret
	pagerDutySecret, err := r.waitForManagedSecret("redhat-rhods-pagerduty", dsciInit.Spec.Monitoring.Namespace)
	if err != nil {
		return fmt.Errorf("error getting pagerduty secret: %v", err)
	}

	// Get Smtp Secret
	smtpSecret, err := r.waitForManagedSecret("redhat-rhods-smtp", dsciInit.Spec.Monitoring.Namespace)
	if err != nil {
		return fmt.Errorf("error getting smtp secret: %v", err)
	}

	// Replace variables in alertmanager configmap
	// TODO: Following variables can later be exposed by the API
	err = ReplaceStringsInFile(deploy.DefaultManifestPath+"/monitoring/alertmanager/monitoring-configs.yaml",
		map[string]string{
			"<snitch_url>":      b64.StdEncoding.EncodeToString(deadmansnitchSecret.Data["SNITCH_URL"]),
			"<pagerduty_token>": b64.StdEncoding.EncodeToString(pagerDutySecret.Data["PAGERDUTY_KEY"]),
			"<smtp_host>":       b64.StdEncoding.EncodeToString(smtpSecret.Data["host"]),
			"<smtp_port>":       b64.StdEncoding.EncodeToString(smtpSecret.Data["port"]),
			"<smtp_username>":   b64.StdEncoding.EncodeToString(smtpSecret.Data["username"]),
			"<smtp_password>":   b64.StdEncoding.EncodeToString(smtpSecret.Data["password"]),
		})

	if err != nil {
		return err
	}

	err = deploy.DeployManifestsFromPath(dsciInit, r.Client,
		deploy.DefaultManifestPath+"/monitoring/alertmanager",
		dsciInit.Spec.Monitoring.Namespace, r.Scheme, dsciInit.Spec.Monitoring.Enabled)
	if err != nil {
		return err
	}

	// TODO: Add watch for SMTP secret and configure emails

	// Create proxy secret
	if err := createMonitoringProxySecret("alertmanager-proxy", dsciInit, r.Client, r.Scheme); err != nil {
		return err
	}
	return nil
}

func configureBlackboxExporter(dsciInit *dsci.DSCInitialization, cli client.Client, s *runtime.Scheme) error {

	consoleRoute := &routev1.Route{}
	err := cli.Get(context.TODO(), client.ObjectKey{Name: "console", Namespace: "openshift-console"}, consoleRoute)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return err
		}
	}

	if apierrs.IsNotFound(err) || strings.Contains(consoleRoute.Spec.Host, "redhat.com") {
		err := deploy.DeployManifestsFromPath(dsciInit, cli,
			deploy.DefaultManifestPath+"/monitoring/blackbox-exporter/internal",
			dsciInit.Spec.Monitoring.Namespace, s, dsciInit.Spec.Monitoring.Enabled)
		if err != nil {
			return err
		}

	} else {
		err := deploy.DeployManifestsFromPath(dsciInit, cli,
			deploy.DefaultManifestPath+"/monitoring/blackbox-exporter/external",
			dsciInit.Spec.Monitoring.Namespace, s, dsciInit.Spec.Monitoring.Enabled)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *DSCInitializationReconciler) configureManagedMonitoring(dscInit *dsci.DSCInitialization) error {
	// configure Alertmanager
	if err := configureAlertManager(dscInit, r); err != nil {
		fmt.Printf("Error in alertmanager")
		return err
	}

	// configure Prometheus
	if err := configurePrometheus(dscInit, r); err != nil {
		fmt.Printf("Error in prometheus")
		return err
	}

	// configure Blackbox exporter
	if err := configureBlackboxExporter(dscInit, r.Client, r.Scheme); err != nil {
		fmt.Printf("Error in blackbox exporter")
		return err
	}
	return nil
}

func createMonitoringProxySecret(name string, dsciInit *dsci.DSCInitialization, cli client.Client, s *runtime.Scheme) error {

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
			err = ctrl.SetControllerReference(dsciInit, desiredProxySecret, s)
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

func replaceInAlertManagerConfigmap(cli client.Client, dsciInit *dsci.DSCInitialization, cmName, replaceVariable, replaceValue string) error {
	prometheusConfig := &corev1.ConfigMap{}
	err := cli.Get(context.TODO(), client.ObjectKey{Name: cmName, Namespace: dsciInit.Spec.Monitoring.Namespace}, prometheusConfig)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil
		}
		return err
	}
	prometheusAlertmanagerContent := prometheusConfig.Data["alertmanager.yml"]
	prometheusAlertmanagerContent = strings.ReplaceAll(prometheusAlertmanagerContent, replaceVariable, replaceValue)

	prometheusConfig.Data["alertmanager.yml"] = prometheusAlertmanagerContent
	return cli.Update(context.TODO(), prometheusConfig)
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
