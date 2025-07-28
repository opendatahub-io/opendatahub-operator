# Observability Architecture Decision Record (ADR)

## Status
Accepted

## Context
OpenShift AI components need to provide comprehensive observability through metrics, logs, and distributed tracing to enable effective monitoring, debugging, and performance analysis. This ADR specifically addresses the tracing strategy for OpenShift AI components to ensure traces are properly collected and sent to the OpenShift AI Tempo deployment.

## Decision

### Recommended Approach: Native OpenTelemetry SDK Injection

**We recommend using "native" OpenTelemetry instrumentation via the `instrumentation.opentelemetry.io/inject-sdk: "true"` annotation** as the primary method for enabling distributed tracing in OpenShift AI components.

This approach is preferred because:
- **Predictable Runtime Environment**: Components using this annotation have a more predictable and consistent runtime environment
- **Better Performance**: Native SDK injection typically has lower overhead compared to auto-instrumentation
- **Greater Control**: Provides more control over instrumentation configuration and behavior
- **Stability**: More stable across different deployment scenarios and OpenShift versions

### Implementation Details

When enabling tracing for a component:

1. **Annotation-based Injection**: Add the following annotation to your deployment/pod specification:
   ```yaml
   metadata:
     annotations:
       instrumentation.opentelemetry.io/inject-sdk: "true"
   ```

2. **Instrumentation CR**: The OpenShift AI operator automatically creates an `Instrumentation` custom resource when tracing is enabled in the DSCInitialization CR:
   ```yaml
   apiVersion: opentelemetry.io/v1alpha1
   kind: Instrumentation
   metadata:
     name: {{ .InstrumentationName }}
     namespace: {{ .Namespace }}
   spec:
     exporter:
       endpoint: {{ .OtlpEndpoint }}
     sampler:
       type: {{ .SamplerType }}
       argument: "{{ .SampleRatio }}"
   ```

3. **OpenTelemetry Collector**: The operator deploys an OpenTelemetry Collector that:
   - Receives traces via OTLP (gRPC and HTTP)
   - Processes traces with Kubernetes attributes and resource detection
   - Exports traces to the Tempo backend

4. **Tempo Integration**: Traces are automatically forwarded to the OpenShift AI Tempo deployment for storage and querying

### Configuration

Tracing is configured through the DSCInitialization CR:

```yaml
apiVersion: dscinitialization.opendatahub.io/v1
kind: DSCInitialization
metadata:
  name: default-dsci
spec:
  monitoring:
    traces:
      storage:
        backend: "pv"  # or "s3", "gcs"
        size: "10Gi"   # for PV backend
        # secret: "storage-credentials"  # required for s3/gcs
      sampleRatio: "0.1"  # Sample 10% of traces
```

### Alternative Options (Available but Not Recommended)

While the OpenTelemetry Operator supports language-specific auto-instrumentation annotations, these are considered secondary options:

- `instrumentation.opentelemetry.io/inject-java: "true"` - Java auto-instrumentation
- `instrumentation.opentelemetry.io/inject-nodejs: "true"` - Node.js auto-instrumentation  
- `instrumentation.opentelemetry.io/inject-python: "true"` - Python auto-instrumentation
- `instrumentation.opentelemetry.io/inject-dotnet: "true"` - .NET auto-instrumentation
- `instrumentation.opentelemetry.io/inject-go: "true"` - Go auto-instrumentation

**Note**: These language-specific annotations should only be used when the native SDK injection approach is not suitable for specific technical requirements.

## Architecture Overview

The OpenShift AI observability architecture consists of:

1. **Component Level**: Applications instrumented with OpenTelemetry SDK
2. **Collection Layer**: OpenTelemetry Collector for trace aggregation and processing
3. **Storage Layer**: Tempo for trace storage (supports PV, S3, GCS backends)
4. **Query Layer**: Integration with Grafana for trace visualization

```
[Component with inject-sdk] → [OpenTelemetry Collector] → [Tempo] → [Grafana]
```

## Prerequisites

For tracing to work properly, the following operators must be installed:

1. **OpenTelemetry Operator** (`opentelemetry-product`)
   - Provides `Instrumentation` and `OpenTelemetryCollector` CRDs
   - Handles SDK injection based on annotations

2. **Tempo Operator** (`tempo-product`) 
   - Provides `TempoStack` and `TempoMonolithic` CRDs
   - Manages trace storage backend

## Benefits

- **Unified Tracing**: All OpenShift AI components use consistent tracing configuration
- **Automatic Integration**: No manual configuration required in component code
- **Flexible Storage**: Support for multiple storage backends (PV, S3, GCS)
- **Scalable**: OpenTelemetry Collector handles trace processing and batching
- **Observable**: Built-in monitoring and alerting for the tracing pipeline

## Consequences

### Positive
- Standardized approach across all OpenShift AI components
- Reduced complexity for component developers
- Centralized configuration and management
- Better performance compared to agent-based solutions

### Negative
- Requires OpenTelemetry and Tempo operators to be installed
- Additional resource overhead for collector and storage
- Limited to OpenTelemetry-supported languages and frameworks

## References

- [OpenTelemetry Auto-Instrumentation](https://github.com/open-telemetry/opentelemetry-operator/tree/main?tab=readme-ov-file#opentelemetry-auto-instrumentation-injection)
- [OpenShift AI Monitoring Configuration](api-overview.md#traces)
- [Component Integration Guide](COMPONENT_INTEGRATION.md) 