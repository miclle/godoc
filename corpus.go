package godoc

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc"
	"go/token"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/miclle/godoc/util"
	"github.com/miclle/godoc/vfs"
)

// A Corpus holds all the state related to serving and indexing a
// collection of Go code.
//
// Construct a new Corpus with NewCorpus, then modify options,
// then call its Init method.
type Corpus struct {
	fs vfs.FileSystem

	// Verbose logging.
	Verbose bool

	// SummarizePackage optionally specifies a function to
	// summarize a package. It exists as an optimization to
	// avoid reading files to parse package comments.
	//
	// If SummarizePackage returns false for ok, the caller
	// ignores all return values and parses the files in the package
	// as if SummarizePackage were nil.
	//
	// If showList is false, the package is hidden from the
	// package listing.
	SummarizePackage func(pkg string) (summary string, showList, ok bool)

	// file system information
	fsTree      util.RWValue // *Directory tree of packages, updated with each sync (but sync code is removed now)
	docMetadata util.RWValue // mapping from paths to *Metadata

	// flag to check whether a corpus is initialized or not
	initMu   sync.RWMutex
	initDone bool

	// pkgAPIInfo contains the information about which package API
	// features were added in which version of Go.
	pkgAPIInfo apiVersions
}

// NewCorpus returns a new Corpus from a filesystem.
// The returned corpus has all indexing enabled and MaxResults set to 1000.
// Change or set any options on Corpus before calling the Corpus.Init method.
func NewCorpus(fs vfs.FileSystem) *Corpus {
	c := &Corpus{
		fs: fs,
	}
	return c
}

// Init initializes Corpus, once options on Corpus are set.
// It must be called before any subsequent method calls.
func (c *Corpus) Init() error {
	if err := c.initFSTree(); err != nil {
		return err
	}

	c.initMu.Lock()
	c.initDone = true
	c.initMu.Unlock()
	return nil
}

func (c *Corpus) initFSTree() error {
	dir := c.newDirectory("/", -1)
	if dir == nil {
		return errors.New("godoc: corpus fstree is nil")
	}
	c.fsTree.Set(dir)
	return nil
}

// Directory return tree with abspath
func (c *Corpus) Directory(abspath string) (*Directory, time.Time) {

	var (
		directory *Directory
		timestamp time.Time
	)

	// get directory information, if any
	if tree, ts := c.fsTree.Get(); tree != nil && tree.(*Directory) != nil {
		// directory tree is present; lookup respective directory
		// (may still fail if the file system was updated and the
		// new directory tree has not yet been computed)
		directory = tree.(*Directory).lookup(abspath)
		timestamp = ts
	}

	if directory == nil {
		// TODO(agnivade): handle this case better, now since there is no CLI mode.
		// no directory tree present (happens in command-line mode);
		// compute 2 levels for this page. The second level is to
		// get the synopses of sub-directories.
		// note: cannot use path filter here because in general
		// it doesn't contain the FSTree path
		directory = c.newDirectory(abspath, 2)
		timestamp = time.Now()
	}

	return directory, timestamp
}

