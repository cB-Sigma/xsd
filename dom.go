package xsd

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Namespaces the validator knows by heart.
const (
	nsXS  = "http://www.w3.org/2001/XMLSchema"
	nsXSI = "http://www.w3.org/2001/XMLSchema-instance"
	nsXML = "http://www.w3.org/XML/1998/namespace"
	nsNS  = "http://www.w3.org/2000/xmlns/"
)

// qname is an expanded name: namespace URI and local part.
type qname struct{ Space, Local string }

func (q qname) String() string {
	if q.Space == "" {
		return q.Local
	}
	return "{" + q.Space + "}" + q.Local
}

type attr struct {
	name   qname
	prefix string
	value  string
}

// node is one element of a parsed document (schema or instance).
type node struct {
	name     qname
	prefix   string
	attrs    []attr
	ns       *nsScope // in-scope prefix bindings ("" is the default namespace)
	children []*node
	parent   *node
	text     strings.Builder // character data directly inside the element
	line     int
}

// nsScope holds the prefix bindings an element declares, linked to the
// enclosing scope. Elements that declare nothing share their parent's
// scope, so the cost stays linear in the declarations written (copying the
// bindings into every element would be quadratic).
type nsScope struct {
	parent *nsScope
	m      map[string]string
}

func newScope(m map[string]string) *nsScope { return &nsScope{m: m} }

func (s *nsScope) lookup(prefix string) (string, bool) {
	for x := s; x != nil; x = x.parent {
		if v, ok := x.m[prefix]; ok {
			return v, true
		}
	}
	return "", false
}

func (n *node) attr(local string) (string, bool) {
	for _, a := range n.attrs {
		if a.name.Space == "" && a.name.Local == local {
			return a.value, true
		}
	}
	return "", false
}

func (n *node) attrNS(space, local string) (string, bool) {
	for _, a := range n.attrs {
		if a.name.Space == space && a.name.Local == local {
			return a.value, true
		}
	}
	return "", false
}

// display is the element name as the document wrote it.
func (n *node) display() string {
	if n.prefix != "" {
		return n.prefix + ":" + n.name.Local
	}
	return n.name.Local
}

func (n *node) path() string {
	var parts []string
	for x := n; x != nil; x = x.parent {
		parts = append(parts, x.display())
	}
	var b strings.Builder
	for i := len(parts) - 1; i >= 0; i-- {
		b.WriteByte('/')
		b.WriteString(parts[i])
	}
	return b.String()
}

// resolveQName reads a QName-typed value in the element's namespace context.
func (n *node) resolveQName(v string) (qname, error) {
	v = strings.TrimSpace(v)
	prefix, local := "", v
	if i := strings.IndexByte(v, ':'); i >= 0 {
		prefix, local = v[:i], v[i+1:]
	}
	if !isNCName(local) || prefix != "" && !isNCName(prefix) {
		return qname{}, fmt.Errorf("%q is not a QName", v)
	}
	if prefix == "xml" {
		return qname{nsXML, local}, nil
	}
	space, ok := n.ns.lookup(prefix)
	if !ok && prefix != "" {
		return qname{}, fmt.Errorf("prefix %q of %q is not declared", prefix, v)
	}
	return qname{space, local}, nil
}

