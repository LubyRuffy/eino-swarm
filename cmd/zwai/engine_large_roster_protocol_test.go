//go:build !windows

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestLargeFixtureProtocolReadsCompleteConstantStrings(t *testing.T) {
	parse := func(source string) *ast.File {
		t.Helper()
		tree, err := parser.ParseFile(token.NewFileSet(), "protocol.go", source, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		return tree
	}
	files := []*ast.File{
		parse("package protocol\nconst first, second = \"one\\n\", `two`\nconst shared = first + second\nconst alias = shared\n"),
		parse("package protocol\nfunc message() string { return (`start ` + alias + shared + \" end\") }\n"),
	}
	for name, want := range map[string]string{"first": "one\n", "second": "two", "shared": "one\ntwo", "alias": "one\ntwo", "message": "start one\ntwoone\ntwo end"} {
		got, err := largeProtocolString(files, name)
		if err != nil || got != want {
			t.Fatalf("%s = %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestLargeFixtureProtocolRefusesDynamicOrIncompleteReturns(t *testing.T) {
	for name, source := range map[string]string{
		"call":                `func message() string { return build() }`,
		"variable":            `var mutable = "prefix"; func message() string { return mutable + "suffix" }`,
		"unknown-left":        `func message() string { return unknown + "suffix" }`,
		"unknown-right":       `func message() string { return "prefix" + unknown }`,
		"cycle":               `const first = second; const second = first; func message() string { return first }`,
		"number":              `func message() string { return 123 }`,
		"other-operator":      `func message() string { return "prefix" - "suffix" }`,
		"multiple-statements": `func message() string { inspect(); return "text" }`,
		"no-body":             `func message() string`,
		"no-return":           `func message() string { inspect() }`,
		"multiple-values":     `func message() string { return "first", "second" }`,
		"missing":             `const other = "prefix"`,
	} {
		t.Run(name, func(t *testing.T) {
			tree, err := parser.ParseFile(token.NewFileSet(), "protocol.go", "package protocol\n"+source, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := largeProtocolString([]*ast.File{tree}, "message"); err == nil || got != "" {
				t.Fatalf("nonconstant protocol accepted as %q, %v", got, err)
			}
		})
	}
}
