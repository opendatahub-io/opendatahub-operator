package generator

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

func addFieldsToStruct(_ *logrus.Logger, componentName string) error {
	dscFP := filepath.Join(DscApiDir)
	file, err := os.Open(dscFP)
	if err != nil {
		return fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	fs := token.NewFileSet()
	node, err := parser.ParseFile(fs, dscFP, file, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("error parsing file: %w", err)
	}

	modified := false
	ast.Inspect(node, func(n ast.Node) bool {
		if ts, ok := n.(*ast.TypeSpec); ok {
			if ts.Name.Name == "Components" || ts.Name.Name == "ComponentsStatus" {
				modified = addStructField(ts, componentName)
			}
		}
		return true
	})

	if !modified {
		return errors.New("no modifications made to the file")
	}

	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fs, node); err != nil {
		return fmt.Errorf("error storing buffer: %w", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("error formatting code: %w", err)
	}

	return os.WriteFile(dscFP, formatted, FilePerm)
}

func addStructField(ts *ast.TypeSpec, componentName string) bool {
	st, ok := ts.Type.(*ast.StructType)
	if !ok {
		return false
	}

	var fieldType string
	comment := fmt.Sprintf("%s component configuration.", componentName)
	if ts.Name.Name == "ComponentsStatus" {
		comment = fmt.Sprintf("%s component status.", componentName)
		fieldType = fmt.Sprintf("componentApi.DSC%sStatus", componentName)
	} else {
		fieldType = fmt.Sprintf("componentApi.DSC%s", componentName)
	}

	st.Fields.List = append(st.Fields.List,
		&ast.Field{
			Doc: &ast.CommentGroup{
				List: []*ast.Comment{
					{
						Text: "// " + comment,
					},
				},
			},
			Names: []*ast.Ident{ast.NewIdent(componentName)},
			Type:  &ast.Ident{Name: fieldType},
			Tag:   &ast.BasicLit{Value: fmt.Sprintf("`json:\"%s,omitempty\"`", strings.ToLower(componentName))},
		},
	)
	return true
}