// SyntaxAnalysis parse dir
func (c *Corpus) SyntaxAnalysis(abspath string) error {

	mode := NoFiltering

	p := &Package{
		Dir: abspath,
	}

	// Restrict to the package files that would be used when building
	// the package on this system.  This makes sure that if there are
	// separate implementations for, say, Windows vs Unix, we don't
	// jumble them all together.
	// Note: If goos/goarch aren't set, the current binary's GOOS/GOARCH
	// are used.
	ctxt := build.Default
	ctxt.IsAbsPath = path.IsAbs
	ctxt.IsDir = func(path string) bool {
		fi, err := c.fs.Stat(filepath.ToSlash(path))
		return err == nil && fi.IsDir()
	}
	ctxt.ReadDir = func(dir string) ([]os.FileInfo, error) {
		f, err := c.fs.ReadDir(filepath.ToSlash(dir))
		filtered := make([]os.FileInfo, 0, len(f))
		for _, i := range f {
			if mode&NoFiltering != 0 || i.Name() != "internal" {
				filtered = append(filtered, i)
			}
		}
		return filtered, err
	}
	ctxt.OpenFile = func(name string) (r io.ReadCloser, err error) {
		data, err := vfs.ReadFile(c.fs, filepath.ToSlash(name))
		if err != nil {
			return nil, err
		}
		return ioutil.NopCloser(bytes.NewReader(data)), nil
	}

	// Make the syscall/js package always visible by default.
	// It defaults to the host's GOOS/GOARCH, and golang.org's
	// linux/amd64 means the wasm syscall/js package was blank.
	// And you can't run godoc on js/wasm anyway, so host defaults
	// don't make sense here.
	var goos, goarch string
	if goos != "" {
		ctxt.GOOS = goos
	}
	if goarch != "" {
		ctxt.GOARCH = goarch
	}

	pkginfo, err := ctxt.ImportDir(abspath, 0)
	// continue if there are no Go source files; we still want the directory info
	if _, nogo := err.(*build.NoGoError); err != nil && !nogo {
		return err
	}

	// collect package files
	pkgname := pkginfo.Name

	fmt.Println("pkgname: ", pkgname)

	pkgfiles := append(pkginfo.GoFiles, pkginfo.CgoFiles...)
	if len(pkgfiles) == 0 {
		// Commands written in C have no .go files in the build.
		// Instead, documentation may be found in an ignored file.
		// The file may be ignored via an explicit +build ignore
		// constraint (recommended), or by defining the package
		// documentation (historic).
		pkgname = "main" // assume package main since pkginfo.Name == ""
		pkgfiles = pkginfo.IgnoredGoFiles
	}

	// get package information, if any
	if len(pkgfiles) > 0 {
		// build package AST
		fset := token.NewFileSet()
		files, err := c.parseFiles(fset, "", abspath, pkgfiles)
		if err != nil {
			return err
		}

		// ignore any errors - they are due to unresolved identifiers
		pkg, _ := ast.NewPackage(fset, files, poorMansImporter, nil)

		// extract package documentation
		p.FSet = fset
		if mode&ShowSource == 0 {
			// show extracted documentation
			var m doc.Mode
			if mode&NoFiltering != 0 {
				m |= doc.AllDecls
			}
			if mode&AllMethods != 0 {
				m |= doc.AllMethods
			}

			importPath := path.Clean("") // no trailing '/' in importpath
			p.DocPackage = doc.New(pkg, importPath, m)
			if mode&NoTypeAssoc != 0 {
				for _, t := range p.DocPackage.Types {
					p.DocPackage.Consts = append(p.DocPackage.Consts, t.Consts...)
					p.DocPackage.Vars = append(p.DocPackage.Vars, t.Vars...)
					p.DocPackage.Funcs = append(p.DocPackage.Funcs, t.Funcs...)
					t.Consts = nil
					t.Vars = nil
					t.Funcs = nil
				}
				// for now we cannot easily sort consts and vars since
				// go/doc.Value doesn't export the order information
				sort.Sort(funcsByName(p.DocPackage.Funcs))
			}

			// collect examples
			testfiles := append(pkginfo.TestGoFiles, pkginfo.XTestGoFiles...)
			files, err = c.parseFiles(fset, "", abspath, testfiles)
			if err != nil {
				log.Println("parsing examples:", err)
			}
			p.Examples = collectExamples(c, pkg, files)

			// collect any notes that we want to show
			if p.DocPackage.Notes != nil {
				for m, n := range p.DocPackage.Notes {
					if p.Notes == nil {
						p.Notes = make(map[string][]*doc.Note)
					}
					p.Notes[m] = n
				}
			}

		} else {
			// show source code
			// TODO(gri) Consider eliminating export filtering in this mode,
			//           or perhaps eliminating the mode altogether.
			if mode&NoFiltering == 0 {
				packageExports(fset, pkg)
			}
			p.PAst = files
		}

		p.IsMain = pkgname == "main"
	}

	fmt.Printf("package: %#v", p)

	return nil
}
