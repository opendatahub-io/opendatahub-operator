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
	defaultOperatorNS        = "opendatahub-operator-system"
	defaultAppsNS            = "opendatahub"
	defaultOperatorDeploy    = "opendatahub-operator-controller-manager"
	dsciName                 = "default-dsci"
	dscName                  = "default-dsc"
)

func main() {
	outputJSON := flag.Bool("json", false, "Output report as JSON")
	layerFlag := flag.String("layer", "",
		"Run only these layers, comma-separated (e.g. infrastructure or workload). "+
			"infrastructure=nodes,quotas; workload=deployments,pods,events,operator,dsci,dsc")
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
		printReport(report)
	}

	if report.Healthy() {
		os.Exit(0)
	}
	os.Exit(1)
}

func loadConfig(layerFlag, sectionsFlag string) clusterhealth.Config {
	operatorNS := getEnv(envOperatorNamespace, defaultOperatorNS)
	appsNS := getEnv(envApplicationsNamespace, defaultAppsNS)
	operatorDeploy := getEnv(envOperatorDeployment, defaultOperatorDeploy)

	cfg := clusterhealth.Config{
		Operator: clusterhealth.OperatorConfig{
			Namespace: operatorNS,
			Name:      operatorDeploy,
		},
		Namespaces: clusterhealth.NamespaceConfig{
			Apps:  appsNS,
			Extra: []string{"kube-system"},
		},
		DSCI: types.NamespacedName{Name: dsciName},
		DSC:  types.NamespacedName{Name: dscName},
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

func printReport(r *clusterhealth.Report) {
	fmt.Printf("Cluster health check at %s\n", r.CollectedAt.Format("2006-01-02 15:04:05"))
	healthy := r.Healthy()
	fmt.Printf("Healthy: %v\n", healthy)
	if !healthy {
		sections := []struct {
			name   string
			errStr string
		}{
			{"Nodes", r.Nodes.Error},
			{"Deployments", r.Deployments.Error},
			{"Pods", r.Pods.Error},
			{"Events", r.Events.Error},
			{"Quotas", r.Quotas.Error},
			{"Operator", r.Operator.Error},
			{"DSCI", r.DSCI.Error},
			{"DSC", r.DSC.Error},
		}
		for _, s := range sections {
			if s.errStr != "" {
				fmt.Printf("  %s: %s\n", s.name, s.errStr)
			}
		}
	}
}
