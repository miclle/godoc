package godoc

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc"
	"go/token"
	"html"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/yosssi/gohtml"

	"github.com/miclle/godoc/util"
	"github.com/miclle/godoc/vfs"
)

// handlerServer is a migration from an old godoc http Handler type.
// This should probably merge into something else.
type handlerServer struct {
	p           *Presentation
	c           *Corpus  // copy of p.Corpus
	pattern     string   // url pattern; e.g. "/pkg/"
	stripPrefix string   // prefix to strip from import path; e.g. "pkg/"
	fsRoot      string   // file system root to which the pattern is mapped; e.g. "/src"
	exclude     []string // file system paths to exclude; e.g. "/src/cmd"
}

func (handler *handlerServer) registerWithMux(mux *http.ServeMux) {
	mux.Handle(handler.pattern, handler)
}

// GetPageInfo returns the PageInfo for a package directory abspath. If the
// parameter genAST is set, an AST containing only the package exports is
// computed (PageInfo.PAst), otherwise package documentation (PageInfo.Doc)
// is extracted from the AST. If there is no corresponding package in the
// directory, PageInfo.PAst and PageInfo.DocPackage are nil. If there are no sub-
// directories, PageInfo.Directory is nil. If an error occurred, PageInfo.Err is
// set to the respective error but the error is not logged.
//
func (handler *handlerServer) GetPageInfo(abspath, relpath string, mode PageInfoMode, goos, goarch string) *PageInfo {

	pageInfo := &PageInfo{
		Dirname: abspath,
		Mode:    mode,
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
		fi, err := handler.c.fs.Stat(filepath.ToSlash(path))
		return err == nil && fi.IsDir()
	}
	ctxt.ReadDir = func(dir string) ([]os.FileInfo, error) {
		f, err := handler.c.fs.ReadDir(filepath.ToSlash(dir))
		filtered := make([]os.FileInfo, 0, len(f))
		for _, i := range f {
			if mode&NoFiltering != 0 || i.Name() != "internal" {
				filtered = append(filtered, i)
			}
		}
		return filtered, err
	}
	ctxt.OpenFile = func(name string) (r io.ReadCloser, err error) {
		data, err := vfs.ReadFile(handler.c.fs, filepath.ToSlash(name))
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
	if goos == "" && goarch == "" && relpath == "syscall/js" {
		goos, goarch = "js", "wasm"
	}
	if goos != "" {
		ctxt.GOOS = goos
	}
	if goarch != "" {
		ctxt.GOARCH = goarch
	}

	pkginfo, err := ctxt.ImportDir(abspath, 0)
	// continue if there are no Go source files; we still want the directory info
	if _, nogo := err.(*build.NoGoError); err != nil && !nogo {
		pageInfo.Err = err
		return pageInfo
	}

	// collect package files
	pkgname := pkginfo.Name
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
		files, err := handler.c.parseFiles(fset, relpath, abspath, pkgfiles)
		if err != nil {
			pageInfo.Err = err
			return pageInfo
		}

		// ignore any errors - they are due to unresolved identifiers
		pkg, _ := ast.NewPackage(fset, files, poorMansImporter, nil)

		// extract package documentation
		pageInfo.FSet = fset
		if mode&ShowSource == 0 {
			// show extracted documentation
			var m doc.Mode
			if mode&NoFiltering != 0 {
				m |= doc.AllDecls
			}
			if mode&AllMethods != 0 {
				m |= doc.AllMethods
			}

			importPath := path.Clean(relpath) // no trailing '/' in importpath
			pageInfo.DocPackage = doc.New(pkg, importPath, m)
			if mode&NoTypeAssoc != 0 {
				for _, t := range pageInfo.DocPackage.Types {
					pageInfo.DocPackage.Consts = append(pageInfo.DocPackage.Consts, t.Consts...)
					pageInfo.DocPackage.Vars = append(pageInfo.DocPackage.Vars, t.Vars...)
					pageInfo.DocPackage.Funcs = append(pageInfo.DocPackage.Funcs, t.Funcs...)
					t.Consts = nil
					t.Vars = nil
					t.Funcs = nil
				}
				// for now we cannot easily sort consts and vars since
				// go/doc.Value doesn't export the order information
				sort.Sort(funcsByName(pageInfo.DocPackage.Funcs))
			}

			// collect examples
			testfiles := append(pkginfo.TestGoFiles, pkginfo.XTestGoFiles...)
			files, err = handler.c.parseFiles(fset, relpath, abspath, testfiles)
			if err != nil {
				log.Println("parsing examples:", err)
			}
			pageInfo.Examples = collectExamples(handler.c, pkg, files)

			// collect any notes that we want to show
			if pageInfo.DocPackage.Notes != nil {
				// could regexp.Compile only once per godoc, but probably not worth it
				if rx := handler.p.NotesRx; rx != nil {
					for m, n := range pageInfo.DocPackage.Notes {
						if rx.MatchString(m) {
							if pageInfo.Notes == nil {
								pageInfo.Notes = make(map[string][]*doc.Note)
							}
							pageInfo.Notes[m] = n
						}
					}
				}
			}

		} else {
			// show source code
			// TODO(gri) Consider eliminating export filtering in this mode,
			//           or perhaps eliminating the mode altogether.
			if mode&NoFiltering == 0 {
				packageExports(fset, pkg)
			}
			pageInfo.PAst = files
		}
		pageInfo.IsMain = pkgname == "main"
	}

	directory, timestamp := handler.c.Directory(abspath)

	pageInfo.Directory = directory
	pageInfo.DirectoryTime = timestamp
	pageInfo.DirectoryFlat = mode&FlatDir != 0

	return pageInfo
}

