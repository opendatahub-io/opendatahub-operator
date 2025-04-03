package dscinitialization

import (
	"context"
	b64 "encoding/base64"
	"fmt"
	"path/filepath"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

var (
	ComponentName           = "monitoring"
	alertManagerPath        = filepath.Join(deploy.DefaultManifestPath, ComponentName, "alertmanager")
	prometheusManifestsPath = filepath.Join(deploy.DefaultManifestPath, ComponentName, "prometheus", "base")
	prometheusConfigPath    = filepath.Join(deploy.DefaultManifestPath, ComponentName, "prometheus", "apps")
	networkpolicyPath       = filepath.Join(deploy.DefaultManifestPath, ComponentName, "networkpolicy")
)

// only when reconcile on DSCI CR, initial set to true
// if reconcile from monitoring, initial set to false, skip blackbox and rolebinding.
func (r *DSCInitializationReconciler) configureManagedMonitoring(ctx context.Context, dscInit *dsciv1.DSCInitialization, initial string) error {
	log := logf.FromContext(ctx)
	if initial == "init" {
		// configure Blackbox exporter
		if err := configureBlackboxExporter(ctx, dscInit, r); err != nil {
			return fmt.Errorf("error in configureBlackboxExporter: %w", err)
		}
	}
	if initial == "revertbackup" {
		// TODO: implement with a better solution
		// to have - before component name is to filter out the real rules file line
		// e.g line of "workbenches-recording.rules: |"
		err := common.MatchLineInFile(filepath.Join(prometheusConfigPath, "prometheus-configs.yaml"),
			map[string]string{
				"(.*)-(.*)workbenches(.*).rules":                     "",
				"(.*)-(.*)rhods-dashboard(.*).rules":                 "",
				"(.*)-(.*)codeflare(.*).rules":                       "",
				"(.*)-(.*)data-science-pipelines-operator(.*).rules": "",
				"(.*)-(.*)model-mesh(.*).rules":                      "",
				"(.*)-(.*)odh-model-controller(.*).rules":            "",
				"(.*)-(.*)ray(.*).rules":                             "",
				"(.*)-(.*)trustyai(.*).rules":                        "",
				"(.*)-(.*)kueue(.*).rules":                           "",
				"(.*)-(.*)trainingoperator(.*).rules":                "",
			})
		if err != nil {
			log.Error(err, "error to remove previous enabled component rules")
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
		err := cluster.UpdatePodSecurityRolebinding(ctx, r.Client, dscInit.Spec.Monitoring.Namespace, "redhat-ods-monitoring")
		if err != nil {
			return fmt.Errorf("error to update monitoring security rolebinding: %w", err)
		}
	}

	log.Info("Success: finish config managed monitoring stack!")
	return nil
}

func configureAlertManager(ctx context.Context, dsciInit *dsciv1.DSCInitialization, r *DSCInitializationReconciler) error {
	log := logf.FromContext(ctx)
	// Get Deadmansnitch secret
	deadmansnitchSecret, err := r.waitForManagedSecret(ctx, "redhat-rhods-deadmanssnitch", dsciInit.Spec.Monitoring.Namespace)
	if err != nil {
		log.Error(err, "error getting deadmansnitch secret from namespace "+dsciInit.Spec.Monitoring.Namespace)
		return err
	}
	// log.Info("Success: got deadmansnitch secret")

	// Get PagerDuty Secret
	pagerDutySecret, err := r.waitForManagedSecret(ctx, "redhat-rhods-pagerduty", dsciInit.Spec.Monitoring.Namespace)
	if err != nil {
		log.Error(err, "error getting pagerduty secret from namespace "+dsciInit.Spec.Monitoring.Namespace)
		return err
	}
	// log.Info("Success: got pagerduty secret")

	// Get Smtp Secret
	smtpSecret, err := r.waitForManagedSecret(ctx, "redhat-rhods-smtp", dsciInit.Spec.Monitoring.Namespace)
	if err != nil {
		log.Error(err, "error getting smtp secret from namespace "+dsciInit.Spec.Monitoring.Namespace)
		return err
	}
	// log.Info("Success: got smtp secret")

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
		})
	if err != nil {
		log.Error(err, "error to inject data to alertmanager-configs.yaml")
		return err
	}
	// log.Info("Success: inject alertmanage-configs.yaml")

	// special handling for dev-mod
	consolelinkDomain, err := cluster.GetDomain(ctx, r.Client)
	if err != nil {
		return fmt.Errorf("error getting console route URL : %w", err)
	}
	if strings.Contains(consolelinkDomain, "devshift.org") {
		log.Info("inject alertmanage-configs.yaml for dev mode1")
		err = common.ReplaceStringsInFile(filepath.Join(alertManagerPath, "alertmanager-configs.yaml"),
			map[string]string{
				"@devshift.net": "@rhmw.io",
			})
		if err != nil {
			log.Error(err, "error to replace data for dev mode1 to alertmanager-configs.yaml")
			return err
		}
	}
	if strings.Contains(consolelinkDomain, "aisrhods") {
		log.Info("inject alertmanage-configs.yaml for dev mode2")
		err = common.ReplaceStringsInFile(filepath.Join(alertManagerPath, "alertmanager-configs.yaml"),
			map[string]string{
				"receiver: PagerDuty": "receiver: alerts-sink",
			})
		if err != nil {
			log.Error(err, "error to replace data for dev mode2 to alertmanager-configs.yaml")
			return err
		}
	}

	// log.Info("Success: inject alertmanage-configs.yaml for dev mode")

	operatorNs, err := cluster.GetOperatorNamespace()
	if err != nil {
		log.Error(err, "error getting operator namespace for smtp secret")
		return err
	}

	// Get SMTP receiver email secret (assume operator namespace for managed service is not configurable)
	smtpEmailSecret, err := r.waitForManagedSecret(ctx, "addon-managed-odh-parameters", operatorNs)
	if err != nil {
		return fmt.Errorf("error getting smtp receiver email secret: %w", err)
	}
	// log.Info("Success: got smpt email secret")
	// replace smtpEmailSecret in alertmanager-configs.yaml
	if err = common.MatchLineInFile(filepath.Join(alertManagerPath, "alertmanager-configs.yaml"),
		map[string]string{
			"- to: ": "- to: " + string(smtpEmailSecret.Data["notification-email"]),
		},
	); err != nil {
		log.Error(err, "error to update with new notification-email")
		return err
	}
	// log.Info("Success: update alertmanage-configs.yaml with email")
	err = deploy.DeployManifestsFromPath(ctx, r.Client, dsciInit, alertManagerPath, dsciInit.Spec.Monitoring.Namespace, "alertmanager", true)
	if err != nil {
		log.Error(err, "error to deploy manifests", "path", alertManagerPath)
		return err
	}
	// log.Info("Success: update alertmanager with manifests")

	// Create alertmanager-proxy secret
	if err := createMonitoringProxySecret(ctx, r.Client, "alertmanager-proxy", dsciInit); err != nil {
		log.Error(err, "error to create secret alertmanager-proxy")
		return err
	}
	// log.Info("Success: create alertmanager-proxy secret")
	return nil
}

