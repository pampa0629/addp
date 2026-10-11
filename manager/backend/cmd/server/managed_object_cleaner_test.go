package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Service tests exercise deletion behavior; this contract also checks the real
// startup wiring that originally sent PDF objects to the PMTiles cleaner.
func TestManagedObjectCleanerProductionBindings(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var cleaner string
	factories := 0
	ast.Inspect(file, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
			return true
		}
		call, ok := assignment.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		factory, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || factory.Sel.Name != "NewMinIOManagedObjectCleaner" {
			return true
		}
		if name, ok := assignment.Lhs[0].(*ast.Ident); ok {
			cleaner = name.Name
			factories++
		}
		return true
	})
	if factories != 1 {
		t.Fatalf("single object cleanup must have one production instance, got %d", factories)
	}
	bindings := map[string]int{
		"pptxPDFTaskSvc": 0, "rasterCOGTaskSvc": 0, "pointCloudCOPCTaskSvc": 0,
		"model3DGLBTaskSvc": 0, "gaussianSplatKSplatTaskSvc": 0,
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		setter, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || setter.Sel.Name != "SetCleaner" {
			return true
		}
		receiver, ok := setter.X.(*ast.Ident)
		if !ok {
			return true
		}
		if _, managed := bindings[receiver.Name]; !managed {
			return true
		}
		if len(call.Args) != 1 {
			t.Errorf("%s must bind the shared object cleaner", receiver.Name)
			return true
		}
		argument, ok := call.Args[0].(*ast.Ident)
		if !ok || argument.Name != cleaner {
			t.Errorf("%s must bind the shared object cleaner", receiver.Name)
		}
		bindings[receiver.Name]++
		return true
	})
	for service, count := range bindings {
		if count != 1 {
			t.Errorf("%s has %d cleaner bindings, want one", service, count)
		}
	}
}