type funcsByName []*doc.Func

func (s funcsByName) Len() int           { return len(s) }
func (s funcsByName) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s funcsByName) Less(i, j int) bool { return s[i].Name < s[j].Name }

func (handler *handlerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if redirect(w, r) {
		return
	}

	relpath := path.Clean(r.URL.Path[len(handler.stripPrefix)+1:])

	if !handler.corpusInitialized() {
		handler.p.ServeError(w, r, relpath, errors.New("scan is not yet complete. Please retry after a few moments"))
		return
	}

	abspath := path.Join(handler.fsRoot, relpath)
	mode := handler.p.GetPageInfoMode(r)
	if relpath == builtinPkgPath {
		// The fake built-in package contains unexported identifiers,
		// but we want to show them. Also, disable type association,
		// since it's not helpful for this fake package (see issue 6645).
		mode |= NoFiltering | NoTypeAssoc
	}
	pageInfo := handler.GetPageInfo(abspath, relpath, mode, r.FormValue("GOOS"), r.FormValue("GOARCH"))
	if pageInfo.Err != nil {
		log.Print(pageInfo.Err)
		handler.p.ServeError(w, r, relpath, pageInfo.Err)
		return
	}

	var tabtitle, title, subtitle string
	switch {
	case pageInfo.PAst != nil:
		for _, ast := range pageInfo.PAst {
			tabtitle = ast.Name.Name
			break
		}
	case pageInfo.DocPackage != nil:
		tabtitle = pageInfo.DocPackage.Name
	default:
		tabtitle = pageInfo.Dirname
		title = "Directory "
		if handler.p.ShowTimestamps {
			subtitle = "Last update: " + pageInfo.DirectoryTime.String()
		}
	}
	if title == "" {
		if pageInfo.IsMain {
			// assume that the directory name is the command name
			_, tabtitle = path.Split(relpath)
			title = "Command "
		} else {
			title = "Package "
		}
	}
	title += tabtitle

	// special cases for top-level package/command directories
	switch tabtitle {
	case "/src":
		title = "Packages"
		tabtitle = "Packages"
	case "/src/cmd":
		title = "Commands"
		tabtitle = "Commands"
	}

	page := Page{
		Title:    title,
		Tabtitle: tabtitle,
		Subtitle: subtitle,
		PageInfo: pageInfo,
	}

	// set sidebar info
	// ------------------------------------------------------------------
	page.Directory, _ = handler.c.Directory("/src")
	page.Sidebar = applyTemplate(handler.p.SidebarHTML, "sidebarHTML", page)
	// ------------------------------------------------------------------

	if pageInfo.Dirname == "/src" {
		page.Body = applyTemplate(handler.p.PackageRootHTML, "packageRootHTML", pageInfo)
	} else {
		page.Body = applyTemplate(handler.p.PackageHTML, "packageHTML", pageInfo)
	}

	handler.p.ServePage(w, page)
}

func (handler *handlerServer) corpusInitialized() bool {
	handler.c.initMu.RLock()
	defer handler.c.initMu.RUnlock()
	return handler.c.initDone
}

type PageInfoMode uint

const (
	PageInfoModeQueryString = "m" // query string where PageInfoMode is stored

	NoFiltering PageInfoMode = 1 << iota // do not filter exports
	AllMethods                           // show all embedded methods
	ShowSource                           // show source code, do not extract documentation
	FlatDir                              // show directory in a flat (non-indented) manner
	NoTypeAssoc                          // don't associate consts, vars, and factory functions with types (not exposed via ?m= query parameter, used for package builtin, see issue 6645)
)