func configurePrometheus(ctx context.Context, dsciInit *dsciv1.DSCInitialization, r *DSCInitializationReconciler) error {
	log := logf.FromContext(ctx)
	// Update rolebinding-viewer
	err := common.ReplaceStringsInFile(filepath.Join(prometheusManifestsPath, "prometheus-rolebinding-viewer.yaml"),
		map[string]string{
			"<odh_monitoring_project>": dsciInit.Spec.Monitoring.Namespace,
		})
	if err != nil {
		log.Error(err, "error to inject data to prometheus-rolebinding-viewer.yaml")
		return err
	}
	// Update prometheus-config for dashboard, dsp and workbench
	consolelinkDomain, err := cluster.GetDomain(ctx, r.Client)
	if err != nil {
		return fmt.Errorf("error getting console route URL : %w", err)
	}
	err = common.ReplaceStringsInFile(filepath.Join(prometheusConfigPath, "prometheus-configs.yaml"),
		map[string]string{
			"<odh_application_namespace>": dsciInit.Spec.ApplicationsNamespace,
			"<odh_monitoring_project>":    dsciInit.Spec.Monitoring.Namespace,
			"<console_domain>":            consolelinkDomain,
		})
	if err != nil {
		log.Error(err, "error to inject data to prometheus-configs.yaml")
		return err
	}

	// Deploy prometheus manifests from prometheus/apps
	if err = deploy.DeployManifestsFromPath(
		ctx,
		r.Client,
		dsciInit,
		prometheusConfigPath,
		dsciInit.Spec.Monitoring.Namespace,
		"prometheus",
		dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed); err != nil {
		log.Error(err, "error to deploy manifests for prometheus configs", "path", prometheusConfigPath)
		return err
	}
	// log.Info("Success: create prometheus configmap 'prometheus'")

	// Get prometheus configmap
	prometheusConfigMap := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "prometheus",
	}, prometheusConfigMap)
	if err != nil {
		log.Error(err, "error to get configmap 'prometheus'")
		return err
	}
	// log.Info("Success: got prometheus configmap")

	// Get encoded prometheus data from configmap 'prometheus'
	prometheusData, err := common.GetMonitoringData(fmt.Sprint(prometheusConfigMap.Data))
	if err != nil {
		log.Error(err, "error to get prometheus data")
		return err
	}
	// log.Info("Success: read encoded prometheus data from prometheus.yml in configmap")

	// Get alertmanager host
	alertmanagerRoute := &routev1.Route{}
	err = r.Client.Get(ctx, client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "alertmanager",
	}, alertmanagerRoute)
	if err != nil {
		log.Error(err, "error to get alertmanager route")
		return err
	}
	// log.Info("Success: got alertmanager route")

	// Get alertmanager configmap
	alertManagerConfigMap := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "alertmanager",
	}, alertManagerConfigMap)
	if err != nil {
		log.Error(err, "error to get configmap 'alertmanager'")
		return err
	}
	// log.Info("Success: got configmap 'alertmanager'")

	alertmanagerData, err := common.GetMonitoringData(alertManagerConfigMap.Data["alertmanager.yml"])
	if err != nil {
		log.Error(err, "error to get encoded alertmanager data from alertmanager.yml")
		return err
	}
	// log.Info("Success: read alertmanager data from alertmanage.yml")

	// Update prometheus deployment with alertmanager and prometheus data
	err = common.ReplaceStringsInFile(filepath.Join(prometheusManifestsPath, "prometheus-deployment.yaml"),
		map[string]string{
			"<set_alertmanager_host>": alertmanagerRoute.Spec.Host,
		})
	if err != nil {
		log.Error(err, "error to inject set_alertmanager_host to prometheus-deployment.yaml")
		return err
	}
	// log.Info("Success: update set_alertmanager_host in prometheus-deployment.yaml")
	err = common.MatchLineInFile(filepath.Join(prometheusManifestsPath, "prometheus-deployment.yaml"),
		map[string]string{
			"alertmanager: ": "alertmanager: " + alertmanagerData,
			"prometheus: ":   "prometheus: " + prometheusData,
		})
	if err != nil {
		log.Error(err, "error to update annotations in prometheus-deployment.yaml")
		return err
	}
	// log.Info("Success: update annotations in prometheus-deployment.yaml")

	// final apply prometheus manifests including prometheus deployment
	// Check if Prometheus deployment from legacy version exists(check for initContainer)
	// Need to delete wait-for-deployment initContainer
	existingPromDep := &appsv1.Deployment{}
	err = r.Client.Get(ctx, client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "prometheus",
	}, existingPromDep)
	if err != nil {
		if !k8serr.IsNotFound(err) {
			return err
		}
	}
	if len(existingPromDep.Spec.Template.Spec.InitContainers) > 0 {
		err = r.Client.Delete(ctx, existingPromDep)
		if err != nil {
			return fmt.Errorf("error deleting legacy prometheus deployment %w", err)
		}
	}

	err = deploy.DeployManifestsFromPath(ctx, r.Client, dsciInit, prometheusManifestsPath,
		dsciInit.Spec.Monitoring.Namespace, "prometheus", true)
	if err != nil {
		log.Error(err, "error to deploy manifests for prometheus", "path", prometheusManifestsPath)
		return err
	}

	// Create prometheus-proxy secret
	if err := createMonitoringProxySecret(ctx, r.Client, "prometheus-proxy", dsciInit); err != nil {
		return err
	}
	// log.Info("Success: create prometheus-proxy secret")
	return nil
}

