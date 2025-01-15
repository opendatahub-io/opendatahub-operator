package monitoring

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

var (
	prometheusConfigPath = filepath.Join(deploy.DefaultManifestPath, "monitoring", "prometheus", "apps", "prometheus-configs.yaml")
)

// UpdatePrometheusConfig update prometheus-configs.yaml to include/exclude <component>.rules
// parameter enable when set to true to add new rules, when set to false to remove existing rules.
func UpdatePrometheusConfig(ctx context.Context, _ client.Client, enable bool, component string) error {
	l := logf.FromContext(ctx)

	// create a struct to mock poremtheus.yml
	type ConfigMap struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Metadata   struct {
			Name      string `yaml:"name"`
			Namespace string `yaml:"namespace"`
		} `yaml:"metadata"`
		Data struct {
			PrometheusYML          string `yaml:"prometheus.yml"`
			OperatorRules          string `yaml:"operator-recording.rules"`
			DeadManSnitchRules     string `yaml:"deadmanssnitch-alerting.rules"`
			CFRRules               string `yaml:"codeflare-recording.rules"`
			CRARules               string `yaml:"codeflare-alerting.rules"`
			DashboardRRules        string `yaml:"rhods-dashboard-recording.rules"`
			DashboardARules        string `yaml:"rhods-dashboard-alerting.rules"`
			DSPRRules              string `yaml:"data-science-pipelines-operator-recording.rules"`
			DSPARules              string `yaml:"data-science-pipelines-operator-alerting.rules"`
			MMRRules               string `yaml:"model-mesh-recording.rules"`
			MMARules               string `yaml:"model-mesh-alerting.rules"`
			OdhModelRRules         string `yaml:"odh-model-controller-recording.rules"`
			OdhModelARules         string `yaml:"odh-model-controller-alerting.rules"`
			CFORRules              string `yaml:"codeflare-recording.rules"`
			CFOARules              string `yaml:"codeflare-alerting.rules"`
			RayARules              string `yaml:"ray-alerting.rules"`
			WorkbenchesRRules      string `yaml:"workbenches-recording.rules"`
			WorkbenchesARules      string `yaml:"workbenches-alerting.rules"`
			KserveRRules           string `yaml:"kserve-recording.rules"`
			KserveARules           string `yaml:"kserve-alerting.rules"`
			TrustyAIRRules         string `yaml:"trustyai-recording.rules"`
			TrustyAIARules         string `yaml:"trustyai-alerting.rules"`
			KueueRRules            string `yaml:"kueue-recording.rules"`
			KueueARules            string `yaml:"kueue-alerting.rules"`
			TrainingOperatorRRules string `yaml:"trainingoperator-recording.rules"`
			TrainingOperatorARules string `yaml:"trainingoperator-alerting.rules"`
			ModelRegistryRRules    string `yaml:"model-registry-operator-recording.rules"`
			ModelRegistryARules    string `yaml:"model-registry-operator-alerting.rules"`
		} `yaml:"data"`
	}

	var configMap ConfigMap
	// prometheusContent will represent content of prometheus.yml due to its dynamic struct
	var prometheusContent map[interface{}]interface{}

	// read prometheus.yml from local disk /opt/mainfests/monitoring/prometheus/apps/
	yamlData, err := os.ReadFile(prometheusConfigPath)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(yamlData, &configMap); err != nil {
		return err
	}

	// get prometheus.yml part from configmap
	if err := yaml.Unmarshal([]byte(configMap.Data.PrometheusYML), &prometheusContent); err != nil {
		return err
	}

	// to add component rules when it is not there yet
	if enable {
		// Check if the rule not yet exists in rule_files
		if !strings.Contains(configMap.Data.PrometheusYML, component+"*.rules") {
			// check if have rule_files
			if ruleFiles, ok := prometheusContent["rule_files"]; ok {
				if ruleList, isList := ruleFiles.([]interface{}); isList {
					// add new component rules back to rule_files
					ruleList = append(ruleList, component+"*.rules")
					prometheusContent["rule_files"] = ruleList
				}
			}
		}
	} else { // to remove component rules if it is there
		l.Info("Removing prometheus rule: " + component + "*.rules")
		if ruleList, ok := prometheusContent["rule_files"].([]interface{}); ok {
			for i, item := range ruleList {
				if rule, isStr := item.(string); isStr && rule == component+"*.rules" {
					ruleList = append(ruleList[:i], ruleList[i+1:]...)

					break
				}
			}
			prometheusContent["rule_files"] = ruleList
		}
	}

	// Marshal back
	newDataYAML, err := yaml.Marshal(&prometheusContent)
	if err != nil {
		return err
	}
	configMap.Data.PrometheusYML = string(newDataYAML)

	newyamlData, err := yaml.Marshal(&configMap)
	if err != nil {
		return err
	}

	// Write the modified content back to the file
	err = os.WriteFile(prometheusConfigPath, newyamlData, 0)

	return err
}
