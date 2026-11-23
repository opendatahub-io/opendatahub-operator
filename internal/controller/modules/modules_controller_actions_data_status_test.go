//nolint:testpackage
package modules

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	conditionstest "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
)

func TestWriteDataAggregateStatus(t *testing.T) {
	tests := []struct {
		name             string
		feastEnabled     bool
		feastReady       bool
		feastDegraded    bool
		dchEnabled       bool
		dchReady         bool
		dchDegraded      bool
		wantState        operatorv1.ManagementState
		wantReady        metav1.ConditionStatus
		wantReason       string
		wantReleaseNames []string
	}{
		{
			name:         "Feast only",
			feastEnabled: true, feastReady: true,
			wantState: operatorv1.Managed, wantReady: metav1.ConditionTrue, wantReason: status.ReadyReason,
			wantReleaseNames: []string{"feast", "platform"},
		},
		{
			name:       "DCH only without Feast dependency",
			dchEnabled: true, dchReady: true,
			wantState: operatorv1.Managed, wantReady: metav1.ConditionTrue, wantReason: status.ReadyReason,
			wantReleaseNames: []string{"flight-service", "platform"},
		},
		{
			name:      "all removed",
			wantState: operatorv1.Removed, wantReady: metav1.ConditionFalse, wantReason: status.RemovedReason,
		},
		{
			name:         "unhealthy DCH degrades active Data aggregate",
			feastEnabled: true, feastReady: true,
			dchEnabled: true, dchDegraded: true,
			wantState: operatorv1.Managed, wantReady: metav1.ConditionFalse, wantReason: status.ConditionTypeDegraded,
			wantReleaseNames: []string{"data/platform", "dataconnecthub/platform", "feast", "flight-service"},
		},
		{
			name:         "unready Feast does not prevent DCH reconciliation but affects aggregate",
			feastEnabled: true,
			dchEnabled:   true, dchReady: true,
			wantState: operatorv1.Managed, wantReady: metav1.ConditionFalse, wantReason: status.NotReadyReason,
			wantReleaseNames: []string{"data/platform", "dataconnecthub/platform", "feast", "flight-service"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsc := &dscApi.DataScienceCluster{}
			cm := conditionstest.NewManager(dsc, "Ready", "DataReady")
			results := []perModuleResult{
				dataResult("data", "FeastOperator", tt.feastEnabled, tt.feastReady, tt.feastDegraded, "feast"),
				dataResult("dataconnecthub", "DataConnectHub", tt.dchEnabled, tt.dchReady, tt.dchDegraded, "flight-service", "platform"),
			}

			writeDataAggregateStatus(dsc, results, cm)

			if dsc.Status.Components.Data.ManagementState != tt.wantState {
				t.Fatalf("Data managementState = %q, want %q", dsc.Status.Components.Data.ManagementState, tt.wantState)
			}
			condition := cm.GetCondition("DataReady")
			if condition == nil {
				t.Fatal("DataReady condition is missing")
			}
			if condition.Status != tt.wantReady || condition.Reason != tt.wantReason {
				t.Fatalf("DataReady = (%q, %q), want (%q, %q)", condition.Status, condition.Reason, tt.wantReady, tt.wantReason)
			}

			var releaseNames []string
			if dsc.Status.Components.Data.DataCommonStatus != nil {
				for _, release := range dsc.Status.Components.Data.Releases {
					releaseNames = append(releaseNames, release.Name)
				}
			}
			if len(releaseNames) != len(tt.wantReleaseNames) {
				t.Fatalf("release names = %v, want %v", releaseNames, tt.wantReleaseNames)
			}
			for i := range releaseNames {
				if releaseNames[i] != tt.wantReleaseNames[i] {
					t.Fatalf("release names = %v, want %v", releaseNames, tt.wantReleaseNames)
				}
			}
		})
	}
}

func dataResult(moduleName, kind string, enabled, ready, degraded bool, releaseNames ...string) perModuleResult {
	result := perModuleResult{
		handler:  newStatusTestHandler(moduleName, kind, enabled, nil),
		enabled:  enabled,
		ready:    ready,
		degraded: degraded,
	}
	if enabled {
		result.moduleStatus = &ModuleStatus{}
		for _, name := range releaseNames {
			result.moduleStatus.Releases = append(result.moduleStatus.Releases, common.ComponentRelease{
				Name:    name,
				Version: "1.0.0",
			})
		}
		if moduleName == "data" {
			result.moduleStatus.Releases = []common.ComponentRelease{
				{Name: "feast", Version: "1.0.0"},
				{Name: "platform", Version: "1.0.0"},
			}
		}
	}
	if !enabled {
		result.condition = common.Condition{Type: kind + "Ready", Status: metav1.ConditionFalse, Reason: status.RemovedReason}
	}
	return result
}
