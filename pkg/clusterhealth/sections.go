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
	// LayerOperator is operator and CRs: operator deployment, DSCI, DSC.
	LayerOperator = "operator"
)

var layerSections = map[string][]string{
	LayerInfrastructure: {SectionNodes, SectionQuotas},
	LayerWorkload:       {SectionDeployments, SectionPods, SectionEvents, SectionOperator, SectionDSCI, SectionDSC},
	LayerOperator:       {SectionOperator, SectionDSCI, SectionDSC},
}

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
	return sliceToSet([]string{
		SectionNodes, SectionDeployments, SectionPods, SectionEvents,
		SectionQuotas, SectionOperator, SectionDSCI, SectionDSC,
	})
}

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
