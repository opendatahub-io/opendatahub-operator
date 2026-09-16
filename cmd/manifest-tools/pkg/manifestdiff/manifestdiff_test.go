package manifestdiff_test

import (
	"slices"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/manifestdiff"
)

func repo(ref string) *config.PlatformRepo {
	return &config.PlatformRepo{Repo: "opendatahub-io/example", Ref: ref, SourcePath: "config"}
}

func TestChangedNames(t *testing.T) {
	tests := []struct {
		name                         string
		old                          *config.ManifestsConfig
		new                          *config.ManifestsConfig
		wantNames                    []string
		wantUnattributed             []string
		wantPlatformManifestsChanged bool
	}{
		{
			name: "component ref changed",
			old: &config.ManifestsConfig{
				Components: map[string]config.Component{"kserve": {ODH: repo("main@aaa")}},
			},
			new: &config.ManifestsConfig{
				Components: map[string]config.Component{"kserve": {ODH: repo("main@bbb")}},
			},
			wantNames: []string{"kserve"},
		},
		{
			name: "component unchanged produces no diff",
			old: &config.ManifestsConfig{
				Components: map[string]config.Component{"kserve": {ODH: repo("main@aaa")}},
			},
			new: &config.ManifestsConfig{
				Components: map[string]config.Component{"kserve": {ODH: repo("main@aaa")}},
			},
			wantNames: nil,
		},
		{
			name: "component added",
			old:  &config.ManifestsConfig{Components: map[string]config.Component{}},
			new: &config.ManifestsConfig{
				Components: map[string]config.Component{"ray": {ODH: repo("main@aaa")}},
			},
			wantNames: []string{"ray"},
		},
		{
			name: "component removed",
			old: &config.ManifestsConfig{
				Components: map[string]config.Component{"ray": {ODH: repo("main@aaa")}},
			},
			new:       &config.ManifestsConfig{Components: map[string]config.Component{}},
			wantNames: []string{"ray"},
		},
		{
			name: "ccmCharts and componentCharts changes are both attributed",
			old: &config.ManifestsConfig{
				CCMCharts:       map[string]config.Component{"cert-manager-operator": {ODH: repo("main@aaa")}},
				ComponentCharts: map[string]config.Component{"dashboard-operator": {ODH: repo("main@aaa")}},
			},
			new: &config.ManifestsConfig{
				CCMCharts:       map[string]config.Component{"cert-manager-operator": {ODH: repo("main@bbb")}},
				ComponentCharts: map[string]config.Component{"dashboard-operator": {ODH: repo("main@aaa")}},
			},
			wantNames: []string{"cert-manager-operator"},
		},
		{
			name: "imageOverride change is attributed to its Component field, not the map key",
			old: &config.ManifestsConfig{
				ImageOverrides: map[string]config.ImageOverride{
					"RELATED_IMAGE_ODH_KSERVE_IMAGE": {Component: "kserve", ODH: &config.PlatformImage{Digest: "sha256:aaa"}},
				},
			},
			new: &config.ManifestsConfig{
				ImageOverrides: map[string]config.ImageOverride{
					"RELATED_IMAGE_ODH_KSERVE_IMAGE": {Component: "kserve", ODH: &config.PlatformImage{Digest: "sha256:bbb"}},
				},
			},
			wantNames: []string{"kserve"},
		},
		{
			name: "imageOverride added and removed are both attributed",
			old: &config.ManifestsConfig{
				ImageOverrides: map[string]config.ImageOverride{
					"RELATED_IMAGE_ODH_RAY_IMAGE": {Component: "ray"},
				},
			},
			new: &config.ManifestsConfig{
				ImageOverrides: map[string]config.ImageOverride{
					"RELATED_IMAGE_ODH_KSERVE_IMAGE": {Component: "kserve"},
				},
			},
			wantNames: []string{"kserve", "ray"},
		},
		{
			name: "duplicate names across sections are deduplicated",
			old: &config.ManifestsConfig{
				Components: map[string]config.Component{"kserve": {ODH: repo("main@aaa")}},
				ImageOverrides: map[string]config.ImageOverride{
					"RELATED_IMAGE_ODH_KSERVE_IMAGE": {Component: "kserve", ODH: &config.PlatformImage{Digest: "sha256:aaa"}},
				},
			},
			new: &config.ManifestsConfig{
				Components: map[string]config.Component{"kserve": {ODH: repo("main@bbb")}},
				ImageOverrides: map[string]config.ImageOverride{
					"RELATED_IMAGE_ODH_KSERVE_IMAGE": {Component: "kserve", ODH: &config.PlatformImage{Digest: "sha256:bbb"}},
				},
			},
			wantNames: []string{"kserve"},
		},
		{
			name: "only platformManifests changed -- not attributable, sentinel case",
			old: &config.ManifestsConfig{
				PlatformManifests: map[string]string{"osd-configs": "config/osd-configs"},
			},
			new: &config.ManifestsConfig{
				PlatformManifests: map[string]string{"osd-configs": "config/osd-configs-v2"},
			},
			wantNames:                    nil,
			wantPlatformManifestsChanged: true,
		},
		{
			name:      "identical empty configs",
			old:       &config.ManifestsConfig{},
			new:       &config.ManifestsConfig{},
			wantNames: nil,
		},
		{
			name:      "nil oldCfg is treated as empty, not a panic",
			old:       nil,
			new:       &config.ManifestsConfig{Components: map[string]config.Component{"kserve": {ODH: repo("main@aaa")}}},
			wantNames: []string{"kserve"},
		},
		{
			name:      "nil newCfg is treated as empty, not a panic",
			old:       &config.ManifestsConfig{Components: map[string]config.Component{"kserve": {ODH: repo("main@aaa")}}},
			new:       nil,
			wantNames: []string{"kserve"},
		},
		{
			name:      "both nil is treated as identical empty configs",
			old:       nil,
			new:       nil,
			wantNames: nil,
		},
		{
			name: "imageOverride change with no Component field is reported as unattributed, not silently dropped",
			old: &config.ManifestsConfig{
				ImageOverrides: map[string]config.ImageOverride{
					"RELATED_IMAGE_ODH_MLSERVER_IMAGE": {Source: "csv", ODH: &config.PlatformImage{Digest: "sha256:aaa"}},
				},
			},
			new: &config.ManifestsConfig{
				ImageOverrides: map[string]config.ImageOverride{
					"RELATED_IMAGE_ODH_MLSERVER_IMAGE": {Source: "csv", ODH: &config.PlatformImage{Digest: "sha256:bbb"}},
				},
			},
			wantNames:        nil,
			wantUnattributed: []string{"RELATED_IMAGE_ODH_MLSERVER_IMAGE"},
		},
		{
			name: "mix of attributed and unattributed imageOverride changes",
			old: &config.ManifestsConfig{
				ImageOverrides: map[string]config.ImageOverride{
					"RELATED_IMAGE_ODH_KSERVE_IMAGE":   {Component: "kserve", ODH: &config.PlatformImage{Digest: "sha256:aaa"}},
					"RELATED_IMAGE_ODH_MLSERVER_IMAGE": {Source: "csv", ODH: &config.PlatformImage{Digest: "sha256:aaa"}},
				},
			},
			new: &config.ManifestsConfig{
				ImageOverrides: map[string]config.ImageOverride{
					"RELATED_IMAGE_ODH_KSERVE_IMAGE":   {Component: "kserve", ODH: &config.PlatformImage{Digest: "sha256:bbb"}},
					"RELATED_IMAGE_ODH_MLSERVER_IMAGE": {Source: "csv", ODH: &config.PlatformImage{Digest: "sha256:bbb"}},
				},
			},
			wantNames:        []string{"kserve"},
			wantUnattributed: []string{"RELATED_IMAGE_ODH_MLSERVER_IMAGE"},
		},
		{
			name: "imageOverride reassigned to a different component is attributed to both",
			old: &config.ManifestsConfig{
				ImageOverrides: map[string]config.ImageOverride{
					"RELATED_IMAGE_ODH_SHARED_IMAGE": {Component: "kserve", ODH: &config.PlatformImage{Digest: "sha256:aaa"}},
				},
			},
			new: &config.ManifestsConfig{
				ImageOverrides: map[string]config.ImageOverride{
					"RELATED_IMAGE_ODH_SHARED_IMAGE": {Component: "dashboard", ODH: &config.PlatformImage{Digest: "sha256:aaa"}},
				},
			},
			wantNames: []string{"dashboard", "kserve"},
		},
		{
			name: "imageOverride digest bump with the same Component is attributed once, not twice",
			old: &config.ManifestsConfig{
				ImageOverrides: map[string]config.ImageOverride{
					"RELATED_IMAGE_ODH_KSERVE_IMAGE": {Component: "kserve", ODH: &config.PlatformImage{Digest: "sha256:aaa"}},
				},
			},
			new: &config.ManifestsConfig{
				ImageOverrides: map[string]config.ImageOverride{
					"RELATED_IMAGE_ODH_KSERVE_IMAGE": {Component: "kserve", ODH: &config.PlatformImage{Digest: "sha256:bbb"}},
				},
			},
			wantNames: []string{"kserve"},
		},
		{
			name:                         "platformManifests changed alone",
			old:                          &config.ManifestsConfig{PlatformManifests: map[string]string{"rhoai": "config"}},
			new:                          &config.ManifestsConfig{PlatformManifests: map[string]string{"rhoai": "config-v2"}},
			wantPlatformManifestsChanged: true,
		},
		{
			name: "platformManifests changed alongside an unrelated component must still be flagged, not dropped for a partial scope",
			old: &config.ManifestsConfig{
				Components:        map[string]config.Component{"kserve": {ODH: repo("main@aaa")}},
				PlatformManifests: map[string]string{"rhoai": "config"},
			},
			new: &config.ManifestsConfig{
				Components:        map[string]config.Component{"kserve": {ODH: repo("main@bbb")}},
				PlatformManifests: map[string]string{"rhoai": "config-v2"},
			},
			wantNames:                    []string{"kserve"},
			wantPlatformManifestsChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := manifestdiff.ChangedNames(tt.old, tt.new)
			gotNames, gotUnattributed := diff.Names, diff.UnattributedEnvVars

			slices.Sort(gotNames)
			wantSorted := slices.Clone(tt.wantNames)
			slices.Sort(wantSorted)
			if !slices.Equal(gotNames, wantSorted) {
				t.Errorf("names = %v, want %v", gotNames, wantSorted)
			}

			slices.Sort(gotUnattributed)
			wantUnattributedSorted := slices.Clone(tt.wantUnattributed)
			slices.Sort(wantUnattributedSorted)
			if !slices.Equal(gotUnattributed, wantUnattributedSorted) {
				t.Errorf("unattributedEnvVars = %v, want %v", gotUnattributed, wantUnattributedSorted)
			}

			if diff.PlatformManifestsChanged != tt.wantPlatformManifestsChanged {
				t.Errorf("platformManifestsChanged = %v, want %v", diff.PlatformManifestsChanged, tt.wantPlatformManifestsChanged)
			}
		})
	}
}
