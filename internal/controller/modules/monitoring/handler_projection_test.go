package monitoring_test

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/monitoring"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"

	. "github.com/onsi/gomega"
)

// crdPath is the module CRD's location within its local downloaded chart
// assets (see make get-manifests).
const crdPath = "crds/services.platform.opendatahub.io_monitorings.yaml"

// monitoringSuite drives the Monitoring DSCI projection cases through the
// shared runner; each case is one method registered as a t.Run, building its
// DSC context explicitly from newDSCI with a monitoring namespace that
// differs from the CRD default (proving verbatim projection) and amending
// only the fields under test. The client feeds the single-node
// Infrastructure lookups; DSCI-level managementState must not be projected
// into the module CR — the CRD defines (and defaults) the field, so only a
// pre-admission check can catch a leak.
type monitoringSuite struct {
	*projection.Runner
}

func (s monitoringSuite) namespaceOnly(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.namespace == "odh-monitoring"`),
		jq.Match(`.spec.metrics == null`),
		jq.Match(`.spec.traces == null`),
		jq.Match(`.spec.logs == null`),
		jq.Match(`.spec.usageLogs == null`),
		jq.Match(`.spec.collectorReplicas == null`),
		jq.Match(`.spec.managementState == null`),
	)))
}

func (s monitoringSuite) storageBackedMetricsWithEmptyAlertingObject(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"
	dsci.Spec.Monitoring.Metrics = &serviceApi.Metrics{
		Storage: &serviceApi.MetricsStorage{
			Size:      resource.MustParse("10Gi"),
			Retention: "7d",
		},
		Replicas: 2,
	}
	dsci.Spec.Monitoring.Alerting = &serviceApi.Alerting{}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.metrics.storage.size == "10Gi"`),
		jq.Match(`.spec.metrics.storage.retention == "7d"`),
		jq.Match(`.spec.metrics.replicas == 2`),
		// The emitted empty alerting object must survive admission as a
		// key; the cross-field CEL rule is satisfied by metrics.storage.
		jq.Match(`.spec.alerting == {}`),
		jq.Match(`.spec.collectorReplicas == 2`),
	)))
}

func (s monitoringSuite) snoDefaultsCollectorReplicasToOne(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"
	dsci.Spec.Monitoring.Metrics = &serviceApi.Metrics{
		Storage: &serviceApi.MetricsStorage{Size: resource.MustParse("5Gi")},
	}
	client := newFakeClient(newInfrastructure(configv1.SingleReplicaTopologyMode))

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
		Client: client,
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.collectorReplicas == 1`),
	)))
}

func (s monitoringSuite) explicitCollectorReplicasPreservedOnSNO(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"
	dsci.Spec.Monitoring.Metrics = &serviceApi.Metrics{
		Storage: &serviceApi.MetricsStorage{Size: resource.MustParse("5Gi")},
	}
	dsci.Spec.Monitoring.CollectorReplicas = 5
	client := newFakeClient(newInfrastructure(configv1.SingleReplicaTopologyMode))

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
		Client: client,
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.collectorReplicas == 5`),
	)))
}

func (s monitoringSuite) exporterOnlyMetrics(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"
	dsci.Spec.Monitoring.Metrics = &serviceApi.Metrics{
		Exporters: map[string]runtime.RawExtension{
			"custom": {Raw: []byte(`{"endpoint":"http://example.com"}`)},
		},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.metrics.exporters.custom.endpoint == "http://example.com"`),
		jq.Match(`.spec.metrics.storage == null`),
		jq.Match(`.spec.collectorReplicas == 2`),
	)))
}

// exporterOnlyMetricsAndTraces ports main's Mode2ExportersOnly: both signal
// stanzas configured with exporters only, so both storage stanzas stay
// absent while the collector defaults to two replicas.
func (s monitoringSuite) exporterOnlyMetricsAndTraces(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"
	dsci.Spec.Monitoring.Metrics = &serviceApi.Metrics{
		Exporters: map[string]runtime.RawExtension{
			"debug": {Raw: []byte(`{"verbosity":"detailed"}`)},
		},
	}
	dsci.Spec.Monitoring.Traces = &serviceApi.Traces{
		Exporters: map[string]runtime.RawExtension{
			"debug": {Raw: []byte(`{"verbosity":"detailed"}`)},
		},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.metrics.exporters.debug.verbosity == "detailed"`),
		jq.Match(`.spec.metrics.storage == null`),
		jq.Match(`.spec.traces.exporters.debug.verbosity == "detailed"`),
		jq.Match(`.spec.traces.storage == null`),
		jq.Match(`.spec.collectorReplicas == 2`),
	)))
}

