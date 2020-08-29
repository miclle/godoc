package godoc

import (
	"testing"

	"github.com/miclle/godoc/vfs/mapfs"
)

// TestIgnoredGoFiles tests the scenario where a folder has no .go or .c files,
// but has an ignored go file.
func TestIgnoredGoFiles(t *testing.T) {
	packagePath := "github.com/package"
	packageComment := "main is documented in an ignored .go file"

	c := NewCorpus(mapfs.New(map[string]string{
		"src/" + packagePath + "/ignored.go": `// +build ignore

// ` + packageComment + `
package main`}))
	srv := &handlerServer{
		presentation: &Presentation{
			Corpus: c,
		},
		corpus: c,
	}
	pInfo := srv.GetPageInfo("/src/"+packagePath, packagePath, NoFiltering, "linux", "amd64")

	if pInfo.DocPackage == nil {
		t.Error("pInfo.DocPackage = nil; want non-nil.")
	} else {
		if got, want := pInfo.DocPackage.Doc, packageComment+"\n"; got != want {
			t.Errorf("pInfo.DocPackage.Doc = %q; want %q.", got, want)
		}
		if got, want := pInfo.DocPackage.Name, "main"; got != want {
			t.Errorf("pInfo.DocPackage.Name = %q; want %q.", got, want)
		}
		if got, want := pInfo.DocPackage.ImportPath, packagePath; got != want {
			t.Errorf("pInfo.DocPackage.ImportPath = %q; want %q.", got, want)
		}
	}
	if pInfo.FSet == nil {
		t.Error("pInfo.FSet = nil; want non-nil.")
	}
}

func TestIssue5247(t *testing.T) {
	const packagePath = "example.com/p"
	c := NewCorpus(mapfs.New(map[string]string{
		"src/" + packagePath + "/p.go": `package p

//line notgen.go:3
// F doc //line 1 should appear
// line 2 should appear
func F()
//line foo.go:100`})) // No newline at end to check corner cases.

	srv := &handlerServer{
		presentation: &Presentation{Corpus: c},
		corpus:       c,
	}
	pInfo := srv.GetPageInfo("/src/"+packagePath, packagePath, 0, "linux", "amd64")
	if got, want := pInfo.DocPackage.Funcs[0].Doc, "F doc //line 1 should appear\nline 2 should appear\n"; got != want {
		t.Errorf("pInfo.DocPackage.Funcs[0].Doc = %q; want %q", got, want)
	}
}