// modeNames defines names for each PageInfoMode flag.
var modeNames = map[string]PageInfoMode{
	"all":     NoFiltering,
	"methods": AllMethods,
	"src":     ShowSource,
	"flat":    FlatDir,
}

// generate a query string for persisting PageInfoMode between pages.
func modeQueryString(mode PageInfoMode) string {
	if modeNames := mode.names(); len(modeNames) > 0 {
		return "?m=" + strings.Join(modeNames, ",")
	}
	return ""
}

// alphabetically sorted names of active flags for a PageInfoMode.
func (m PageInfoMode) names() []string {
	var names []string
	for name, mode := range modeNames {
		if m&mode != 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// GetPageInfoMode computes the PageInfoMode flags by analyzing the request
// URL form value "m". It is value is a comma-separated list of mode names
// as defined by modeNames (e.g.: m=src,text).
func (p *Presentation) GetPageInfoMode(r *http.Request) PageInfoMode {
	var mode PageInfoMode
	for _, k := range strings.Split(r.FormValue(PageInfoModeQueryString), ",") {
		if m, found := modeNames[strings.TrimSpace(k)]; found {
			mode |= m
		}
	}
	if p.AdjustPageInfoMode != nil {
		mode = p.AdjustPageInfoMode(r, mode)
	}
	return mode
}

// poorMansImporter returns a (dummy) package object named
// by the last path component of the provided package path
// (as is the convention for packages). This is sufficient
// to resolve package identifiers without doing an actual
// import. It never returns an error.
//
func poorMansImporter(imports map[string]*ast.Object, path string) (*ast.Object, error) {
	pkg := imports[path]
	if pkg == nil {
		// note that strings.LastIndex returns -1 if there is no "/"
		pkg = ast.NewObj(ast.Pkg, path[strings.LastIndex(path, "/")+1:])
		pkg.Data = ast.NewScope(nil) // required by ast.NewPackage for dot-import
		imports[path] = pkg
	}
	return pkg, nil
}

// globalNames returns a set of the names declared by all package-level
// declarations. Method names are returned in the form Receiver_Method.
func globalNames(pkg *ast.Package) map[string]bool {
	names := make(map[string]bool)
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			addNames(names, decl)
		}
	}
	return names
}

// collectExamples collects examples for pkg from testfiles.
func collectExamples(c *Corpus, pkg *ast.Package, testfiles map[string]*ast.File) []*doc.Example {
	var files []*ast.File
	for _, f := range testfiles {
		files = append(files, f)
	}

	var examples []*doc.Example
	globals := globalNames(pkg)
	for _, e := range doc.Examples(files...) {
		name := stripExampleSuffix(e.Name)
		if name == "" || globals[name] {
			examples = append(examples, e)
		} else if c.Verbose {
			log.Printf("skipping example 'Example%s' because '%s' is not a known function or type", e.Name, e.Name)
		}
	}

	return examples
}

// addNames adds the names declared by decl to the names set.
// Method names are added in the form ReceiverTypeName_Method.
func addNames(names map[string]bool, decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		name := d.Name.Name
		if d.Recv != nil {
			var typeName string
			switch r := d.Recv.List[0].Type.(type) {
			case *ast.StarExpr:
				typeName = r.X.(*ast.Ident).Name
			case *ast.Ident:
				typeName = r.Name
			}
			name = typeName + "_" + name
		}
		names[name] = true
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				names[s.Name.Name] = true
			case *ast.ValueSpec:
				for _, id := range s.Names {
					names[id.Name] = true
				}
			}
		}
	}
}

// packageExports is a local implementation of ast.PackageExports
// which correctly updates each package file's comment list.
// (The ast.PackageExports signature is frozen, hence the local
// implementation).
//
func packageExports(fset *token.FileSet, pkg *ast.Package) {
	for _, src := range pkg.Files {
		cmap := ast.NewCommentMap(fset, src, src.Comments)
		ast.FileExports(src)
		src.Comments = cmap.Filter(src).Comments()
	}
}

func applyTemplate(t *template.Template, name string, data interface{}) []byte {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		log.Printf("%s.Execute: %s", name, err)
	}
	return buf.Bytes()
}

