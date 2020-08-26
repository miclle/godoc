package godoc

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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

	TreeView bool // page needs to contain treeview related js and css

	// filled in by ServePage
	SearchBox       bool
	Playground      bool
	Version         string
	GoogleAnalytics string
}

func (p *Presentation) ServePage(w http.ResponseWriter, page Page) {
	if page.Tabtitle == "" {
		page.Tabtitle = page.Title
	}
	page.SearchBox = p.Corpus.IndexEnabled
	page.Playground = p.ShowPlayground
	page.Version = runtime.Version()
	page.GoogleAnalytics = p.GoogleAnalytics

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
		Title:           "File " + relpath,
		Subtitle:        relpath,
		Body:            applyTemplate(p.ErrorHTML, "errorHTML", err),
		GoogleAnalytics: p.GoogleAnalytics,
	})
}
