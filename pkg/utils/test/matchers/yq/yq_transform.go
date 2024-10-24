package yq

import (
	"bytes"
	"fmt"

	"github.com/mikefarah/yq/v4/pkg/yqlib"
)

func Extract(expression string) func(in any) (any, error) {
	return func(in any) (any, error) {
		results, err := evaluate(expression, in)
		if err != nil {
			return false, err
		}

		out := new(bytes.Buffer)

		encoder := yqlib.NewYamlEncoder(yqlib.YamlPreferences{
			Indent:                      defaultIndent,
			ColorsEnabled:               false,
			LeadingContentPreProcessing: true,
			PrintDocSeparators:          true,
			UnwrapScalar:                true,
			EvaluateTogether:            false,
		})

		printer := yqlib.NewPrinter(encoder, yqlib.NewSinglePrinterWriter(out))
		if err := printer.PrintResults(results); err != nil {
			return "", fmt.Errorf("failure rendering results: %w", err)
		}

		return out.String(), nil
	}
}
