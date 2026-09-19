package main

import (
	"go/ast"
	"go/constant"
	"go/token"
	"regexp"
	"slices"
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

		// report suppresses a diagnostic when a nolint:odhlog directive sits on
		// the diagnostic's own line, so a directive beside a message in a
		// multiline Error call is honored rather than only one on the call's
		// opening line.
		report := func(pos token.Pos, format string, args ...any) {
			if hasNolint(pass, pos) {
				return
			}
			pass.Reportf(pos, format, args...)
		}

		if msg, ok := messageMarker(pass, call.Args[1]); ok && embeddedIdentity.MatchString(msg) {
			report(call.Args[1].Pos(), "odhlog: message embeds resource name or namespace; use structured keys \"name\" and \"namespace\"")
		}

		keys := kvKeys(pass, call.Args[2:])
		for _, key := range keys {
			if want, bad := forbiddenKeys[key]; bad {
				report(call.Pos(), "odhlog: non-standard structured log key %q; use %q", key, want)
			}
		}
		if slices.Contains(keys, "name") && !slices.Contains(keys, "resourceKind") {
			report(call.Pos(), "odhlog: missing required structured field \"resourceKind\"")
		}
	})

	return nil, nil
}

func kvKeys(pass *analysis.Pass, args []ast.Expr) []string {
	var keys []string
	for i := 0; i < len(args); i += 2 {
		if s, ok := constString(pass, args[i]); ok {
			keys = append(keys, s)
		}
	}
	return keys
}

// messageMarker returns the static text to scan for embedded identity. It
// recognizes compile-time constant strings (literals, string consts, and
// constant concatenations) as well as the format-string argument of a
// fmt.Sprintf call. Genuinely dynamic expressions yield ok == false so they
// are left unflagged rather than guessed at.
func messageMarker(pass *analysis.Pass, e ast.Expr) (string, bool) {
	if format, ok := sprintfFormat(pass, e); ok {
		return format, true
	}
	return constString(pass, e)
}

// sprintfFormat reports the constant format string of a fmt.Sprintf call. Only
// the statically known format argument is inspected; formatted values are never
// evaluated.
func sprintfFormat(pass *analysis.Pass, e ast.Expr) (string, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Sprintf" {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "fmt" || len(call.Args) == 0 {
		return "", false
	}
	return constString(pass, call.Args[0])
}

// constString returns the value of a compile-time constant string expression,
// covering literals, named string constants, and constant concatenations.
func constString(pass *analysis.Pass, e ast.Expr) (string, bool) {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
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
