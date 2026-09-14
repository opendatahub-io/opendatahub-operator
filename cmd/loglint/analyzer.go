package main

import (
	"go/ast"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const doc = "report log.Error calls that violate the structured resource-context convention (RHAI-529)"

// Analyzer flags logr-style Error() calls with identity in the message,
// non-standard identity keys, or an explicit name key without resourceKind.
var Analyzer = &analysis.Analyzer{
	Name:     "odhlog",
	Doc:      doc,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

var embeddedIdentity = regexp.MustCompile(`(?i)(in namespace|Request\.Name)`)

var forbiddenKeys = map[string]string{
	"Request.Name":                   "name",
	"Request.Namespace":              "namespace",
	"DSCInitialization Request.Name": "name",
	"DSCInitialization":              "namespace",
	"resource_kind":                  "resourceKind",
	"ResourceKind":                   "resourceKind",
	"ns":                             "namespace",
}

func run(pass *analysis.Pass) (any, error) {
	insp, ok := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !ok {
		return nil, nil
	}
	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Error" {
			return
		}
		if len(call.Args) < 2 {
			return
		}
		if isTestFile(pass, call.Pos()) {
			return
		}
		if hasNolint(pass, call.Pos()) {
			return
		}

		msg, ok := stringLit(call.Args[1])
		if ok && embeddedIdentity.MatchString(msg) {
			pass.Reportf(call.Args[1].Pos(), "odhlog: message embeds resource name or namespace; use structured keys \"name\" and \"namespace\"")
		}

		keys := kvKeys(call.Args[2:])
		for _, key := range keys {
			if want, bad := forbiddenKeys[key]; bad {
				pass.Reportf(call.Pos(), "odhlog: non-standard structured log key %q; use %q", key, want)
			}
		}
		if slices.Contains(keys, "name") && !slices.Contains(keys, "resourceKind") {
			pass.Reportf(call.Pos(), "odhlog: missing required structured field \"resourceKind\"")
		}
	})

	return nil, nil
}

func kvKeys(args []ast.Expr) []string {
	var keys []string
	for i := 0; i < len(args); i += 2 {
		if s, ok := stringLit(args[i]); ok {
			keys = append(keys, s)
		}
	}
	return keys
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

func isTestFile(pass *analysis.Pass, pos token.Pos) bool {
	name := pass.Fset.Position(pos).Filename
	return strings.HasSuffix(name, "_test.go")
}

func hasNolint(pass *analysis.Pass, pos token.Pos) bool {
	file := fileFor(pass, pos)
	if file == nil {
		return false
	}
	line := pass.Fset.Position(pos).Line
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			if pass.Fset.Position(c.Pos()).Line != line {
				continue
			}
			if strings.Contains(c.Text, "nolint:odhlog") {
				return true
			}
		}
	}
	return false
}

func fileFor(pass *analysis.Pass, pos token.Pos) *ast.File {
	for _, f := range pass.Files {
		if f.Pos() <= pos && pos < f.End() {
			return f
		}
	}
	return nil
}
