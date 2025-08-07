/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

//nolint:testpackage // Need to test unexported function getTemplateData
package monitoring

import (
	"context"
	"strings"
	"testing"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func TestCustomMetricsExporters(t *testing.T) {
	tests := []struct {
		name                 string
		exporters            map[string]string
		expectError          bool
		errorMsg             string
		expectedParsedConfig map[string]interface{} // Add expected parsed output
		expectedNames        []string               // Add expected names
	}{
		{
			name: "valid custom exporters",
			exporters: map[string]string{
				"logging":     "loglevel: debug",
				"otlp/jaeger": "endpoint: http://jaeger:4317\ntls:\n  insecure: true",
			},
			expectError: false,
			expectedParsedConfig: map[string]interface{}{
				"logging": map[string]interface{}{
					"loglevel": "debug",
				},
				"otlp/jaeger": map[string]interface{}{
					"endpoint": "http://jaeger:4317",
					"tls": map[string]interface{}{
						"insecure": true,
					},
				},
			},
			expectedNames: []string{"logging", "otlp/jaeger"}, // Note: order may vary
		},
		{
			name:                 "empty exporters map",
			exporters:            map[string]string{},
			expectError:          false,
			expectedParsedConfig: map[string]interface{}{},
			expectedNames:        []string{},
		},
		{
			name: "reserved name prometheus",
			exporters: map[string]string{
				"prometheus": "endpoint: http://example.com",
			},
			expectError: true,
			errorMsg:    "reserved",
		},
		{
			name: "invalid YAML",
			exporters: map[string]string{
				"logging": "invalid: yaml: content: [unclosed",
			},
			expectError: true,
			errorMsg:    "invalid YAML",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mon := &serviceApi.Monitoring{
				Spec: serviceApi.MonitoringSpec{
					MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
						Namespace: "test-namespace",
						Metrics: &serviceApi.Metrics{
							Exporters: tt.exporters,
						},
					},
				},
			}

			rr := &odhtypes.ReconciliationRequest{
				Instance: mon,
			}

			templateData, err := getTemplateData(context.Background(), rr)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error to contain '%s', got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}

				// Verify template data structure
				exporters, ok := templateData["CustomMetricsExporters"]
				if !ok {
					t.Error("CustomMetricsExporters should be in template data")
					return
				}

				exporterMap, ok := exporters.(map[string]interface{})
				if !ok {
					t.Error("CustomMetricsExporters should be a map[string]interface{}")
					return
				}

				if len(exporterMap) != len(tt.exporters) {
					t.Errorf("Expected %d exporters, got %d", len(tt.exporters), len(exporterMap))
				}

				// Verify parsed YAML content matches expected values
				if tt.expectedParsedConfig != nil {
					for name, expectedConfig := range tt.expectedParsedConfig {
						actualConfig, exists := exporterMap[name]
						if !exists {
							t.Errorf("Expected exporter '%s' not found in parsed config", name)
							continue
						}

						// Deep compare the parsed configuration
						if !deepEqual(actualConfig, expectedConfig) {
							t.Errorf("Exporter '%s' config mismatch.\nExpected: %+v\nActual: %+v",
								name, expectedConfig, actualConfig)
						}
					}
				}

				// Verify CustomMetricsExporterNames
				exporterNames, ok := templateData["CustomMetricsExporterNames"]
				if !ok {
					t.Error("CustomMetricsExporterNames should be in template data")
					return
				}

				namesList, ok := exporterNames.([]string)
				if !ok {
					t.Error("CustomMetricsExporterNames should be a []string")
					return
				}

				if len(namesList) != len(tt.exporters) {
					t.Errorf("Expected %d exporter names, got %d", len(tt.exporters), len(namesList))
				}

				// Verify all expected names are present (order may vary)
				if tt.expectedNames != nil {
					for _, expectedName := range tt.expectedNames {
						found := false
						for _, actualName := range namesList {
							if actualName == expectedName {
								found = true
								break
							}
						}
						if !found {
							t.Errorf("Expected exporter name '%s' not found in names list: %v",
								expectedName, namesList)
						}
					}
				}
			}
		})
	}
}

// deepEqual performs a deep comparison of two interface{} values.
// This is a simplified version for our specific use case.
func deepEqual(a, b interface{}) bool {
	switch va := a.(type) {
	case map[string]interface{}:
		vb, ok := b.(map[string]interface{})
		if !ok || len(va) != len(vb) {
			return false
		}
		for k, v := range va {
			if !deepEqual(v, vb[k]) {
				return false
			}
		}
		return true
	case string:
		vb, ok := b.(string)
		return ok && va == vb
	case bool:
		vb, ok := b.(bool)
		return ok && va == vb
	case int:
		vb, ok := b.(int)
		return ok && va == vb
	case float64:
		vb, ok := b.(float64)
		return ok && va == vb
	default:
		return a == b
	}
}
