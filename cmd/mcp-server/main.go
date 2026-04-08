package main

import (
	"log"
	"os"

	"github.com/mark3labs/mcp-go/server"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client/config"
)

func main() {
	log.SetOutput(os.Stderr)

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

	s := server.NewMCPServer("opendatahub-health", "0.1.0")

	registerPlatformHealth(s, kubeClient)

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("mcp-server: serve: %v", err)
	}
}
