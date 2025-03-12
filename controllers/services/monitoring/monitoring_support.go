package monitoring

import (
	"context"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/utils/set"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	odhcli "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
)

const (
	prometheusConfigurationEntry = "prometheus.yml"
)

type PrometheusConfig struct {
	RuleFiles []string       `yaml:"rule_files"`
	Others    map[string]any `yaml:",inline"`
}

func (pc *PrometheusConfig) computeRules(
	ctx context.Context,
	cli *odhcli.Client,
	registry *cr.Registry,
) error {
	// Map component names to their rule prefixes
	dsc, err := cluster.GetDSC(ctx, cli)
	if err != nil {
		return fmt.Errorf("failed to get DataScienceCluster instance: %w", err)
	}

	s := set.New(pc.RuleFiles...)

	err = registry.ForEach(func(ch cr.ComponentHandler) error {
		rule, ok := componentRules[ch.GetName()]
		if !ok {
			return nil
		}

		ms := ch.GetManagementState(dsc)
		switch ms {
		case operatorv1.Removed:
			s = s.Delete(rule)
		case operatorv1.Managed:
			ci := ch.NewCRObject(dsc)
			ready, err := cluster.IsReady(ctx, cli, ci)
			if err != nil {
				return fmt.Errorf("failed to get component status %w", err)
			}
			if ready {
				s = s.Insert(rule)
			}
		default:
			return fmt.Errorf("unsuported management state %s", ms)
		}

		return nil
	})

	if err != nil {
		return err
	}

	pc.RuleFiles = s.SortedList()

	return nil
}
