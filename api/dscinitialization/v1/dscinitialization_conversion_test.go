/*
Copyright 2026.

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

package v1

import (
	"testing"

	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
)

func TestBuiltinDCGMScrapeOverrideSurvivesVersionConversion(t *testing.T) {
	disabled := false
	legacy := &DSCInitialization{
		Spec: DSCInitializationSpec{
			Monitoring: serviceApi.DSCIMonitoring{
				MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
					Metrics: &serviceApi.Metrics{BuiltinDCGMScrapeEnabled: &disabled},
				},
			},
		},
	}

	hub := &dsciv2.DSCInitialization{}
	if err := legacy.ConvertTo(hub); err != nil {
		t.Fatalf("convert DSCI v1 to v2: %v", err)
	}
	if got := hub.Spec.Monitoring.MonitoringCommonSpec.Metrics; got == nil || got.BuiltinDCGMScrapeEnabled == nil || *got.BuiltinDCGMScrapeEnabled {
		t.Fatal("explicit false DCGM scrape override was not preserved in DSCI v2")
	}

	roundTripped := &DSCInitialization{}
	if err := roundTripped.ConvertFrom(hub); err != nil {
		t.Fatalf("convert DSCI v2 to v1: %v", err)
	}
	if got := roundTripped.Spec.Monitoring.MonitoringCommonSpec.Metrics; got == nil || got.BuiltinDCGMScrapeEnabled == nil || *got.BuiltinDCGMScrapeEnabled {
		t.Fatal("explicit false DCGM scrape override was not preserved after DSCI v1 round-trip")
	}
}
