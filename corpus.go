package godoc

import (
	"errors"
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