// applyTemplateToResponseWriter uses an http.ResponseWriter as the io.Writer
// for the call to template.Execute.  It uses an io.Writer wrapper to capture
// errors from the underlying http.ResponseWriter.  Errors are logged only when
// they come from the template processing and not the Writer; this avoid
// polluting log files with error messages due to networking issues, such as
// client disconnects and http HEAD protocol violations.
func applyTemplateToResponseWriter(rw http.ResponseWriter, t *template.Template, data interface{}) {

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		log.Printf("%s.Execute: %s", t.Name(), err)
	}

	// format template html
	gohtml.Condense = true
	if _, err := rw.Write(gohtml.FormatBytes(buf.Bytes())); err != nil {
		log.Printf("http.ResponseWriter.Write: %s", err)
	}
}

func redirect(w http.ResponseWriter, r *http.Request) (redirected bool) {
	canonical := path.Clean(r.URL.Path)
	if !strings.HasSuffix(canonical, "/") {
		canonical += "/"
	}
	if r.URL.Path != canonical {
		url := *r.URL
		url.Path = canonical
		http.Redirect(w, r, url.String(), http.StatusMovedPermanently)
		redirected = true
	}
	return
}

func redirectFile(w http.ResponseWriter, r *http.Request) (redirected bool) {
	c := path.Clean(r.URL.Path)
	c = strings.TrimRight(c, "/")
	if r.URL.Path != c {
		url := *r.URL
		url.Path = c
		http.Redirect(w, r, url.String(), http.StatusMovedPermanently)
		redirected = true
	}
	return
}

func (p *Presentation) serveTextFile(w http.ResponseWriter, r *http.Request, abspath, relpath, title string) {
	src, err := vfs.ReadFile(p.Corpus.fs, abspath)
	if err != nil {
		log.Printf("ReadFile: %s", err)
		p.ServeError(w, r, relpath, err)
		return
	}

	if r.FormValue(PageInfoModeQueryString) == "text" {
		p.ServeText(w, src)
		return
	}

	h := r.FormValue("h")
	s := RangeSelection(r.FormValue("s"))
	// <a href="abcd">testlink</a>
	var buf bytes.Buffer
	if path.Ext(abspath) == ".go" {
		buf.WriteString("<pre>")
		formatGoSource(&buf, src, h, s)
		buf.WriteString("</pre>")
	} else {
		buf.WriteString("<pre>")
		FormatText(&buf, src, 1, false, h, s)
		buf.WriteString("</pre>")
	}
	fmt.Fprintf(&buf, `<p><a href="/%s?m=text">View as plain text</a></p>`, html.EscapeString(relpath))

	p.ServePage(w, Page{
		Title:    title,
		SrcPath:  relpath,
		Tabtitle: relpath,
		Body:     buf.Bytes(),
	})
}

// formatGoSource HTML-escapes Go source text and writes it to w,
// decorating it with the specified analysis links.
//
func formatGoSource(buf *bytes.Buffer, text []byte, pattern string, selection Selection) {
	// Emit to a temp buffer so that we can add line anchors at the end.
	saved, buf := buf, new(bytes.Buffer)

	// var i int
	// var link analysis.Link // shared state of the two funcs below
	segmentIter := func() (seg Segment) {
		// if i < len(links) {
		// 	link = links[i]
		// 	i++
		// 	seg = Segment{link.Start(), link.End()}
		// }
		return
	}

	linkWriter := func(w io.Writer, offs int, start bool) {
		// link.Write(w, offs, start)
	}

	comments := tokenSelection(text, token.COMMENT)
	var highlights Selection
	if pattern != "" {
		highlights = regexpSelection(text, pattern)
	}

	FormatSelections(buf, text, linkWriter, segmentIter, selectionTag, comments, highlights, selection)

	// Now copy buf to saved, adding line anchors.

	// The lineSelection mechanism can't be composed with our
	// linkWriter, so we have to add line spans as another pass.
	n := 1
	for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
		// The line numbers are inserted into the document via a CSS ::before
		// pseudo-element. This prevents them from being copied when users
		// highlight and copy text.
		// ::before is supported in 98% of browsers: https://caniuse.com/#feat=css-gencontent
		// This is also the trick Github uses to hide line numbers.
		//
		// The first tab for the code snippet needs to start in column 9, so
		// it indents a full 8 spaces, hence the two nbsp's. Otherwise the tab
		// character only indents a short amount.
		//
		// Due to rounding and font width Firefox might not treat 8 rendered
		// characters as 8 characters wide, and subsequently may treat the tab
		// character in the 9th position as moving the width from (7.5 or so) up
		// to 8. See
		// https://github.com/webcompat/web-bugs/issues/17530#issuecomment-402675091
		// for a fuller explanation. The solution is to add a CSS class to
		// explicitly declare the width to be 8 characters.
		fmt.Fprintf(saved, `<span id="L%d" class="ln">%6d&nbsp;&nbsp;</span>`, n, n)
		n++
		saved.Write(line)
		saved.WriteByte('\n')
	}
}