// highlyAvailableInfrastructureDefaultsCollectorReplicasToTwo ports main's
// multi-node Infrastructure lookup branch: without an explicit
// collectorReplicas, a HighlyAvailable topology defaults to two replicas
// (the single-node branch is covered by snoDefaultsCollectorReplicasToOne).
func (s monitoringSuite) highlyAvailableInfrastructureDefaultsCollectorReplicasToTwo(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"
	dsci.Spec.Monitoring.Metrics = &serviceApi.Metrics{
		Storage: &serviceApi.MetricsStorage{Size: resource.MustParse("5Gi")},
	}
	client := newFakeClient(newInfrastructure(configv1.HighlyAvailableTopologyMode))

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
		Client: client,
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.collectorReplicas == 2`),
	)))
}

func (s monitoringSuite) storageBackedTracesWithTLSEnabled(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"
	dsci.Spec.Monitoring.Traces = &serviceApi.Traces{
		Storage: &serviceApi.TracesStorage{
			Backend: serviceApi.StorageBackendS3,
			Secret:  "my-s3-creds",
			Retention: metav1.Duration{
				Duration: 3600000000000, // 1h
			},
		},
		SampleRatio: "0.5",
		TLS: &serviceApi.TracesTLS{
			Enabled:           true,
			CertificateSecret: "tls-secret",
			CAConfigMap:       "ca-cm",
		},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.traces.storage.backend == "s3"`),
		jq.Match(`.spec.traces.storage.secret == "my-s3-creds"`),
		jq.Match(`.spec.traces.storage.retention == "1h0m0s"`),
		jq.Match(`.spec.traces.sampleRatio == "0.5"`),
		jq.Match(`.spec.traces.tls.enabled == true`),
		jq.Match(`.spec.traces.tls.certificateSecret == "tls-secret"`),
		jq.Match(`.spec.traces.tls.caConfigMap == "ca-cm"`),
		jq.Match(`.spec.collectorReplicas == 2`),
	)))
}

func (s monitoringSuite) pvBackedTracesWithTLSDisabled(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"
	dsci.Spec.Monitoring.Traces = &serviceApi.Traces{
		Storage: &serviceApi.TracesStorage{
			Backend: serviceApi.StorageBackendPV,
			Size:    "50Gi",
		},
		TLS: &serviceApi.TracesTLS{Enabled: false},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.traces.storage.backend == "pv"`),
		jq.Match(`.spec.traces.storage.size == "50Gi"`),
		// A disabled TLS stanza is nulled in the projection.
		jq.Match(`.spec.traces.tls == null`),
		jq.Match(`.spec.collectorReplicas == 2`),
	)))
}

func (s monitoringSuite) exporterOnlyTraces(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"
	dsci.Spec.Monitoring.Traces = &serviceApi.Traces{
		Exporters: map[string]runtime.RawExtension{
			"otlp/external": {Raw: []byte(`{"endpoint":"https://traces.example.com:4317"}`)},
		},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.traces.exporters["otlp/external"].endpoint == "https://traces.example.com:4317"`),
		jq.Match(`.spec.traces.storage == null`),
		jq.Match(`.spec.collectorReplicas == 2`),
	)))
}

func (s monitoringSuite) tracesWithoutStorageOrExportersOmitted(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"
	dsci.Spec.Monitoring.Traces = &serviceApi.Traces{SampleRatio: "0.1"}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.traces == null`),
		jq.Match(`.spec.collectorReplicas == null`),
	)))
}

func (s monitoringSuite) metricsWithoutStorageOrExportersOmitted(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"
	dsci.Spec.Monitoring.Metrics = &serviceApi.Metrics{Replicas: 2}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.metrics == null`),
		jq.Match(`.spec.collectorReplicas == null`),
	)))
}

