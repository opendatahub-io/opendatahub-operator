package main

import (
	"flag"
	"log"
	"os"

	"github.com/mark3labs/mcp-go/server"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client/config"
)

func main() {
	log.SetOutput(os.Stderr)

	oneShot := flag.Bool("one-shot", false, "Run diagnostics once (health check + failure classification), output JSON, and exit.")
	testName := flag.String("test-name", "ci-diagnose", "Test name for failure classification (used with --one-shot)")
	flag.Parse()

	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Fatalf("mcp-server: kubeconfig: %v", err)
	}

	kubeClient, err := client.New(kubeConfig, client.Options{
		Scheme: clientgoscheme.Scheme,
	})
	if err != nil {
		log.Fatalf("mcp-server: kube client: %v", err)
	}

	if *oneShot {
		os.Exit(runOneShot(kubeClient, *testName))
	}

	s := server.NewMCPServer("opendatahub-health", "0.1.0")

	registerPlatformHealth(s, kubeClient)
	registerOperatorDependencies(s, kubeClient)
	registerRecentEvents(s, kubeClient)
	registerClassifyFailure(s, kubeClient)
	registerComponentStatus(s, kubeClient)

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("mcp-server: serve: %v", err)
	}
}
