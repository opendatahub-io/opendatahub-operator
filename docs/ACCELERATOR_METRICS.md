# Accelerator Metrics Collection in RHOAI

## Overview

This document describes the accelerator metrics collection feature implemented in Red Hat OpenShift AI (RHOAI) via the OpenTelemetry (otel) collector. This feature allows independent collection of hardware accelerator metrics (GPUs, NPUs, TPUs, etc.) into the RHOAI Prometheus instance when monitoring is enabled and properly configured.

## Architecture

The accelerator metrics collection integrates with the existing RHOAI monitoring infrastructure:

1. **Data Source**: NVIDIA DCGM Exporter (deployed by GPU Operator)
2. **Collection**: OpenTelemetry Collector with Prometheus receiver
3. **Storage**: RHOAI Prometheus instance
4. **Processing**: Metric normalization and relabeling for OCP compatibility

## Configuration Requirements

Accelerator metrics collection is enabled when:

1. `.spec.monitoring.managementState` is set to `managed`
2. `.spec.monitoring.metrics` configuration is present

When either condition is not met, accelerator metrics are not scraped.

## Implementation Details

### Template Data Configuration

The monitoring controller support (`monitoring_controller_support.go`) adds an `AcceleratorMetrics` flag to the template data:

```go
templateData["AcceleratorMetrics"] = monitoring.Spec.Metrics != nil
```

### OpenTelemetry Collector Configuration

The otel-collector template (`opentelemetry-collector.tmpl.yaml`) includes a conditional accelerator metrics scrape job:

```yaml
{{- if .AcceleratorMetrics }}
- job_name: 'dcgm-exporter-accelerator-metrics'
  # Scrapes from nvidia-gpu-operator namespace
  # Targets nvidia-dcgm-exporter pods on port 9400
  # Applies OCP normalization via relabeling rules
{{- end }}
```

### OCP Metric Normalization

The implementation applies OpenShift Container Platform (OCP) normalization patterns:

#### DCGM to OCP Metric Name Mapping

| DCGM Metric                 | Normalized Name                       | Description                    |
| --------------------------- | ------------------------------------- | ------------------------------ |
| `DCGM_FI_DEV_GPU_TEMP`      | `nvidia_gpu_temperature_celsius`      | GPU temperature in Celsius     |
| `DCGM_FI_DEV_GPU_UTIL`      | `nvidia_gpu_utilization_ratio`        | GPU utilization ratio (0-1)    |
| `DCGM_FI_DEV_MEM_COPY_UTIL` | `nvidia_gpu_memory_utilization_ratio` | Memory utilization ratio (0-1) |
| `DCGM_FI_DEV_FB_USED`       | `nvidia_gpu_memory_used_bytes`        | GPU memory used in bytes       |
| `DCGM_FI_DEV_FB_FREE`       | `nvidia_gpu_memory_free_bytes`        | GPU memory free in bytes       |
| `DCGM_FI_DEV_POWER_USAGE`   | `nvidia_gpu_power_usage_watts`        | GPU power usage in watts       |
| `DCGM_FI_DEV_SM_CLOCK`      | `nvidia_gpu_sm_clock_mhz`             | SM clock frequency in MHz      |
| `DCGM_FI_DEV_MEM_CLOCK`     | `nvidia_gpu_memory_clock_mhz`         | Memory clock frequency in MHz  |

#### Additional Labels

- `component`: Set from pod app label
- `job`: Set to `rhoai-accelerator-metrics`
- `node`: Set from pod node name

### Recording Rules

RHOAI-specific accelerator recording rules are added to provide aggregated metrics:

```yaml
# GPU utilization aggregated across all GPUs
cluster:usage:consumption:rhods:gpu:utilization:avg

# GPU memory utilization aggregated across all GPUs
cluster:usage:consumption:rhods:gpu:memory_utilization:avg

# Maximum GPU temperature across all GPUs
cluster:usage:consumption:rhods:gpu:temperature:max

# Total GPU memory used across all GPUs
cluster:usage:consumption:rhods:gpu:memory_used:sum

# Total GPU power consumption across all GPUs
cluster:usage:consumption:rhods:gpu:power:sum

# Count of active GPUs
cluster:usage:consumption:rhods:gpu:count
```

## Prerequisites

1. **NVIDIA GPU Operator**: Must be installed with DCGM Exporter enabled
2. **GPU Hardware**: Cluster nodes with NVIDIA GPUs
3. **RHOAI Monitoring**: Monitoring service deployed with metrics enabled

## Verification

### Check Accelerator Metrics Collection

1. Verify monitoring configuration:

```bash
oc get monitoring default-monitoring -o yaml
```

1. Check OpenTelemetry Collector logs:

```bash
oc logs -n <monitoring-namespace> deployment/data-science-collector
```

1. Query accelerator metrics from Prometheus:

```bash
# Access RHOAI Prometheus and query:
nvidia_gpu_utilization_ratio
nvidia_gpu_temperature_celsius
```

### Expected Behavior

- **When Enabled**: Accelerator metrics appear in RHOAI Prometheus with `job="rhoai-accelerator-metrics"`
- **When Disabled**: No accelerator metrics collection occurs, reducing overhead

## Troubleshooting

### Common Issues

1. **No accelerator metrics collected**:

   - Verify GPU Operator is installed with DCGM Exporter
   - Check monitoring management state is `managed`
   - Ensure metrics configuration is present

2. **DCGM Exporter not found**:

   - Verify DCGM Exporter pods are running in `nvidia-gpu-operator` namespace
   - Check service is exposing port 9400

3. **Metrics not normalized**:
   - Check relabeling rules are applied correctly
   - Verify metric names are transformed as expected

### Debug Commands

```bash
# Check DCGM Exporter status
oc get pods -n nvidia-gpu-operator -l app=nvidia-dcgm-exporter

# Check raw DCGM metrics
oc exec -n nvidia-gpu-operator <dcgm-pod> -- curl localhost:9400/metrics

# Check otel-collector configuration
oc get opentelemetrycollector data-science-collector -o yaml
```

## Related Components

- **GPU Operator**: Provides DCGM Exporter
- **OpenTelemetry Operator**: Manages otel-collector instances
- **Prometheus Operator**: Manages RHOAI Prometheus instance
- **Cluster Monitoring**: OCP platform monitoring integration
