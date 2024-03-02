package components

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
)

// Component struct defines the basis for each OpenDataHub component configuration.
// +kubebuilder:object:generate=true
type Component struct {
	// Set to one of the following values:
	//
	// - "Managed" : the operator is actively managing the component and trying to keep it active.
	//               It will only upgrade the component if it is safe to do so
	//
	// - "Removed" : the operator is actively managing the component and will not install it,
	//               or if it is installed, the operator will try to remove it
	//
	// +kubebuilder:validation:Enum=Managed;Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
	// Add any other common fields across components below

	// Add developer fields
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=2
	DevFlags *DevFlags `json:"devFlags,omitempty"`
}

func (c *Component) GetManagementState() operatorv1.ManagementState {
	return c.ManagementState
}

func (c *Component) SetImageParamsMap(imageMap map[string]string) map[string]string {
	return imageMap
}

func (c *Component) Cleanup(_ client.Client, _ *dsciv1.DSCInitializationSpec) error {
	// noop
	return nil
}

// DevFlags defines list of fields that can be used by developers to test customizations. This is not recommended
// to be used in production environment.
// +kubebuilder:object:generate=true
type DevFlags struct {
	// List of custom manifests for the given component
	// +optional
	Manifests []ManifestsConfig `json:"manifests,omitempty"`
}

type ManifestsConfig struct {
	// uri is the URI point to a git repo with tag/branch. e.g.  https://github.com/org/repo/tarball/<tag/branch>
	// +optional
	// +kubebuilder:default:=""
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=1
	URI string `json:"uri,omitempty"`

	// contextDir is the relative path to the folder containing manifests in a repository
	// +optional
	// +kubebuilder:default:=""
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=2
	ContextDir string `json:"contextDir,omitempty"`

	// sourcePath is the subpath within contextDir where kustomize builds start. Examples include any sub-folder or path: `base`, `overlays/dev`, `default`, `odh` etc.
	// +optional
	// +kubebuilder:default:=""
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=3
	SourcePath string `json:"sourcePath,omitempty"`
}

type ComponentInterface interface {
	ReconcileComponent(ctx context.Context, cli client.Client, resConf *rest.Config, owner metav1.Object, DSCISpec *dsciv1.DSCInitializationSpec, currentComponentExist bool) error
	Cleanup(cli client.Client, DSCISpec *dsciv1.DSCInitializationSpec) error
	GetComponentName() string
	GetManagementState() operatorv1.ManagementState
	SetImageParamsMap(imageMap map[string]string) map[string]string
	OverrideManifests(platform string) error
	UpdatePrometheusConfig(cli client.Client, enable bool, component string) error
}

// UpdatePrometheusConfig update prometheus-configs.yaml to include/exclude <component>.rules
// parameter enable when set to true to add new rules, when set to false to remove existing rules.
func (c *Component) UpdatePrometheusConfig(_ client.Client, enable bool, component string) error {
	prometheusconfigPath := filepath.Join("/opt/manifests", "monitoring", "prometheus", "apps", "prometheus-configs.yaml")

	// create a struct to mock poremtheus.yml
	type ConfigMap struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Metadata   struct {
			Name      string `yaml:"name"`
			Namespace string `yaml:"namespace"`
		} `yaml:"metadata"`
		Data struct {
			PrometheusYML      string `yaml:"prometheus.yml"`
			OperatorRules      string `yaml:"operator-recording.rules"`
			DeadManSnitchRules string `yaml:"deadmanssnitch-alerting.rules"`
			CFRRules           string `yaml:"codeflare-recording.rules"`
			CRARules           string `yaml:"codeflare-alerting.rules"`
			DashboardRRules    string `yaml:"rhods-dashboard-recording.rules"`
			DashboardARules    string `yaml:"rhods-dashboard-alerting.rules"`
			DSPRRules          string `yaml:"data-science-pipelines-operator-recording.rules"`
			DSPARules          string `yaml:"data-science-pipelines-operator-alerting.rules"`
			MMRRules           string `yaml:"model-mesh-recording.rules"`
			MMARules           string `yaml:"model-mesh-alerting.rules"`
			OdhModelRRules     string `yaml:"odh-model-controller-recording.rules"`
			OdhModelARules     string `yaml:"odh-model-controller-alerting.rules"`
			RayARules          string `yaml:"ray-alerting.rules"`
			WorkbenchesRRules  string `yaml:"workbenches-recording.rules"`
			WorkbenchesARules  string `yaml:"workbenches-alerting.rules"`
			KserveRRules       string `yaml:"kserve-recording.rules"`
			KserveARules       string `yaml:"kserve-alerting.rules"`
			KueueRRules        string `yaml:"kueue-recording.rules"`
			KueueARules        string `yaml:"kueue-alerting.rules"`
		} `yaml:"data"`
	}
	var configMap ConfigMap
	// prometheusContent will represent content of prometheus.yml due to its dynamic struct
	var prometheusContent map[interface{}]interface{}

	// read prometheus.yml from local disk /opt/mainfests/monitoring/prometheus/apps/
	yamlData, err := os.ReadFile(prometheusconfigPath)
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
		fmt.Println("Removing prometheus rule: " + component + "*.rules")
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
	err = os.WriteFile(prometheusconfigPath, newyamlData, 0)
	return err
}
