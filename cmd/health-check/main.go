// health-check runs cluster health checks and exits 0 if the cluster is healthy, 1 otherwise.
// It uses the same environment variables as the e2e tests so it can be run before e2e (e.g. from Makefile).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/clusterhealth"
)

const (
	envOperatorNamespace     = "E2E_TEST_OPERATOR_NAMESPACE"
	envApplicationsNamespace = "E2E_TEST_APPLICATIONS_NAMESPACE"
	envOperatorDeployment    = "E2E_TEST_OPERATOR_DEPLOYMENT_NAME"
	envMonitoringNamespace   = "E2E_TEST_DSC_MONITORING_NAMESPACE"
	defaultOperatorNS        = "opendatahub-operator-system"
	defaultAppsNS            = "opendatahub"
	defaultOperatorDeploy    = "opendatahub-operator-controller-manager"
	defaultMonitoringNS      = "opendatahub"
)

func main() {
	outputJSON := flag.Bool("json", false, "Output report as JSON")
	longFormat := flag.Bool("l", false, "Long format: list conditions and details per section (like ls -l)")
	layerFlag := flag.String("layer", "",
		"Run only these layers, comma-separated (e.g. infrastructure, workload, operator). "+
			"infrastructure=nodes,quotas; workload=deployments,pods,events,operator,dsci,dsc; operator=operator,dsci,dsc")
	sectionsFlag := flag.String("sections", "", "Comma-separated list of sections to run (e.g. nodes,quotas or deployments,pods). Overrides -layer.")
	flag.Parse()

	cfg := loadConfig(*layerFlag, *sectionsFlag)

	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "health-check: kube config: %v\n", err)
		os.Exit(1)
	}

	scheme := clientgoscheme.Scheme
	c, err := client.New(kubeConfig, client.Options{Scheme: scheme})
	if err != nil {
		fmt.Fprintf(os.Stderr, "health-check: client: %v\n", err)
		os.Exit(1)
	}
	cfg.Client = c

	report, err := clusterhealth.Run(context.Background(), cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "health-check: run: %v\n", err)
		os.Exit(1)
	}

	if *outputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "health-check: json: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Printf("Cluster health check at %s\n\n", report.CollectedAt.Format("2006-01-02 15:04:05"))
		fmt.Print(report.PrettyPrint(*longFormat))
		fmt.Printf("\nOverall: %s\n", healthyStr(report.Healthy()))
	}

	if report.Healthy() {
		os.Exit(0)
	}
	os.Exit(1)
}

func healthyStr(healthy bool) string {
	if healthy {
		return "healthy"
	}
	return "unhealthy"
}

func loadConfig(layerFlag, sectionsFlag string) clusterhealth.Config {
	operatorNS := getEnv(envOperatorNamespace, defaultOperatorNS)
	appsNS := getEnv(envApplicationsNamespace, defaultAppsNS)
	operatorDeploy := getEnv(envOperatorDeployment, defaultOperatorDeploy)
	monitoringNS := getEnv(envMonitoringNamespace, defaultMonitoringNS)

	cfg := clusterhealth.Config{
		Operator: clusterhealth.OperatorConfig{
			Namespace: operatorNS,
			Name:      operatorDeploy,
		},
		Namespaces: clusterhealth.NamespaceConfig{
			Apps:       appsNS,
			Monitoring: monitoringNS,
			Extra:      []string{"kube-system"},
		},
		// DSCI and DSC are singletons; empty name means "discover the instance on the cluster".
		DSCI: types.NamespacedName{},
		DSC:  types.NamespacedName{},
	}

	if sectionsFlag != "" {
		cfg.OnlySections = strings.Split(strings.TrimSpace(sectionsFlag), ",")
		for i := range cfg.OnlySections {
			cfg.OnlySections[i] = strings.TrimSpace(cfg.OnlySections[i])
		}
	} else if layerFlag != "" {
		cfg.Layers = strings.Split(strings.TrimSpace(layerFlag), ",")
		for i := range cfg.Layers {
			cfg.Layers[i] = strings.TrimSpace(cfg.Layers[i])
		}
	}

	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}
