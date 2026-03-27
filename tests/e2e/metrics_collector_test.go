package e2e_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/clusterhealth"
)

const (
	defaultMetricsInterval = 30 * time.Second
	metricsFileName        = "cluster-health-metrics.txt"
	envMetricsInterval     = "E2E_METRICS_INTERVAL"
)

// MetricsCollector periodically runs clusterhealth.Run() and appends
// Prometheus exposition format lines to a file. Designed to run as a
// background goroutine during e2e test suites for CI observability.
type MetricsCollector struct {
	cfg      clusterhealth.Config
	interval time.Duration
	file     *os.File
	mu       sync.Mutex
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// startMetricsCollectorIfEnabled starts a MetricsCollector if ARTIFACT_DIR is set.
// Returns nil if collection is disabled (no ARTIFACT_DIR) or on any setup error.
func startMetricsCollectorIfEnabled() *MetricsCollector {
	artifactDir := os.Getenv("ARTIFACT_DIR")
	if artifactDir == "" {
		log.Printf("ARTIFACT_DIR not set, skipping periodic metrics collection")
		return nil
	}

	c, err := createMetricsClient()
	if err != nil {
		log.Printf("Failed to create metrics collector client: %v", err)
		return nil
	}

	outputPath := artifactDir + "/" + metricsFileName
	interval := parseMetricsInterval()

	cfg := clusterhealth.Config{
		Client: c,
		Operator: clusterhealth.OperatorConfig{
			Namespace: testOpts.operatorNamespace,
		},
		Namespaces: clusterhealth.NamespaceConfig{
			Apps:       testOpts.appsNamespace,
			Monitoring: testOpts.monitoringNamespace,
			Extra:      []string{"kube-system"},
		},
		DSCI:         types.NamespacedName{Name: dsciInstanceName},
		DSC:          types.NamespacedName{Name: dscInstanceName},
		OnlySections: []string{clusterhealth.SectionNodes, clusterhealth.SectionDeployments, clusterhealth.SectionPods, clusterhealth.SectionQuotas},
	}

	mc, err := newMetricsCollector(cfg, outputPath, interval)
	if err != nil {
		log.Printf("Failed to start metrics collector: %v", err)
		return nil
	}

	mc.start()
	log.Printf("Periodic metrics collector started (interval=%s, output=%s)", interval, outputPath)
	return mc
}

func createMetricsClient() (client.Client, error) {
	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("kube config: %w", err)
	}
	c, err := client.New(kubeConfig, client.Options{Scheme: Scheme})
	if err != nil {
		return nil, fmt.Errorf("client: %w", err)
	}
	return c, nil
}

func newMetricsCollector(cfg clusterhealth.Config, outputPath string, interval time.Duration) (*MetricsCollector, error) {
	f, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &MetricsCollector{
		cfg:      cfg,
		interval: interval,
		file:     f,
		stopCh:   make(chan struct{}),
	}, nil
}

func (mc *MetricsCollector) start() {
	mc.wg.Add(1)
	go mc.loop()
}

// Stop signals the collector to stop and waits for it to finish.
func (mc *MetricsCollector) Stop() {
	close(mc.stopCh)
	mc.wg.Wait()

	mc.mu.Lock()
	defer mc.mu.Unlock()
	if err := mc.file.Close(); err != nil {
		log.Printf("metrics collector: failed to close file: %v", err)
	}
	log.Printf("Periodic metrics collector stopped")
}

func (mc *MetricsCollector) loop() {
	defer mc.wg.Done()

	mc.collect()

	ticker := time.NewTicker(mc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-mc.stopCh:
			return
		case <-ticker.C:
			mc.collect()
		}
	}
}

func (mc *MetricsCollector) collect() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ts := time.Now().UnixMilli()

	report, err := clusterhealth.Run(ctx, mc.cfg)
	if err != nil {
		log.Printf("metrics collector: clusterhealth.Run failed: %v", err)
		mc.writeLine(fmt.Sprintf("cluster_health_collection_error 1 %d", ts))
		return
	}

	lines := report.PrometheusExport()
	if len(lines) == 0 {
		return
	}

	mc.mu.Lock()
	defer mc.mu.Unlock()

	data := strings.Join(lines, "\n") + "\n"
	if _, err := mc.file.WriteString(data); err != nil {
		log.Printf("metrics collector: write failed: %v", err)
		return
	}
	if err := mc.file.Sync(); err != nil {
		log.Printf("metrics collector: sync failed: %v", err)
	}
}

func (mc *MetricsCollector) writeLine(line string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if _, err := mc.file.WriteString(line + "\n"); err != nil {
		log.Printf("metrics collector: write failed: %v", err)
	}
}

func parseMetricsInterval() time.Duration {
	if v := os.Getenv(envMetricsInterval); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil && d > 0 {
			return d
		}
		log.Printf("Invalid %s=%q, using default %s", envMetricsInterval, v, defaultMetricsInterval)
	}
	return defaultMetricsInterval
}