func configureBlackboxExporter(ctx context.Context, dsciInit *dsciv1.DSCInitialization, r *DSCInitializationReconciler) error {
	log := logf.FromContext(ctx)
	consoleRoute := &routev1.Route{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: cluster.NameConsoleLink, Namespace: cluster.NamespaceConsoleLink}, consoleRoute)
	if err != nil {
		if !k8serr.IsNotFound(err) {
			return err
		}
	}

	// Check if Blackbox exporter deployment from legacy version exists(check for initContainer)
	// Need to delete wait-for-deployment initContainer
	existingBlackboxExp := &appsv1.Deployment{}
	err = r.Client.Get(ctx, client.ObjectKey{
		Namespace: dsciInit.Spec.Monitoring.Namespace,
		Name:      "blackbox-exporter",
	}, existingBlackboxExp)
	if err != nil {
		if !k8serr.IsNotFound(err) {
			return err
		}
	}
	if len(existingBlackboxExp.Spec.Template.Spec.InitContainers) > 0 {
		err = r.Client.Delete(ctx, existingBlackboxExp)
		if err != nil {
			return fmt.Errorf("error deleting legacy blackbox deployment %w", err)
		}
	}

	blackBoxPath := filepath.Join(deploy.DefaultManifestPath, "monitoring", "blackbox-exporter")
	if k8serr.IsNotFound(err) || strings.Contains(consoleRoute.Spec.Host, "redhat.com") {
		if err := deploy.DeployManifestsFromPath(ctx, r.Client,
			dsciInit,
			filepath.Join(blackBoxPath, "internal"),
			dsciInit.Spec.Monitoring.Namespace,
			"blackbox-exporter",
			dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed); err != nil {
			log.Error(err, "error to deploy manifests: %w", "error", err)
			return err
		}
	} else {
		if err := deploy.DeployManifestsFromPath(ctx, r.Client,
			dsciInit,
			filepath.Join(blackBoxPath, "external"),
			dsciInit.Spec.Monitoring.Namespace,
			"blackbox-exporter",
			dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed); err != nil {
			log.Error(err, "error to deploy manifests: %w", "error", err)
			return err
		}
	}
	return nil
}

