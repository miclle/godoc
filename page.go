package godoc

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"text/template"

	"github.com/miclle/godoc/vfs/httpfs"
)

// Page describes the contents of the top-level godoc webpage.
type Page struct {
	Title    string
	Tabtitle string
	Subtitle string
	SrcPath  string
	Query    string

	Sidebar []byte // Sidebar content
	Body    []byte // Main content

	// filled in by ServePage
	Playground bool
	Version    string

	Directory *Directory // sidebar directory
	PageInfo  *PageInfo  // current package info
}

// Presentation generates output from a corpus.
type Presentation struct {
	Corpus *Corpus

	mux        *http.ServeMux
	fileServer http.Handler
	cmdHandler handlerServer
	pkgHandler handlerServer

	LayoutHTML,
	SidebarHTML,

	DirlistHTML,
	ErrorHTML,
	ExampleHTML,

	PackageHTML,
	PackageRootHTML *template.Template // If not nil

	// TabWidth optionally specifies the tab width.
	TabWidth int

	ShowTimestamps bool
	ShowPlayground bool
	DeclLinks      bool

	// NotesRx optionally specifies a regexp to match
	// notes to render in the output.
	NotesRx *regexp.Regexp

	// AdjustPageInfoMode optionally specifies a function to
	// modify the PageInfoMode of a request. The default chosen
	// value is provided.
	AdjustPageInfoMode func(req *http.Request, mode PageInfoMode) PageInfoMode

	// URLForSrc optionally specifies a function that takes a source file and
	// returns a URL for it.
	// The source file argument has the form /src/<path>/<filename>.
	URLForSrc func(src string) string

	// URLForSrcPos optionally specifies a function to create a URL given a
	// source file, a line from the source file (1-based), and low & high offset
	// positions (0-based, bytes from beginning of file). Ideally, the returned
	// URL will be for the specified line of the file, while the high & low
	// positions will be used to highlight a section of the file.
	// The source file argument has the form /src/<path>/<filename>.
	URLForSrcPos func(src string, line, low, high int) string

	// URLForSrcQuery optionally specifies a function to create a URL given a
	// source file, a query string, and a line from the source file (1-based).
	// The source file argument has the form /src/<path>/<filename>.
	// The query argument will be escaped for the purposes of embedding in a URL
	// query parameter.
	// Ideally, the returned URL will be for the specified line of the file with
	// the query string highlighted.
	URLForSrcQuery func(src, query string, line int) string

	initFuncMapOnce sync.Once
	funcMap         template.FuncMap
	templateFuncs   template.FuncMap
}

// NewPresentation returns a new Presentation from a corpus.
// It sets SearchResults to:
// [SearchResultDoc SearchResultCode SearchResultTxt].
func NewPresentation(c *Corpus) *Presentation {
	if c == nil {
		panic("nil Corpus")
	}
	p := &Presentation{
		Corpus:     c,
		mux:        http.NewServeMux(),
		fileServer: http.FileServer(httpfs.New(c.fs)),

		TabWidth:  4,
		DeclLinks: true,
	}
	p.cmdHandler = handlerServer{
		presentation: p,
		corpus:       c,
		pattern:      "/cmd/",
		fsRoot:       "/src",
	}
	p.pkgHandler = handlerServer{
		presentation: p,
		corpus:       c,
		pattern:      "/pkg/",
		stripPrefix:  "pkg/",
		fsRoot:       "/src",
		exclude:      []string{"/src/cmd"},
	}
	p.cmdHandler.registerWithMux(p.mux)
	p.pkgHandler.registerWithMux(p.mux)
	p.mux.HandleFunc("/", p.ServeFile)
	return p
}

func (p *Presentation) FileServer() http.Handler {
	return p.fileServer
}

func (p *Presentation) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mux.ServeHTTP(w, r)
}

func (p *Presentation) PkgFSRoot() string {
	return p.pkgHandler.fsRoot
}

func (p *Presentation) CmdFSRoot() string {
	return p.cmdHandler.fsRoot
}

func (p *Presentation) ServePage(w http.ResponseWriter, page Page) {
	if page.Tabtitle == "" {
		page.Tabtitle = page.Title
	}
	page.Playground = p.ShowPlayground
	page.Version = runtime.Version()

	// render page with layout
	applyTemplateToResponseWriter(w, p.LayoutHTML, page)
}

func (p *Presentation) ServeError(w http.ResponseWriter, r *http.Request, relpath string, err error) {
	w.WriteHeader(http.StatusNotFound)
	if perr, ok := err.(*os.PathError); ok {
		rel, err := filepath.Rel(runtime.GOROOT(), perr.Path)
		if err != nil {
			perr.Path = "REDACTED"
		} else {
			perr.Path = filepath.Join("$GOROOT", rel)
		}
	}
	p.ServePage(w, Page{
		Title:    "File " + relpath,
		Subtitle: relpath,
		Body:     applyTemplate(p.ErrorHTML, "errorHTML", err),
	})
}

// TODO(bradfitz): move this to be a method on Corpus. Just moving code around for now,
// but this doesn't feel right.
func (p *Presentation) GetPkgPageInfo(abspath, relpath string, mode PageInfoMode) *PageInfo {
	return p.pkgHandler.GetPageInfo(abspath, relpath, mode, "", "")
}

// TODO(bradfitz): move this to be a method on Corpus. Just moving code around for now,
// but this doesn't feel right.
func (p *Presentation) GetCmdPageInfo(abspath, relpath string, mode PageInfoMode) *PageInfo {
	return p.cmdHandler.GetPageInfo(abspath, relpath, mode, "", "")
}
