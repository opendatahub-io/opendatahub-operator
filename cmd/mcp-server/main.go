package main

import (
	"log"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
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

	clientsetCfg := rest.CopyConfig(kubeConfig)
	if clientsetCfg.Timeout == 0 {
		clientsetCfg.Timeout = 30 * time.Second
	}
	clientset, err := kubernetes.NewForConfig(clientsetCfg)
	if err != nil {
		log.Fatalf("mcp-server: clientset: %v", err)
	}

	s := server.NewMCPServer("opendatahub-health", "0.1.0")

	registerPlatformHealth(s, kubeClient)
	registerPodLogs(s, clientset)

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("mcp-server: serve: %v", err)
	}
}
