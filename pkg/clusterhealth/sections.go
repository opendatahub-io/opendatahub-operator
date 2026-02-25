package clusterhealth

// Section name constants for use in Config.OnlySections.
const (
	SectionNodes       = "nodes"
	SectionDeployments = "deployments"
	SectionPods        = "pods"
	SectionEvents      = "events"
	SectionQuotas      = "quotas"
	SectionOperator    = "operator"
	SectionDSCI        = "dsci"
	SectionDSC         = "dsc"
)

// Layer name constants. Layers group sections for common use cases.
// Use Config.Layers to run only checks in one or more layers.
const (
	// LayerInfrastructure is cluster-level health: nodes and resource quotas.
	LayerInfrastructure = "infrastructure"
	// LayerWorkload is on-cluster components: deployments, pods, events, operator, DSCI, DSC.
	LayerWorkload = "workload"
)

// layerSections maps each layer to its section names.
var layerSections = map[string][]string{
	LayerInfrastructure: {SectionNodes, SectionQuotas},
	LayerWorkload:       {SectionDeployments, SectionPods, SectionEvents, SectionOperator, SectionDSCI, SectionDSC},
}

// sectionsToRun returns the set of section names to run based on cfg.
// If OnlySections is non-empty, that list is used (after expanding any layer names).
// If Layers is non-empty (and OnlySections is empty), sections from those layers are used.
// Otherwise all sections are run.
func (c *Config) sectionsToRun() map[string]bool {
	if len(c.OnlySections) > 0 {
		return sliceToSet(expandSectionList(c.OnlySections))
	}
	if len(c.Layers) > 0 {
		var list []string
		seen := make(map[string]bool)
		for _, layer := range c.Layers {
			for _, s := range layerSections[layer] {
				if !seen[s] {
					seen[s] = true
					list = append(list, s)
				}
			}
		}
		return sliceToSet(list)
	}
	// run all
	return sliceToSet([]string{
		SectionNodes, SectionDeployments, SectionPods, SectionEvents,
		SectionQuotas, SectionOperator, SectionDSCI, SectionDSC,
	})
}

// expandSectionList expands any layer names in list to their section names.
func expandSectionList(list []string) []string {
	var out []string
	for _, name := range list {
		if sections, ok := layerSections[name]; ok {
			out = append(out, sections...)
		} else {
			out = append(out, name)
		}
	}
	return out
}

func sliceToSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}
