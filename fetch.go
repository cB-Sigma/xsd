// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package xsd

import (
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"strings"
)

// LoadOptions adjust LoadWith.
type LoadOptions struct {
	// Fetch reads a schema document named by an absolute URL: an
	// xs:include, xs:import or xs:redefine whose schemaLocation is not in
	// the file system (http://..., urn:...), and relative locations inside
	// such a document. The loader never reaches the network itself: without
	// Fetch those documents are not read (a warning, or, for a component
	// that is then referenced, an error). Fetch may serve local copies
	// (a catalog) or go to the network with its own client and limits.
	Fetch func(url string) ([]byte, error)
	// MaxFetches bounds how many documents one load fetches; 0 means
	// DefaultMaxFetches, a negative value no bound.
	MaxFetches int
}

// DefaultMaxFetches is how many documents a load fetches unless
// LoadOptions say otherwise.
const DefaultMaxFetches = 64

func isURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme != "" && len(u.Scheme) > 1
}

// locate resolves a schemaLocation written in document d: a path in the
// file system, or (remote) an absolute URL.
func (l *loader) locate(d *schemaDoc, loc string) (string, bool, error) {
	loc = strings.TrimSpace(loc)
	if isURL(d.path) {
		base, err := url.Parse(d.path)
		if err != nil {
			return "", false, err
		}
		ref, err := url.Parse(loc)
		if err != nil {
			return "", false, fmt.Errorf("schemaLocation %s: %v", loc, err)
		}
		return base.ResolveReference(ref).String(), true, nil
	}
	if isURL(loc) || strings.HasPrefix(loc, "/") {
		return loc, true, nil
	}
	target := path.Join(path.Dir(d.path), loc)
	if strings.HasPrefix(target, "../") || target == ".." {
		return "", false, fmt.Errorf("schemaLocation %s points outside the schema files", loc)
	}
	return target, false, nil
}

// fetchable reads a remote document through Fetch, once; it reports
// whether the document is there to load. A failure is a warning.
func (l *loader) fetchable(c *node, from, target string) bool {
	if _, ok := l.fetched[target]; ok {
		return true
	}
	if l.opt.Fetch == nil || !isURL(target) {
		return false
	}
	max := l.opt.MaxFetches
	if max == 0 {
		max = DefaultMaxFetches
	}
	if max > 0 && len(l.fetched) >= max {
		l.s.warnings = append(l.s.warnings, Issue{Line: c.line, Path: from, Kind: IssueOther,
			Msg: fmt.Sprintf("%s: not fetched, the load already fetched %d documents (the limit)", target, max)})
		return false
	}
	b, err := l.opt.Fetch(target)
	if err != nil {
		l.s.warnings = append(l.s.warnings, Issue{Line: c.line, Path: from, Kind: IssueOther,
			Msg: fmt.Sprintf("%s: not fetched (%v)", target, err)})
		return false
	}
	l.fetched[target] = b
	return true
}

// read returns a schema document's bytes: fetched or from the file system.
func (l *loader) read(p string) ([]byte, error) {
	if b, ok := l.fetched[p]; ok {
		return b, nil
	}
	return fs.ReadFile(l.fsys, p)
}