// parseDoc reads a well-formed XML document into a tree, within the
// limits. Namespace declarations are resolved here (RawToken leaves them to
// us, which also keeps each element's bindings for QName values). XML
// white space before the XML declaration is accepted with a warning unless
// strict.
func parseDoc(b []byte, lim Limits, strict, entities bool) (*node, []Issue, error) {
	var warn []Issue
	lim = lim.withDefaults()
	if lim.MaxBytes > 0 && len(b) > lim.MaxBytes {
		return nil, nil, &limitError{msg: fmt.Sprintf("the document is larger than %d bytes (the limit)", lim.MaxBytes)}
	}
	// encoding/xml does not skip a UTF-8 byte order mark itself
	b = bytes.TrimPrefix(b, []byte("\uFEFF"))
	b = asXML10(b)
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = true
	d.CharsetReader = charsetReader
	top := &nsScope{m: map[string]string{"xml": nsXML}}
	var root, cur *node
	depth, elements := 0, 0
	for {
		line, _ := d.InputPos()
		off := d.InputOffset()
		tok, err := d.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		switch t := tok.(type) {
		case xml.Directive:
			if entities && root == nil && bytes.HasPrefix(t, []byte("DOCTYPE")) {
				if err := useEntities(d, t, b[d.InputOffset():], lim.entityBytes()); err != nil {
					if le, ok := err.(*limitError); ok {
						le.line = line
						return nil, nil, le
					}
					return nil, nil, fmt.Errorf("line %d: %w", line, err)
				}
			}
		case xml.ProcInst:
			if strings.EqualFold(t.Target, "xml") {
				switch {
				case t.Target == "xml" && off == 0:
				case t.Target == "xml" && !strict && root == nil && onlySpace(b[:off]):
					warn = append(warn, Issue{Line: line, Kind: IssueLeadingSpace,
						Msg: "white space before the XML declaration, which XML 1.0 wants first in the document (accepted)"})
				default:
					return nil, nil, fmt.Errorf("line %d: the XML declaration must come first in the document", line)
				}
				if !reXMLDeclVersion.Match(t.Inst) {
					return nil, nil, fmt.Errorf("line %d: the XML declaration has no version", line)
				}
			}
		case xml.StartElement:
			if cur == nil && root != nil {
				return nil, nil, fmt.Errorf("line %d: content after the root element", line)
			}
			depth++
			elements++
			if lim.MaxDepth > 0 && depth > lim.MaxDepth {
				return nil, nil, &limitError{line: line, msg: fmt.Sprintf("elements are nested deeper than %d (the limit)", lim.MaxDepth)}
			}
			if lim.MaxElements > 0 && elements > lim.MaxElements {
				return nil, nil, &limitError{line: line, msg: fmt.Sprintf("the document has more than %d elements (the limit)", lim.MaxElements)}
			}
			if lim.MaxAttrs > 0 && len(t.Attr) > lim.MaxAttrs {
				return nil, nil, &limitError{line: line, msg: fmt.Sprintf("an element has more than %d attributes (the limit)", lim.MaxAttrs)}
			}
			l := line
			n := &node{parent: cur, line: l}
			n.ns = top
			if cur != nil {
				n.ns = cur.ns
			}
			own := false
			for _, a := range t.Attr {
				decl := ""
				switch {
				case a.Name.Space == "" && a.Name.Local == "xmlns":
				case a.Name.Space == "xmlns":
					decl = a.Name.Local
				default:
					continue
				}
				if !own {
					n.ns, own = &nsScope{parent: n.ns, m: map[string]string{}}, true
				}
				if decl != "" && a.Value == "" {
					return nil, nil, fmt.Errorf("line %d: prefix %s bound to the empty namespace", l, decl)
				}
				n.ns.m[decl] = a.Value
			}
			space, ok := n.ns.lookup(t.Name.Space)
			if !ok && t.Name.Space != "" {
				return nil, nil, fmt.Errorf("line %d: prefix %q is not declared", l, t.Name.Space)
			}
			n.name, n.prefix = qname{space, t.Name.Local}, t.Name.Space
			seen := make(map[qname]bool, len(t.Attr))
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Space == "" && a.Name.Local == "xmlns" {
					continue
				}
				q := qname{Local: a.Name.Local}
				if a.Name.Space != "" {
					s, ok := n.ns.lookup(a.Name.Space)
					if !ok {
						return nil, nil, fmt.Errorf("line %d: prefix %q is not declared", l, a.Name.Space)
					}
					q.Space = s
				}
				if seen[q] {
					return nil, nil, fmt.Errorf("line %d: attribute %s repeated", l, q)
				}
				seen[q] = true
				n.attrs = append(n.attrs, attr{name: q, prefix: a.Name.Space, value: a.Value})
			}
			if cur == nil {
				root = n
			} else {
				cur.children = append(cur.children, n)
			}
			cur = n
		case xml.EndElement:
			if cur == nil || t.Name.Space != cur.prefix || t.Name.Local != cur.name.Local {
				return nil, nil, fmt.Errorf("line %d: unexpected end element %s", line, t.Name.Local)
			}
			cur = cur.parent
			depth--
		case xml.CharData:
			if cur != nil {
				cur.text.Write(t)
			} else if len(bytes.TrimSpace(t)) > 0 {
				return nil, nil, fmt.Errorf("line %d: text outside the root element", line)
			}
		}
	}
	if root == nil {
		return nil, nil, errors.New("no root element")
	}
	if cur != nil {
		return nil, nil, fmt.Errorf("element %s is not closed", cur.display())
	}
	return root, warn, nil
}

// asXML10 lets an XML 1.1 document be read by encoding/xml, which knows
// only XML 1.0: the version in the declaration is rewritten. What XML 1.1
// adds beyond that (NEL line ends, more name characters, C0 controls by
// reference) is read as XML 1.0 reads it.
func asXML10(b []byte) []byte {
	i := bytes.Index(b, []byte("?>"))
	if !bytes.HasPrefix(bytes.TrimLeft(b, " \t\r\n"), []byte("<?xml")) || i < 0 {
		return b
	}
	decl := b[:i]
	m := reXML11.FindIndex(decl)
	if m == nil {
		return b
	}
	out := append([]byte(nil), b...)
	out[m[1]-2] = '0' // "1.1" -> "1.0", the quote follows
	return out
}

var reXML11 = regexp.MustCompile(`version[ \t\r\n]*=[ \t\r\n]*["']1\.1["']`)

// onlySpace reports whether b holds XML white space only.
func onlySpace(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			return false
		}
	}
	return true
}

// reXMLDeclVersion: an XML declaration starts with its version (XML 1.0
// section 2.8); encoding/xml itself accepts one without.
var reXMLDeclVersion = regexp.MustCompile(`^[ \t\r\n]*version[ \t\r\n]*=[ \t\r\n]*("1\.[0-9]+"|'1\.[0-9]+')`)

// charsetReader decodes the non-UTF-8 encodings schema files are commonly
// declared in: ISO-8859-1 (whose bytes are the first 256 code points) and
// US-ASCII.
func charsetReader(label string, in io.Reader) (io.Reader, error) {
	switch strings.ToLower(label) {
	case "iso-8859-1", "iso8859-1", "latin1", "l1", "iso_8859-1":
		b, err := io.ReadAll(in)
		if err != nil {
			return nil, err
		}
		out := make([]rune, len(b))
		for i, c := range b {
			out[i] = rune(c)
		}
		return strings.NewReader(string(out)), nil
	case "us-ascii", "ascii", "utf8":
		return in, nil
	}
	return nil, fmt.Errorf("unsupported encoding %q", label)
}
