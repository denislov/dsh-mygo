// Command metrics reports reproducible production-Go AST hotspots. It skips
// tests, generated files, vendor and non-source directories; UI declaration
// length is reported, never treated as a failure threshold.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type item struct {
	Name  string `json:"name"`
	File  string `json:"file"`
	Lines int    `json:"lines"`
	Count int    `json:"count,omitempty"`
}

type report struct {
	Root         string `json:"root"`
	Files        int    `json:"files"`
	Functions    int    `json:"functions"`
	Methods      int    `json:"methods"`
	Structs      int    `json:"structs"`
	Fields       int    `json:"fields"`
	TopFunctions []item `json:"topFunctions"`
	TopStructs   []item `json:"topStructs"`
}

func main() {
	root := flag.String("root", ".", "repository root")
	top := flag.Int("top", 10, "number of hotspots to report (0 shows totals only)")
	jsonOutput := flag.Bool("json", false, "emit JSON")
	flag.Parse()
	result, err := analyzeRoot(*root, *top)
	if err != nil {
		fmt.Fprintln(os.Stderr, "metrics:", err)
		os.Exit(1)
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			fmt.Fprintln(os.Stderr, "metrics:", err)
			os.Exit(1)
		}
		return
	}
	fmt.Printf("production Go: files=%d functions=%d methods=%d structs=%d fields=%d\n", result.Files, result.Functions, result.Methods, result.Structs, result.Fields)
	for _, entry := range result.TopFunctions {
		fmt.Printf("func %4d lines  %s  (%s)\n", entry.Lines, entry.Name, entry.File)
	}
	for _, entry := range result.TopStructs {
		fmt.Printf("struct %2d fields  %s  (%s)\n", entry.Count, entry.Name, entry.File)
	}
}

func analyzeRoot(root string, top int) (report, error) {
	result := report{Root: root}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != root && (name == "vendor" || name == "node_modules" || name == "runtime" || name == "build" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if ast.IsGenerated(file) {
			return nil
		}
		result.Files++
		relative, _ := filepath.Rel(root, path)
		relative = filepath.ToSlash(relative)
		for _, declaration := range file.Decls {
			switch node := declaration.(type) {
			case *ast.FuncDecl:
				lineCount := fset.Position(node.End()).Line - fset.Position(node.Pos()).Line + 1
				name := node.Name.Name
				if node.Recv != nil {
					result.Methods++
					if len(node.Recv.List) > 0 {
						name = receiverName(node.Recv.List[0].Type) + "." + name
					}
				} else {
					result.Functions++
				}
				result.TopFunctions = append(result.TopFunctions, item{Name: name, File: relative, Lines: lineCount})
			case *ast.GenDecl:
				if node.Tok != token.TYPE {
					continue
				}
				for _, spec := range node.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					structType, ok := typeSpec.Type.(*ast.StructType)
					if !ok {
						continue
					}
					fields := len(structType.Fields.List)
					result.Structs++
					result.Fields += fields
					result.TopStructs = append(result.TopStructs, item{Name: typeSpec.Name.Name, File: relative, Count: fields})
				}
			}
		}
		return nil
	})
	if err != nil {
		return report{}, err
	}
	sort.Slice(result.TopFunctions, func(i, j int) bool {
		if result.TopFunctions[i].Lines != result.TopFunctions[j].Lines {
			return result.TopFunctions[i].Lines > result.TopFunctions[j].Lines
		}
		return result.TopFunctions[i].Name < result.TopFunctions[j].Name
	})
	sort.Slice(result.TopStructs, func(i, j int) bool {
		if result.TopStructs[i].Count != result.TopStructs[j].Count {
			return result.TopStructs[i].Count > result.TopStructs[j].Count
		}
		return result.TopStructs[i].Name < result.TopStructs[j].Name
	})
	if top >= 0 {
		if len(result.TopFunctions) > top {
			result.TopFunctions = result.TopFunctions[:top]
		}
		if len(result.TopStructs) > top {
			result.TopStructs = result.TopStructs[:top]
		}
	}
	return result, nil
}

func receiverName(expression ast.Expr) string {
	switch node := expression.(type) {
	case *ast.StarExpr:
		return receiverName(node.X)
	case *ast.Ident:
		return node.Name
	case *ast.IndexExpr:
		return receiverName(node.X)
	case *ast.IndexListExpr:
		return receiverName(node.X)
	default:
		return "receiver"
	}
}