func (p *Presentation) serveDirectory(w http.ResponseWriter, r *http.Request, abspath, relpath string) {
	if redirect(w, r) {
		return
	}

	list, err := p.Corpus.fs.ReadDir(abspath)
	if err != nil {
		p.ServeError(w, r, relpath, err)
		return
	}

	p.ServePage(w, Page{
		Title:    "Directory",
		SrcPath:  relpath,
		Tabtitle: relpath,
		Body:     applyTemplate(p.DirlistHTML, "dirlistHTML", list),
	})
}

func (p *Presentation) ServeHTMLDoc(w http.ResponseWriter, r *http.Request, abspath, relpath string) {
	// get HTML body contents
	src, err := vfs.ReadFile(p.Corpus.fs, abspath)
	if err != nil {
		log.Printf("ReadFile: %s", err)
		p.ServeError(w, r, relpath, err)
		return
	}

	// if it begins with "<!DOCTYPE " assume it is standalone
	// html that doesn't need the template wrapping.
	if bytes.HasPrefix(src, doctype) {
		w.Write(src)
		return
	}

	// if it begins with a JSON blob, read in the metadata.
	meta, src, err := extractMetadata(src)
	if err != nil {
		log.Printf("decoding metadata %s: %v", relpath, err)
	}

	page := Page{
		Title:    meta.Title,
		Subtitle: meta.Subtitle,
	}

	// evaluate as template if indicated
	if meta.Template {
		tmpl, err := template.New("main").Funcs(p.TemplateFuncs()).Parse(string(src))
		if err != nil {
			log.Printf("parsing template %s: %v", relpath, err)
			p.ServeError(w, r, relpath, err)
			return
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, page); err != nil {
			log.Printf("executing template %s: %v", relpath, err)
			p.ServeError(w, r, relpath, err)
			return
		}
		src = buf.Bytes()
	}

	// if it's the language spec, add tags to EBNF productions
	if strings.HasSuffix(abspath, "go_spec.html") {
		var buf bytes.Buffer
		Linkify(&buf, src)
		src = buf.Bytes()
	}

	page.Body = src
	p.ServePage(w, page)
}

func (p *Presentation) ServeFile(w http.ResponseWriter, r *http.Request) {
	p.serveFile(w, r)
}

func (p *Presentation) serveFile(w http.ResponseWriter, r *http.Request) {
	relpath := r.URL.Path

	// Check to see if we need to redirect or serve another file.
	if m := p.Corpus.MetadataFor(relpath); m != nil {
		if m.Path != relpath {
			// Redirect to canonical path.
			http.Redirect(w, r, m.Path, http.StatusMovedPermanently)
			return
		}
		// Serve from the actual filesystem path.
		relpath = m.filePath
	}

	abspath := relpath
	relpath = relpath[1:] // strip leading slash

	switch path.Ext(relpath) {
	case ".html":
		if strings.HasSuffix(relpath, "/index.html") {
			// We'll show index.html for the directory.
			// Use the dir/ version as canonical instead of dir/index.html.
			http.Redirect(w, r, r.URL.Path[0:len(r.URL.Path)-len("index.html")], http.StatusMovedPermanently)
			return
		}
		p.ServeHTMLDoc(w, r, abspath, relpath)
		return

	case ".go":
		p.serveTextFile(w, r, abspath, relpath, "Source file")
		return
	}

	dir, err := p.Corpus.fs.Lstat(abspath)
	if err != nil {
		log.Print(err)
		p.ServeError(w, r, relpath, err)
		return
	}

	if dir != nil && dir.IsDir() {
		if redirect(w, r) {
			return
		}
		if index := path.Join(abspath, "index.html"); util.IsTextFile(p.Corpus.fs, index) {
			p.ServeHTMLDoc(w, r, index, index)
			return
		}
		p.serveDirectory(w, r, abspath, relpath)
		return
	}

	if util.IsTextFile(p.Corpus.fs, abspath) {
		if redirectFile(w, r) {
			return
		}
		p.serveTextFile(w, r, abspath, relpath, "Text file")
		return
	}

	p.fileServer.ServeHTTP(w, r)
}

func (p *Presentation) ServeText(w http.ResponseWriter, text []byte) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(text)
}
