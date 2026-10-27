package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// The constructor starts a Redis subscriber. Check the actual startup wiring:
// replacing an instance loses ownership of its still-running subscription.
func TestEngineCacheProductionLifecycle(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var instance string
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
		if !ok || factory.Sel.Name != "NewEngineCacheService" {
			return true
		}
		factories++
		if name, ok := assignment.Lhs[0].(*ast.Ident); ok {
			instance = name.Name
		}
		if len(call.Args) != 3 {
			t.Error("engine cache must receive its service credentials and Redis client")
		} else if value, ok := call.Args[1].(*ast.Ident); ok && value.Name == "nil" {
			t.Error("startup must not create a credentialless placeholder subscription")
		}
		return true
	})
	if factories != 1 || instance == "" {
		t.Fatalf("engine cache must have one production owner, got %d constructors", factories)
	}
	stops := 0
	ast.Inspect(file, func(node ast.Node) bool {
		deferred, ok := node.(*ast.DeferStmt)
		if !ok {
			return true
		}
		method, ok := deferred.Call.Fun.(*ast.SelectorExpr)
		if !ok || method.Sel.Name != "Stop" {
			return true
		}
		if receiver, ok := method.X.(*ast.Ident); ok && receiver.Name == instance {
			stops++
		}
		return true
	})
	if stops != 1 {
		t.Fatalf("engine cache subscriber must stop on process exit, got %d deferred stops", stops)
	}
}