func createMonitoringProxySecret(ctx context.Context, cli client.Client, name string, dsciInit *dsciv1.DSCInitialization) error {
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
	err = cli.Get(ctx, client.ObjectKeyFromObject(desiredProxySecret), foundProxySecret)
	if err != nil {
		if k8serr.IsNotFound(err) {
			// Set Controller reference
			err = ctrl.SetControllerReference(dsciInit, desiredProxySecret, cli.Scheme())
			if err != nil {
				return err
			}
			err = cli.Create(ctx, desiredProxySecret)
			if err != nil && !k8serr.IsAlreadyExists(err) {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

func (r *DSCInitializationReconciler) configureSegmentIO(ctx context.Context, dsciInit *dsciv1.DSCInitialization) error {
	log := logf.FromContext(ctx)
	// create segment.io only when configmap does not exist in the cluster
	segmentioConfigMap := &corev1.ConfigMap{}
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: dsciInit.Spec.ApplicationsNamespace,
		Name:      "odh-segment-key-config",
	}, segmentioConfigMap); err != nil {
		if !k8serr.IsNotFound(err) {
			log.Error(err, "error to get configmap 'odh-segment-key-config'")
			return err
		} else {
			segmentPath := filepath.Join(deploy.DefaultManifestPath, "monitoring", "segment")
			if err := deploy.DeployManifestsFromPath(
				ctx,
				r.Client,
				dsciInit,
				segmentPath,
				dsciInit.Spec.ApplicationsNamespace,
				"segment-io",
				dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed); err != nil {
				log.Error(err, "error to deploy manifests under "+segmentPath)
				return err
			}
		}
	}
	return nil
}

func (r *DSCInitializationReconciler) configureCommonMonitoring(ctx context.Context, dsciInit *dsciv1.DSCInitialization) error {
	log := logf.FromContext(ctx)
	if err := r.configureSegmentIO(ctx, dsciInit); err != nil {
		return err
	}

	// configure monitoring base
	monitoringBasePath := filepath.Join(deploy.DefaultManifestPath, "monitoring", "base")
	err := common.ReplaceStringsInFile(filepath.Join(monitoringBasePath, "rhods-servicemonitor.yaml"),
		map[string]string{
			"<odh_monitoring_project>": dsciInit.Spec.Monitoring.Namespace,
		})
	if err != nil {
		log.Error(err, "error to inject namespace to common monitoring")

		return err
	}
	// do not set monitoring namespace here, it is hardcoded by manifests
	if err := deploy.DeployManifestsFromPath(
		ctx,
		r.Client,
		dsciInit,
		monitoringBasePath,
		"",
		"monitoring-base",
		dsciInit.Spec.Monitoring.ManagementState == operatorv1.Managed); err != nil {
		log.Error(err, "error to deploy manifests under "+monitoringBasePath)
		return err
	}
	return nil
}
