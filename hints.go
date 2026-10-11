// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package xsd

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"path"
	"sort"
	"strings"
)

// Hint is one schema document a document names through
// xsi:schemaLocation (Namespace set) or xsi:noNamespaceSchemaLocation.
type Hint struct {
	Namespace string
	Location  string
}

// Hints lists the schema location hints of a document, on any element, in
// document order without repeats. The document is read within the default
// limits.
func Hints(doc []byte) ([]Hint, error) {
	d := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(asXML10(doc), []byte("\uFEFF"))))
	d.Strict = true
	d.CharsetReader = charsetReader
	var out []Hint
	seen := map[Hint]bool{}
	used := map[string]bool{} // namespaces of the elements and attributes met
	depth := 0
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("not well-formed XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth > DefaultMaxDepth {
				return nil, fmt.Errorf("elements are nested deeper than %d (the limit)", DefaultMaxDepth)
			}
			for _, a := range t.Attr {
				if a.Name.Space != nsXSI {
					continue
				}
				var hs []Hint
				switch a.Name.Local {
				case "schemaLocation":
					f := strings.Fields(a.Value)
					if len(f)%2 != 0 {
						return nil, fmt.Errorf("xsi:schemaLocation %q: namespace and location must come in pairs", a.Value)
					}
					for i := 0; i < len(f); i += 2 {
						hs = append(hs, Hint{Namespace: f[i], Location: f[i+1]})
					}
				case "noNamespaceSchemaLocation":
					if v := strings.TrimSpace(a.Value); v != "" {
						hs = append(hs, Hint{Location: v})
					}
				}
				for _, h := range hs {
					if seen[h] {
						continue
					}
					// XSD 1.0 (section 4.3.2): a new location for a
					// namespace once an element or attribute of it has
					// been met is an error
					if used[h.Namespace] {
						line, _ := d.InputPos()
						return nil, fmt.Errorf("line %d: a schema location for namespace %q comes after an element or attribute of that namespace", line, h.Namespace)
					}
					seen[h] = true
					out = append(out, h)
				}
			}
			used[t.Name.Space] = true
			for _, a := range t.Attr {
				if a.Name.Space != nsXSI && a.Name.Space != "xmlns" && !(a.Name.Space == "" && a.Name.Local == "xmlns") && a.Name.Space != "" {
					used[a.Name.Space] = true
				}
			}
		case xml.EndElement:
			depth--
		}
	}
}

// HintOptions adjust LoadHinted.
type HintOptions struct {
	LoadOptions
	// DocPath is where the document is: a path in the file system, or a
	// URL. Relative hint locations are resolved against it.
	DocPath string
	// Override, when set, makes a hint for a namespace the root documents
	// already declare load too (it then must not clash with them). By
	// default such a hint is ignored: the caller's schema wins, which XSD
	// 1.0 allows a processor to choose.
	Override bool
}

// LoadHinted assembles the schema for one document from the root schema
// documents (which may be none) and the documents the document's
// xsi:schemaLocation and xsi:noNamespaceSchemaLocation hints name. Hints
// are followed only through this function, never by Validate: a receiver
// that lets a sender choose the schema must say so by calling it.
// Locations are read from fsys, or through o.Fetch for URLs (never by the
// loader itself), within o.MaxFetches.
func LoadHinted(fsys fs.FS, o HintOptions, doc []byte, roots ...string) (*Schema, error) {
	hints, err := Hints(doc)
	if err != nil {
		return nil, err
	}
	declared := map[string]bool{}
	if len(roots) > 0 && !o.Override {
		base, err := loadWith(fsys, o.LoadOptions, nil, roots)
		if err != nil {
			return nil, err
		}
		for ns := range base.namespaces {
			declared[ns] = true
		}
	}
	files := append([]string(nil), roots...)
	have := map[string]bool{}
	for _, r := range roots {
		have[path.Clean(r)] = true
	}
	var urls []string
	for _, h := range hints {
		if declared[h.Namespace] {
			continue
		}
		loc, err := hintTarget(o.DocPath, h.Location)
		if err != nil {
			return nil, err
		}
		if have[loc] {
			continue
		}
		have[loc] = true
		if isURL(loc) {
			urls = append(urls, loc)
		}
		files = append(files, loc)
	}
	if len(files) == 0 {
		return nil, errors.New("no schema document given and the document has no schema location hints")
	}
	sort.Strings(urls)
	return loadWith(fsys, o.LoadOptions, urls, files)
}

// hintTarget resolves a hint's location against the document's.
func hintTarget(docPath, loc string) (string, error) {
	if isURL(docPath) {
		b, err := url.Parse(docPath)
		if err != nil {
			return "", err
		}
		r, err := url.Parse(loc)
		if err != nil {
			return "", fmt.Errorf("schema location %s: %v", loc, err)
		}
		return b.ResolveReference(r).String(), nil
	}
	if isURL(loc) {
		return loc, nil
	}
	target := path.Clean(path.Join(path.Dir(docPath), loc))
	if strings.HasPrefix(target, "../") || target == ".." || strings.HasPrefix(loc, "/") {
		return "", fmt.Errorf("schema location %s points outside the files given", loc)
	}
	return target, nil
}
