package view

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestViewDoesNotImportAppOrTransportPackages(t *testing.T) {
	forbidden := []string{
		"dsh-mygo/internal/app",
		"dsh-mygo/internal/channel",
		"dsh-mygo/internal/host",
		"dsh-mygo/internal/runtimes",
	}
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatal("view sources missing")
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range file.Imports {
			name, _ := strconv.Unquote(imported.Path.Value)
			for _, prefix := range forbidden {
				if name == prefix || strings.HasPrefix(name, prefix+"/") {
					t.Fatalf("%s imports forbidden package %s", path, name)
				}
			}
		}
	}
}
