package main

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
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

var (
	embeddedNamespace = regexp.MustCompile(`(?i)(in namespace|for namespace|namespace\s*[:=]?\s*%|Request\.Namespace)`)
	embeddedName      = regexp.MustCompile(`(?i)(\bname\s*[:=]?\s*%|\bnamed\s+%|Request\.Name)`)
)

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
		if !ok || sel.Sel.Name != "Error" || !isLogrError(pass, sel) {
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

		if msg, ok := messageMarker(pass, call.Args[1]); ok {
			if embeddedNamespace.MatchString(msg) || embeddedName.MatchString(msg) {
				{
					report(call.Args[1].Pos(), "odhlog: message embeds resource name or namespace; use structured keys \"name\" and \"namespace\"")
				}
			}
		}

		keys := kvKeys(pass, call.Args[2:])
		var namePos token.Pos
		hasName, hasKind := false, false
		for _, k := range keys {
			if want, bad := forbiddenKeys[k.name]; bad {
				report(k.pos, "odhlog: non-standard structured log key %q; use %q", k.name, want)
			}
			switch k.name {
			case "name":
				hasName, namePos = true, k.pos
			case "resourceKind":
				hasKind = true
			}
		}
		if hasName && !hasKind {
			report(namePos, "odhlog: missing required structured field \"resourceKind\"")
		}
	})

	return nil, nil
}

type kvKey struct {
	name string
	pos  token.Pos
}

func kvKeys(pass *analysis.Pass, args []ast.Expr) []kvKey {
	var keys []kvKey
	for i := 0; i < len(args); i += 2 {
		if s, ok := constString(pass, args[i]); ok {
			keys = append(keys, kvKey{name: s, pos: args[i].Pos()})
		}
	}
	return keys
}

func isLogrError(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || !sig.Variadic() || sig.Params().Len() != 3 {
		return false
	}
	p := sig.Params()
	if p.At(0).Type() != types.Universe.Lookup("error").Type() {
		return false
	}
	if b, ok := p.At(1).Type().(*types.Basic); !ok || b.Kind() != types.String {
		return false
	}
	slice, ok := p.At(2).Type().(*types.Slice)
	if !ok {
		return false
	}
	iface, ok := slice.Elem().Underlying().(*types.Interface)
	return ok && iface.NumMethods() == 0
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

// sprintfFormat reports the constant format string of a fmt.Sprintf call. The
// callee is resolved through the type information so aliased imports of fmt are
// recognized and identifiers that merely read "fmt" are not mistaken for it.
// Only the statically known format argument is inspected; formatted values are
// never evaluated.
func sprintfFormat(pass *analysis.Pass, e ast.Expr) (string, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 {
		return "", false
	}
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok || fn.FullName() != "fmt.Sprintf" {
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
			if nolintMatches(c.Text) {
				return true
			}
		}
	}
	return false
}

func nolintMatches(text string) bool {
	text = strings.TrimPrefix(text, "//")
	text = strings.TrimPrefix(text, "/*")
	text = strings.TrimSpace(text)

	const prefix = "nolint"

	if !strings.HasPrefix(text, prefix) {
		return false
	}

	rest := text[len(prefix):]

	if rest == "" || strings.HasPrefix(rest, " ") {
		return true
	}

	if !strings.HasPrefix(rest, ":") {
		return false
	}

	list := rest[1:]
	if i := strings.IndexAny(list, " \t"); i >= 0 {
		list = list[:i]
	}

	for _, name := range strings.Split(list, ",") {
		if strings.TrimSpace(name) == "odhlog" {
			return true
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
