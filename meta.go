package godoc

import (
	"bytes"
	"encoding/json"
	"strings"
)

var (
	doctype   = []byte("<!DOCTYPE ")
	jsonStart = []byte("<!--{")
	jsonEnd   = []byte("}-->")
)

// ----------------------------------------------------------------------------
// Documentation Metadata

// TODO(adg): why are some exported and some aren't? -brad
type Metadata struct {
	Title    string
	Subtitle string
	Template bool   // execute as template
	Path     string // canonical path for this page
	filePath string // filesystem path relative to goroot
}

// extractMetadata extracts the Metadata from a byte slice.
// It returns the Metadata value and the remaining data.
// If no metadata is present the original byte slice is returned.
//
func extractMetadata(b []byte) (meta Metadata, tail []byte, err error) {
	tail = b
	if !bytes.HasPrefix(b, jsonStart) {
		return
	}
	end := bytes.Index(b, jsonEnd)
	if end < 0 {
		return
	}
	b = b[len(jsonStart)-1 : end+1] // drop leading <!-- and include trailing }
	if err = json.Unmarshal(b, &meta); err != nil {
		return
	}
	tail = tail[end+len(jsonEnd):]
	return
}

// MetadataFor returns the *Metadata for a given relative path or nil if none
// exists.
//
func (c *Corpus) MetadataFor(relpath string) *Metadata {
	if m, _ := c.docMetadata.Get(); m != nil {
		meta := m.(map[string]*Metadata)
		// If metadata for this relpath exists, return it.
		if p := meta[relpath]; p != nil {
			return p
		}
		// Try with or without trailing slash.
		if strings.HasSuffix(relpath, "/") {
			relpath = relpath[:len(relpath)-1]
		} else {
			relpath = relpath + "/"
		}
		return meta[relpath]
	}
	return nil
}
