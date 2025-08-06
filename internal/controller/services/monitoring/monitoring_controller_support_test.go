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
		name        string
		exporters   map[string]string
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid custom exporters",
			exporters: map[string]string{
				"logging":     "loglevel: debug",
				"otlp/jaeger": "endpoint: http://jaeger:4317\ntls:\n  insecure: true",
			},
			expectError: false,
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

				// Verify template data
				if exporters, ok := templateData["CustomMetricsExporters"]; !ok {
					t.Error("CustomMetricsExporters should be in template data")
				} else {
					exporterMap, ok := exporters.(map[string]interface{})
					if !ok {
						t.Error("CustomMetricsExporters should be a map[string]interface{}")
						return
					}
					if len(exporterMap) != len(tt.exporters) {
						t.Errorf("Expected %d exporters, got %d", len(tt.exporters), len(exporterMap))
					}
				}
			}
		})
	}
}
