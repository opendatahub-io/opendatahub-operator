package generator

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

func addImportField(file *ast.File, componentName string) error {
	if file.Name.Name+".go" != "main.go" {
		return nil
	}
	var importDecl *ast.GenDecl
	for _, decl := range file.Decls {
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
			importDecl = genDecl
		}
	}

	if importDecl == nil {
		return fmt.Errorf("import declaration not found")
	}

	newImport := &ast.ImportSpec{
		Name: ast.NewIdent("_"),
		Path: &ast.BasicLit{
			Kind:  token.STRING,
			Value: fmt.Sprintf("\"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/%s\"", strings.ToLower(componentName)),
		},
	}

	// Ensure new import is added at the end
	importDecl.Specs = append(importDecl.Specs, newImport)
	return nil
}

func addStructField(ts *ast.TypeSpec, componentName string) error {
	st, ok := ts.Type.(*ast.StructType)
	if !ok {
		return fmt.Errorf("not a struct type")
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
	return nil
}

func addFieldsToStruct(log *logrus.Logger, componentName string, fp string) error {
	file, err := os.Open(fp)
	if err != nil {
		return fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	fs := token.NewFileSet()
	node, err := parser.ParseFile(fs, fp, file, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("error parsing file: %w", err)
	}
	ast.Inspect(node, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.File:
			if err = addImportField(n, componentName); err != nil {
				return false
			}
		case *ast.TypeSpec:
			switch n.Name.Name {
			case "Components", "ComponentsStatus":
				if err = addStructField(n, componentName); err != nil {
					return false
				}
			}
		}
		return true
	})

	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fs, node); err != nil {
		return fmt.Errorf("error storing buffer: %w", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("error formatting code: %w", err)
	}

	return os.WriteFile(fp, formatted, FilePerm)
}