func (s monitoringSuite) storageBackedLogs(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"
	dsci.Spec.Monitoring.Logs = &serviceApi.Logs{
		Storage: &serviceApi.LokiStorageConfig{
			Type:             "s3",
			SecretName:       "rhoai-logs-s3",
			CredentialMode:   "static",
			StorageClassName: "gp3-csi",
		},
		InferenceNamespaces: []string{"my-project"},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.logs.storage.type == "s3"`),
		jq.Match(`.spec.logs.storage.secretName == "rhoai-logs-s3"`),
		jq.Match(`.spec.logs.storage.credentialMode == "static"`),
		jq.Match(`.spec.logs.storage.storageClassName == "gp3-csi"`),
		jq.Match(`.spec.logs.inferenceNamespaces == ["my-project"]`),
		jq.Match(`.spec.usageLogs == null`),
		jq.Match(`.spec.collectorReplicas == null`),
	)))
}

func (s monitoringSuite) storageBackedUsageLogsWithoutLogs(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"
	dsci.Spec.Monitoring.UsageLogs = &serviceApi.UsageLogs{
		Storage: &serviceApi.LokiStorageConfig{
			Type:             "s3",
			SecretName:       "rhoai-logs-s3",
			CredentialMode:   "static",
			StorageClassName: "gp3-csi",
		},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.usageLogs.storage.type == "s3"`),
		jq.Match(`.spec.usageLogs.storage.secretName == "rhoai-logs-s3"`),
		jq.Match(`.spec.usageLogs.storage.credentialMode == "static"`),
		jq.Match(`.spec.usageLogs.storage.storageClassName == "gp3-csi"`),
		jq.Match(`.spec.logs == null`),
		jq.Match(`.spec.collectorReplicas == null`),
	)))
}

func (s monitoringSuite) logsAndUsageLogsIndependently(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"
	dsci.Spec.Monitoring.Logs = &serviceApi.Logs{
		Storage: &serviceApi.LokiStorageConfig{Type: "s3", SecretName: "rhoai-logs-s3"},
	}
	dsci.Spec.Monitoring.UsageLogs = &serviceApi.UsageLogs{
		Storage: &serviceApi.LokiStorageConfig{Type: "s3", SecretName: "rhoai-logs-s3"},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.logs.storage.secretName == "rhoai-logs-s3"`),
		jq.Match(`.spec.usageLogs.storage.secretName == "rhoai-logs-s3"`),
		jq.Match(`.spec.collectorReplicas == null`),
	)))
}

func (s monitoringSuite) logsWithoutStorageOmitted(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"
	dsci.Spec.Monitoring.Logs = &serviceApi.Logs{}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.logs == null`),
	)))
}

func (s monitoringSuite) usageLogsWithoutStorageOmitted(t *testing.T) {
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Namespace = "odh-monitoring"
	dsci.Spec.Monitoring.UsageLogs = &serviceApi.UsageLogs{}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSCI: dsci,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.usageLogs == null`),
	)))
}

// TestBuildModuleCRProjection validates the Monitoring DSCI projection against
// the real observability chart CRD from the local downloaded assets. It
// migrates the successful BuildModuleCR cases from handler_test.go, with fake
// clients feeding only the single-node Infrastructure lookups.
func TestBuildModuleCRProjection(t *testing.T) {
	h := monitoring.NewHandler()
	s := monitoringSuite{
		Runner: projection.NewRunner(t, h, crdPath),
	}

	t.Run("namespace only", s.namespaceOnly)
	t.Run("storage backed metrics with empty alerting object", s.storageBackedMetricsWithEmptyAlertingObject)
	t.Run("sno defaults collector replicas to one", s.snoDefaultsCollectorReplicasToOne)
	t.Run("explicit collector replicas preserved on sno", s.explicitCollectorReplicasPreservedOnSNO)
	t.Run("exporter only metrics", s.exporterOnlyMetrics)
	t.Run("exporter only metrics and traces", s.exporterOnlyMetricsAndTraces)
	t.Run("highly available infrastructure defaults collector replicas to two", s.highlyAvailableInfrastructureDefaultsCollectorReplicasToTwo)
	t.Run("storage backed traces with tls enabled", s.storageBackedTracesWithTLSEnabled)
	t.Run("pv backed traces with tls disabled", s.pvBackedTracesWithTLSDisabled)
	t.Run("exporter only traces", s.exporterOnlyTraces)
	t.Run("traces without storage or exporters omitted", s.tracesWithoutStorageOrExportersOmitted)
	t.Run("metrics without storage or exporters omitted", s.metricsWithoutStorageOrExportersOmitted)
	t.Run("storage backed logs", s.storageBackedLogs)
	t.Run("storage backed usage logs without logs", s.storageBackedUsageLogsWithoutLogs)
	t.Run("logs and usage logs independently", s.logsAndUsageLogsIndependently)
	t.Run("logs without storage omitted", s.logsWithoutStorageOmitted)
	t.Run("usage logs without storage omitted", s.usageLogsWithoutStorageOmitted)
}
