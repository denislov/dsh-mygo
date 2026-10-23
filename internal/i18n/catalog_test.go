package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestStaticViewTranslationsAndFormatArguments(t *testing.T) {
	files, err := filepath.Glob("../view/*.go")
	if err != nil || len(files) == 0 {
		t.Fatal("view sources missing")
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "T" {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			key, _ := strconv.Unquote(literal.Value)
			if chinese[key] == "" {
				t.Errorf("missing Chinese text in %s: %q", path, key)
			}
			return true
		})
	}
	formats := regexp.MustCompile(`%[0-9.]*[ds]`)
	for key, value := range chinese {
		if value == "" || !reflect.DeepEqual(formats.FindAllString(key, -1), formats.FindAllString(value, -1)) {
			t.Errorf("invalid translation: %q -> %q", key, value)
		}
		if Text("en-US", key) != key {
			t.Fatal("English changed")
		}
	}
}
