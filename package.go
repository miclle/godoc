package godoc

import (
	"go/ast"
	"go/doc"
	"go/token"
)

// Package type
type Package struct {
	Dir           string // !important: directory containing package sources
	ImportPath    string // !important: import path of package in dir
	ImportComment string // path in import comment on package statement
	Name          string // package name
	Doc           string // package documentation string
	Stale         bool   // would 'go install' do anything for this package?
	StaleReason   string // why is Stale true?

	Imports   []string               // import paths used by this package
	Filenames []string               // all files
	Notes     map[string][]*doc.Note // Contains Buts, etc...

	// declarations
	Consts []*doc.Value
	Types  []*doc.Type
	Vars   []*doc.Value
	Funcs  []*doc.Func

	// Examples is a sorted list of examples associated with
	// the package. Examples are extracted from _test.go files provided to NewFromFiles.
	Examples []*doc.Example

	FSet       *token.FileSet       // nil if no package documentation
	DocPackage *doc.Package         // nil if no package documentation
	PAst       map[string]*ast.File // nil if no AST with package exports
	IsMain     bool                 // true for package main

	ParentImportPath string   // parent package ImportPath
	Parent           *Package // parent package, important: json must ignore, prevent cycle parsing
	SubPackages      Packages // subpackages
}

// Packages with package array
type Packages []*Package
