package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/addp/common/format"
)

func TestWorkerRegistersBuiltinFormats(t *testing.T) {
	if got := format.DetectFormat("source.mdb", nil); got != format.FormatAccess {
		t.Fatalf("DetectFormat(source.mdb) = %q, want %q", got, format.FormatAccess)
	}
}

// A scan may be queued by the Backend but is always consumed by this Worker.
// Keep the receipt client bound before any bounded runner can claim it.
func TestWorkerBindsUploadReceiptBeforeStartingBoundedRunner(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var binding, runner token.Pos
	ast.Inspect(source, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selector.Sel.Name == "NewBoundedRunner" {
			runner = call.Pos()
		}
		if selector.Sel.Name != "WithUploadReceiptReader" || len(call.Args) != 1 {
			return true
		}
		receiver, ok := selector.X.(*ast.Ident)
		if !ok || receiver.Name != "executionService" {
			return true
		}
		constructor, ok := call.Args[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		client, ok := constructor.Fun.(*ast.SelectorExpr)
		if ok && client.Sel.Name == "NewManagerUploadClient" {
			binding = call.Pos()
		}
		return true
	})
	if binding == token.NoPos || runner == token.NoPos || binding >= runner {
		t.Fatal("Worker must bind the authenticated Manager upload receipt client before claiming scans")
	}
}

func TestWorkerBindsProductionRegistryBeforeStartingBoundedRunner(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var registry, binding, runner token.Pos
	ast.Inspect(source, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "NewProductionRegistry":
			registry = call.Pos()
		case "WithProductionRegistry":
			if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "executionService" {
				binding = call.Pos()
			}
		case "NewBoundedRunner":
			runner = call.Pos()
		}
		return true
	})
	if registry == token.NoPos || binding == token.NoPos || runner == token.NoPos || registry >= binding || binding >= runner {
		t.Fatal("Worker must bind the shared authenticated production registry before claiming scans")
	}
}
