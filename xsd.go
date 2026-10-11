// Package xsd validates XML documents against W3C XML Schema 1.0 schemas
// that are read at run time, with the standard library only.
//
// Load reads a root schema document from a file system and follows its
// xs:include and xs:import elements by schemaLocation, relative to the
// document that names them, so a schema set spanning several target
// namespaces loads from one root. LoadZip does the same inside a zip
// archive. Validate then checks an instance document: its root must be a
// global element of the set.
//
// Supported: global and local element declarations (ref, form, abstract,
// nillable, fixed, default, substitutionGroup with transitive membership);
// complex types with sequence, choice, all and named model groups under
// minOccurs/maxOccurs, element-only, mixed and empty content, simple
// content, complexContent and simpleContent derivation by extension and
// restriction (attribute prohibition included); xs:any and
// xs:anyAttribute with ##any, ##other, ##local, ##targetNamespace and
// namespace lists under strict, lax and skip processing; attributes with
// use, fixed and default, global attributes and attribute groups; simple
// types by restriction (enumeration, pattern, length, minLength,
// maxLength, min/max Inclusive/Exclusive, totalDigits, fractionDigits,
// whiteSpace), list and union; xsi:type (checked to derive from the
// declared type), xsi:nil; all XSD 1.0 built-in datatypes; ID uniqueness
// and IDREF resolution. The xml: attributes (lang, space, base, id) are
// predefined when no loaded schema declares them.
//
// Identity constraints (unique, key, keyref) are evaluated with the XPath
// subset XSD 1.0 defines for them (idc.go).
//
// ValidateTree validates a document the caller has already parsed (an
// Element tree) and takes Options: hooks through which a vocabulary with
// its own extension rules (IEC 61850-6 clause 8.2.1, for one) decides
// what happens to elements and attributes the schema does not declare, and
// adds its own value checks. Issues carry a Kind so the caller can grade
// and word them.
//
// A document included without a target namespace takes the including
// document's (chameleon include). A content model in which an element
// could match two particles is refused (Unique Particle Attribution,
// upa.go).
//
// Documents are parsed within Limits: by default, elements nested deeper
// than DefaultMaxDepth are refused before validation (limits.go). The XML
// declaration, when present, must carry a version and come first; XML
// white space before it is accepted with a warning unless
// DocOptions.StrictProlog is set.
//
// Refused at load time with an error naming the construct (so nothing is
// silently accepted): xs:redefine, identity constraint paths outside the
// XPath subset, XSD 1.1 constructs, pattern features RE2 cannot express
// (Unicode block escapes, class subtraction) and range facets on types
// other than numbers, dates and times. Other schema-component constraints
// that only concern the schema's own correctness (for instance that a
// restriction really restricts its base) are not checked: the schema is
// trusted to be correct.
package xsd

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Issue is one validation failure.
type Issue struct {
	Line int
	Path string
	Msg  string
	Kind IssueKind
	// Elem is the element concerned when the document was given to
	// ValidateTree (nil for Validate).
	Elem *Element
	// Attr, Value and Why detail a value outside its simple type
	// (IssueValue): the attribute as written ("" for element content), the
	// value as written and why it is not valid.
	Attr, Value, Why string
}

// String names the value.
func (i Issue) String() string {
	var b strings.Builder
	if i.Line > 0 {
		b.WriteString("line ")
		b.WriteString(strconv.Itoa(i.Line))
		b.WriteString(": ")
	}
	if i.Path != "" {
		b.WriteString(i.Path)
		b.WriteString(": ")
	}
	b.WriteString(i.Msg)
	return b.String()
}

// Errors is the list of failures of one validation.
type Errors []Issue

// maxIssues bounds how many issues one validation collects; errorShown is
// how many of them Error prints.
const (
	maxIssues  = 200
	errorShown = 10
)

// Error describes the failure.
func (e *Errors) Error() string {
	if e == nil || len(*e) == 0 {
		return "no schema errors"
	}
	parts := make([]string, 0, errorShown+1)
	for i, is := range *e {
		if i == errorShown {
			parts = append(parts, fmt.Sprintf("and %d more", len(*e)-errorShown))
			break
		}
		parts = append(parts, is.String())
	}
	return "schema validation: " + strings.Join(parts, "; ")
}

// Schema is a loaded, resolved schema set. It is safe for concurrent use.
type Schema struct {
	elems      map[qname]*elemDecl
	types      map[qname]*xtype
	attrs      map[qname]*attrDecl
	groups     map[qname]*groupDef
	attrGroups map[qname]*attrGroupDef
	anyType    *xtype
	warnings   Errors
}

// LoadWarnings are what loading the schema accepted but noted, each with
// the schema document as Path (for instance IssueLeadingSpace).
func (s *Schema) LoadWarnings() Errors { return s.warnings }

// ---- components -------------------------------------------------------------

const (
	stNew = iota
	stBuilding
	stDone
)

// xtype is a simple or complex type definition.
type xtype struct {
	name       qname // zero for an anonymous type
	complex    bool
	base       *xtype
	derivation string
	state      int
	raw        *node
	doc        *schemaDoc

	// simple types
	variety int
	builtin string // nearest built-in ancestor (atomic variety)
	ws      int
	facets  *facetSet
	item    *xtype
	members []*xtype

	// complex types
	abstract      bool
	mixed         bool
	attrs         []*attrUse
	anyAttr       []*wildcard // any of them admits an attribute (union after extension)
	content       *particle   // nil: empty content
	simpleContent *xtype
	leaves        []*particle
}

func (t *xtype) isBuiltin() bool { return t.name.Space == nsXS }

func (t *xtype) label() string {
	if t.name.Local == "" {
		return "an anonymous type"
	}
	return t.name.Local
}

type elemDecl struct {
	name      qname
	typ       *xtype
	abstract  bool
	nillable  bool
	fixed     *string
	def       *string // default value
	head      *elemDecl
	global    bool
	members   map[qname]*elemDecl // substitution group, transitive (global heads)
	memberSeq []*elemDecl
	idcs      []*idc
	state     int
	raw       *node
	doc       *schemaDoc
}

// supplied is the value an empty element takes (its fixed or default
// value), if any.
func (e *elemDecl) supplied() (string, bool) {
	switch {
	case e == nil:
	case e.fixed != nil:
		return *e.fixed, true
	case e.def != nil:
		return *e.def, true
	}
	return "", false
}

type attrDecl struct {
	name  qname
	typ   *xtype
	fixed *string
	def   *string // default value
	state int
	raw   *node
	doc   *schemaDoc
}

type attrUse struct {
	decl       *attrDecl
	required   bool
	prohibited bool
	fixed      *string
	def        *string // default value
}

// supplied is the value an absent attribute takes (its fixed or default
// value), if any.
func (u *attrUse) supplied() (string, bool) {
	for _, v := range []*string{u.fixed, u.decl.fixed, u.def, u.decl.def} {
		if v != nil {
			return *v, true
		}
	}
	return "", false
}

type wildcard struct {
	any     bool
	not     []string // ##other: neither these namespaces nor the absent one
	list    map[string]bool
	process string // strict, lax, skip
}

func (w *wildcard) allows(ns string) bool {
	switch {
	case w.any:
		return true
	case w.not != nil:
		if ns == "" {
			return false
		}
		for _, x := range w.not {
			if x == ns {
				return false
			}
		}
		return true
	}
	return w.list[ns]
}

func (w *wildcard) describe() string {
	switch {
	case w.any:
		return "any element"
	case w.not != nil:
		return "an element of a namespace other than " + strconv.Quote(w.not[0])
	}
	ns := make([]string, 0, len(w.list))
	for k := range w.list {
		if k == "" {
			k = "(no namespace)"
		}
		ns = append(ns, k)
	}
	sort.Strings(ns)
	return "an element of namespace " + strings.Join(ns, " or ")
}

// sample is a namespace the wildcard admits (for "expected" messages).
func (w *wildcard) sample() (string, bool) {
	switch {
	case w.any || w.not != nil:
		return "urn:x-sigma-xsd:any", true
	}
	for k := range w.list {
		return k, true
	}
	return "", false
}

const (
	pElem = iota + 1
	pSeq
	pChoice
	pAll
	pAny
)

type particle struct {
	kind     int
	min, max int // max -1: unbounded
	items    []*particle
	elem     *elemDecl
	wc       *wildcard
}

type groupDef struct {
	model *particle
	state int
	raw   *node
	doc   *schemaDoc
}

type attrGroupDef struct {
	uses  []*attrUse
	wc    *wildcard
	state int
	raw   *node
	doc   *schemaDoc
}

// schemaDoc is one schema document of the set.
type schemaDoc struct {
	path          string
	tns           string
	chameleon     bool // included without a target namespace: tns is the includer's
	elemQualified bool
	attrQualified bool
	root          *node
}

func (d *schemaDoc) errf(n *node, format string, args ...any) error {
	return fmt.Errorf("%s line %d: %s", d.path, n.line, fmt.Sprintf(format, args...))
}

// ---- loading ------------------------------------------------------------------

type loader struct {
	complex []*xtype // complex types built, for the Unique Particle Attribution check
	s       *Schema
	fsys    fs.FS
	docs    map[string]*schemaDoc
	remote  map[string]string // namespace -> absolute location not in the file system
	roots   map[string]bool   // root documents, which may declare any namespace
	ordered []*schemaDoc
	idcs    []idcAt
}

// Load reads the schema whose root document is root inside fsys, with
// every document it includes or imports.
func Load(fsys fs.FS, root string) (*Schema, error) {
	return LoadFiles(fsys, root)
}

// LoadFiles reads one schema from several root documents inside fsys (for
// instance one per target namespace, none importing the others), with
// every document each includes or imports. A document named twice, or
// reached from two roots, is read once.
func LoadFiles(fsys fs.FS, roots ...string) (*Schema, error) {
	if len(roots) == 0 {
		return nil, errors.New("no schema document given")
	}
	l := &loader{
		s: &Schema{
			elems: map[qname]*elemDecl{}, types: map[qname]*xtype{}, attrs: map[qname]*attrDecl{},
			groups: map[qname]*groupDef{}, attrGroups: map[qname]*attrGroupDef{},
		},
		fsys: fsys, docs: map[string]*schemaDoc{}, remote: map[string]string{}, roots: map[string]bool{},
	}
	l.builtins()
	for _, root := range roots {
		l.roots[path.Clean(root)] = true
	}
	for _, root := range roots {
		if _, err := l.load(path.Clean(root), "", false); err != nil {
			return nil, err
		}
	}
	for ns, loc := range l.remote {
		if !l.hasNamespace(ns) {
			return nil, fmt.Errorf("namespace %q is imported from %s, which is not in the supplied schema files", ns, loc)
		}
	}
	if err := l.build(); err != nil {
		return nil, err
	}
	return l.s, nil
}

// LoadZip reads a schema from a zip archive; root is matched by base name
// anywhere in the archive (macOS resource entries are ignored).
func LoadZip(b []byte, root string) (*Schema, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("schema archive: %w", err)
	}
	var found []string
	for _, f := range zr.File {
		if skipZipEntry(f.Name) {
			continue
		}
		if path.Base(f.Name) == path.Base(root) && !strings.HasSuffix(f.Name, "/") {
			found = append(found, f.Name)
		}
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("schema archive has no %s", path.Base(root))
	case 1:
	default:
		return nil, fmt.Errorf("schema archive has %d files named %s: %s", len(found), path.Base(root), strings.Join(found, ", "))
	}
	return Load(zr, found[0])
}

// skipZipEntry reports macOS resource entries, which are not schema files.
func skipZipEntry(name string) bool {
	return strings.HasPrefix(name, "__MACOSX/") || strings.Contains(name, "/__MACOSX/") || strings.HasPrefix(path.Base(name), "._")
}

func (l *loader) hasNamespace(ns string) bool {
	for _, d := range l.ordered {
		if d.tns == ns {
			return true
		}
	}
	return false
}

func (l *loader) load(p, wantNS string, include bool) (*schemaDoc, error) {
	// A document without a target namespace included into one is loaded
	// once for each namespace it is included into (chameleon include).
	chameleonKey := p + "\x00" + wantNS
	if include && wantNS != "" {
		if d, ok := l.docs[chameleonKey]; ok {
			return d, nil
		}
	}
	if d, ok := l.docs[p]; ok && !(include && wantNS != "" && d.tns == "") {
		return d, nil
	}
	b, err := fs.ReadFile(l.fsys, p)
	if err != nil {
		return nil, fmt.Errorf("schema document %s: %w", p, err)
	}
	root, warn, err := parseDoc(b, Limits{}, false)
	if err != nil {
		return nil, fmt.Errorf("schema document %s: %w", p, err)
	}
	for _, w := range warn {
		w.Path = p
		l.s.warnings = append(l.s.warnings, w)
	}
	if root.name != (qname{nsXS, "schema"}) {
		return nil, fmt.Errorf("schema document %s: root element is %s, not xs:schema", p, root.name)
	}
	d := &schemaDoc{path: p, root: root}
	d.tns, _ = root.attr("targetNamespace")
	v, _ := root.attr("elementFormDefault")
	d.elemQualified = v == "qualified"
	v, _ = root.attr("attributeFormDefault")
	d.attrQualified = v == "qualified"
	key := p
	if include && d.tns != wantNS {
		if d.tns != "" {
			return nil, fmt.Errorf("schema document %s: included into namespace %q but declares %q", p, wantNS, d.tns)
		}
		// chameleon include (XSD 1.0 Part 1, 4.2.1): the document's
		// components take the including document's namespace
		d.tns, d.chameleon, key = wantNS, true, chameleonKey
	}
	if !include && d.tns != wantNS && p != "" && len(l.docs) > 0 && !(wantNS == "" && l.roots[p]) {
		return nil, fmt.Errorf("schema document %s: imported for namespace %q but declares %q", p, wantNS, d.tns)
	}
	l.docs[key] = d
	l.ordered = append(l.ordered, d)
	for _, c := range root.children {
		if c.name.Space != nsXS {
			return nil, d.errf(c, "unexpected element %s in xs:schema", c.name)
		}
		switch c.name.Local {
		case "annotation":
		case "include", "import":
			loc, hasLoc := c.attr("schemaLocation")
			ns, _ := c.attr("namespace")
			if c.name.Local == "include" {
				ns = d.tns
			} else if ns == d.tns {
				return nil, d.errf(c, "xs:import of the document's own target namespace")
			}
			if !hasLoc {
				continue // resolved from documents loaded otherwise, or by references failing
			}
			if strings.Contains(loc, "://") || strings.HasPrefix(loc, "/") {
				if c.name.Local == "include" {
					return nil, d.errf(c, "xs:include of %s: only relative locations inside the schema files are supported", loc)
				}
				l.remote[ns] = loc
				continue
			}
			target := path.Join(path.Dir(p), loc)
			if strings.HasPrefix(target, "../") || target == ".." {
				return nil, d.errf(c, "schemaLocation %s points outside the schema files", loc)
			}
			if _, err := l.load(target, ns, c.name.Local == "include"); err != nil {
				return nil, err
			}
		case "redefine":
			return nil, d.errf(c, "xs:redefine is not supported")
		case "element", "complexType", "simpleType", "attribute", "group", "attributeGroup":
			if err := l.register(d, c); err != nil {
				return nil, err
			}
		case "notation":
			// declarations only; NOTATION-typed values are checked lexically
		default:
			return nil, d.errf(c, "xs:%s is not supported", c.name.Local)
		}
	}
	return d, nil
}

func (l *loader) register(d *schemaDoc, c *node) error {
	name, ok := c.attr("name")
	if !ok || !isNCName(name) {
		return d.errf(c, "global xs:%s needs a name", c.name.Local)
	}
	q := qname{d.tns, name}
	dup := func(exists bool) error {
		if exists {
			return d.errf(c, "xs:%s %s is declared twice", c.name.Local, q)
		}
		return nil
	}
	switch c.name.Local {
	case "element":
		if err := dup(l.s.elems[q] != nil); err != nil {
			return err
		}
		l.s.elems[q] = &elemDecl{name: q, global: true, raw: c, doc: d}
	case "complexType", "simpleType":
		if err := dup(l.s.types[q] != nil); err != nil {
			return err
		}
		l.s.types[q] = &xtype{name: q, complex: c.name.Local == "complexType", raw: c, doc: d}
	case "attribute":
		if err := dup(l.s.attrs[q] != nil); err != nil {
			return err
		}
		l.s.attrs[q] = &attrDecl{name: q, raw: c, doc: d}
	case "group":
		if err := dup(l.s.groups[q] != nil); err != nil {
			return err
		}
		l.s.groups[q] = &groupDef{raw: c, doc: d}
	case "attributeGroup":
		if err := dup(l.s.attrGroups[q] != nil); err != nil {
			return err
		}
		l.s.attrGroups[q] = &attrGroupDef{raw: c, doc: d}
	}
	return nil
}

// builtins defines the XSD built-in types and the xml: attributes.
func (l *loader) builtins() {
	s := l.s
	any := &xtype{name: qname{nsXS, "anyType"}, complex: true, mixed: true, state: stDone}
	wc := &wildcard{any: true, process: "lax"}
	any.content = &particle{kind: pSeq, min: 1, max: 1, items: []*particle{{kind: pAny, min: 0, max: -1, wc: wc}}}
	any.anyAttr = []*wildcard{wc}
	any.leaves = collectLeaves(any.content)
	s.types[any.name] = any
	s.anyType = any
	var def func(name string) *xtype
	def = func(name string) *xtype {
		q := qname{nsXS, name}
		if t := s.types[q]; t != nil {
			return t
		}
		t := &xtype{name: q, state: stDone, builtin: name, ws: builtinWS(name)}
		s.types[q] = t
		if b := builtinBase[name]; b == "anyType" {
			t.base = any
		} else {
			t.base = def(b)
		}
		if item, ok := builtinLists[name]; ok {
			t.variety, t.item, t.builtin, t.ws = varList, def(item), "", wsCollapse
			t.facets = newFacetSet()
			t.facets.minLength = 1
		}
		return t
	}
	for name := range builtinBase {
		def(name)
	}
}

func (l *loader) xmlAttributes() {
	add := func(local, typ string, enum ...string) {
		q := qname{nsXML, local}
		if l.s.attrs[q] != nil {
			return
		}
		t := l.s.types[qname{nsXS, typ}]
		if len(enum) > 0 {
			f := newFacetSet()
			f.enum = enum
			t = &xtype{base: t, builtin: t.builtin, ws: t.ws, facets: f, state: stDone}
		}
		l.s.attrs[q] = &attrDecl{name: q, typ: t, state: stDone}
	}
	// xml:lang also admits the empty string (to undo a language).
	lang := &xtype{variety: varUnion, base: l.s.types[qname{nsXS, "anySimpleType"}], state: stDone, ws: wsCollapse}
	empty := newFacetSet()
	empty.enum = []string{""}
	lang.members = []*xtype{l.s.types[qname{nsXS, "language"}], {base: l.s.types[qname{nsXS, "string"}], builtin: "string", facets: empty, state: stDone}}
	if l.s.attrs[qname{nsXML, "lang"}] == nil {
		l.s.attrs[qname{nsXML, "lang"}] = &attrDecl{name: qname{nsXML, "lang"}, typ: lang, state: stDone}
	}
	add("space", "NCName", "default", "preserve")
	add("base", "anyURI")
	add("id", "ID")
}

// ---- building -----------------------------------------------------------------

func (l *loader) build() error {
	l.xmlAttributes()
	for _, t := range sortedTypes(l.s.types) {
		if err := l.buildType(t); err != nil {
			return err
		}
	}
	for _, a := range l.s.attrs {
		if err := l.buildAttr(a); err != nil {
			return err
		}
	}
	for _, g := range l.s.groups {
		if _, err := l.buildGroup(g); err != nil {
			return err
		}
	}
	for _, g := range l.s.attrGroups {
		if err := l.buildAttrGroup(g); err != nil {
			return err
		}
	}
	elems := make([]*elemDecl, 0, len(l.s.elems))
	for _, e := range l.s.elems {
		elems = append(elems, e)
	}
	sort.Slice(elems, func(i, j int) bool { return elems[i].name.String() < elems[j].name.String() })
	for _, e := range elems {
		if err := l.buildElem(e); err != nil {
			return err
		}
	}
	// substitution groups, transitively
	for _, e := range elems {
		seen := map[*elemDecl]bool{e: true}
		for h := e.head; h != nil; h = h.head {
			if seen[h] {
				return e.doc.errf(e.raw, "substitution group of %s is circular", e.name)
			}
			seen[h] = true
			if h.members == nil {
				h.members = map[qname]*elemDecl{}
			}
			h.members[e.name] = e
			h.memberSeq = append(h.memberSeq, e)
		}
	}
	if err := l.resolveIDCs(); err != nil {
		return err
	}
	for _, t := range l.complex {
		if err := checkUPA(t); err != nil {
			return err
		}
	}
	return nil
}

func sortedTypes(m map[qname]*xtype) []*xtype {
	out := make([]*xtype, 0, len(m))
	for _, t := range m {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name.String() < out[j].name.String() })
	return out
}

// children skips annotations and reports foreign elements.
func xsChildren(d *schemaDoc, n *node) ([]*node, error) {
	var out []*node
	for _, c := range n.children {
		if c.name.Space != nsXS {
			return nil, d.errf(c, "unexpected element %s in xs:%s", c.name, n.name.Local)
		}
		if c.name.Local == "annotation" {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func (l *loader) ref(d *schemaDoc, n *node, attrName string) (qname, bool, error) {
	v, ok := n.attr(attrName)
	if !ok {
		return qname{}, false, nil
	}
	q, err := n.resolveQName(v)
	if err != nil {
		return qname{}, true, d.errf(n, "%s: %v", attrName, err)
	}
	return d.coerce(q), true, nil
}

// coerce moves a reference to a component in no namespace into the
// namespace a chameleon document was included into.
func (d *schemaDoc) coerce(q qname) qname {
	if d.chameleon && q.Space == "" {
		q.Space = d.tns
	}
	return q
}

func (l *loader) typeRef(d *schemaDoc, n *node, attrName string) (*xtype, bool, error) {
	q, ok, err := l.ref(d, n, attrName)
	if !ok || err != nil {
		return nil, ok, err
	}
	t := l.s.types[q]
	if t == nil {
		return nil, true, d.errf(n, "type %s is not defined", q)
	}
	return t, true, nil
}

func (l *loader) buildType(t *xtype) error {
	switch t.state {
	case stDone:
		return nil
	case stBuilding:
		return t.doc.errf(t.raw, "type %s is derived from itself", t.name)
	}
	t.state = stBuilding
	var err error
	if t.complex {
		err = l.buildComplex(t)
	} else {
		err = l.buildSimple(t)
	}
	if err != nil {
		return err
	}
	t.state = stDone
	return nil
}

// anonymous builds an inline type definition.
func (l *loader) anonymous(d *schemaDoc, n *node) (*xtype, error) {
	t := &xtype{complex: n.name.Local == "complexType", raw: n, doc: d}
	if err := l.buildType(t); err != nil {
		return nil, err
	}
	return t, nil
}

func (l *loader) buildSimple(t *xtype) error {
	d, n := t.doc, t.raw
	kids, err := xsChildren(d, n)
	if err != nil {
		return err
	}
	if len(kids) != 1 {
		return d.errf(n, "simple type %s needs exactly one of restriction, list or union", t.label())
	}
	c := kids[0]
	anySimple := l.s.types[qname{nsXS, "anySimpleType"}]
	switch c.name.Local {
	case "restriction":
		base, err := l.simpleBase(d, c)
		if err != nil {
			return err
		}
		t.base, t.derivation = base, "restriction"
		t.variety, t.item, t.members, t.builtin, t.ws = base.variety, base.item, base.members, base.builtin, base.ws
		rest, err := xsChildren(d, c)
		if err != nil {
			return err
		}
		if len(rest) > 0 && rest[0].name.Local == "simpleType" {
			rest = rest[1:]
		}
		return l.facets(t, rest)
	case "list":
		t.variety, t.base, t.derivation, t.ws = varList, anySimple, "list", wsCollapse
		it, ok, err := l.typeRef(d, c, "itemType")
		if err != nil {
			return err
		}
		inner, err := xsChildren(d, c)
		if err != nil {
			return err
		}
		if !ok {
			if len(inner) != 1 || inner[0].name.Local != "simpleType" {
				return d.errf(c, "xs:list needs itemType or one inline simpleType")
			}
			if it, err = l.anonymous(d, inner[0]); err != nil {
				return err
			}
		}
		if it.complex {
			return d.errf(c, "list item type %s is complex", it.label())
		}
		t.item = it
	case "union":
		t.variety, t.base, t.derivation, t.ws = varUnion, anySimple, "union", wsCollapse
		if v, ok := c.attr("memberTypes"); ok {
			for _, f := range strings.Fields(v) {
				q, err := c.resolveQName(f)
				if err != nil {
					return d.errf(c, "memberTypes: %v", err)
				}
				q = d.coerce(q)
				m := l.s.types[q]
				if m == nil {
					return d.errf(c, "member type %s is not defined", q)
				}
				if m.complex {
					return d.errf(c, "member type %s is complex", q)
				}
				t.members = append(t.members, m)
			}
		}
		inner, err := xsChildren(d, c)
		if err != nil {
			return err
		}
		for _, x := range inner {
			if x.name.Local != "simpleType" {
				return d.errf(x, "unexpected xs:%s in xs:union", x.name.Local)
			}
			m, err := l.anonymous(d, x)
			if err != nil {
				return err
			}
			t.members = append(t.members, m)
		}
		if len(t.members) == 0 {
			return d.errf(c, "xs:union without member types")
		}
	default:
		return d.errf(c, "xs:%s is not allowed in a simple type", c.name.Local)
	}
	return nil
}

// simpleBase resolves the base of a simple restriction (attribute or
// inline type) and builds it.
func (l *loader) simpleBase(d *schemaDoc, c *node) (*xtype, error) {
	base, ok, err := l.typeRef(d, c, "base")
	if err != nil {
		return nil, err
	}
	if !ok {
		kids, err := xsChildren(d, c)
		if err != nil {
			return nil, err
		}
		if len(kids) == 0 || kids[0].name.Local != "simpleType" {
			return nil, d.errf(c, "xs:restriction needs a base or an inline simpleType")
		}
		if base, err = l.anonymous(d, kids[0]); err != nil {
			return nil, err
		}
	}
	if err := l.buildType(base); err != nil {
		return nil, err
	}
	if base.complex {
		return nil, d.errf(c, "simple type restricts complex type %s", base.label())
	}
	if base == l.s.types[qname{nsXS, "anySimpleType"}] {
		return nil, d.errf(c, "restriction of anySimpleType is not allowed")
	}
	return base, nil
}

func (l *loader) facets(t *xtype, kids []*node) error {
	d := t.doc
	f := newFacetSet()
	prim := ""
	if t.variety == varAtomic {
		prim = primitive(t.builtin)
	}
	intVal := func(c *node, v string) (int, error) {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < 0 {
			return 0, d.errf(c, "facet %s needs a non-negative integer, not %q", c.name.Local, v)
		}
		return n, nil
	}
	any := false
	for _, c := range kids {
		switch c.name.Local {
		case "enumeration", "pattern", "length", "minLength", "maxLength", "totalDigits", "fractionDigits",
			"whiteSpace", "minInclusive", "maxInclusive", "minExclusive", "maxExclusive":
		default:
			return d.errf(c, "facet xs:%s is not supported", c.name.Local)
		}
		v, ok := c.attr("value")
		if !ok {
			return d.errf(c, "facet xs:%s without a value", c.name.Local)
		}
		any = true
		var err error
		switch c.name.Local {
		case "enumeration":
			f.enum = append(f.enum, applyWS(t.ws, v))
		case "pattern":
			re, e := translateRegex(v)
			if e != nil {
				return d.errf(c, "%v", e)
			}
			f.patterns = append(f.patterns, re)
			f.patternSrc = append(f.patternSrc, v)
		case "length":
			f.length, err = intVal(c, v)
		case "minLength":
			f.minLength, err = intVal(c, v)
		case "maxLength":
			f.maxLength, err = intVal(c, v)
		case "totalDigits":
			f.totalDigits, err = intVal(c, v)
		case "fractionDigits":
			f.fractionDigits, err = intVal(c, v)
		case "whiteSpace":
			switch v {
			case "preserve":
				t.ws = wsPreserve
			case "replace":
				t.ws = wsReplace
			case "collapse":
				t.ws = wsCollapse
			default:
				return d.errf(c, "whiteSpace %q", v)
			}
		case "minInclusive", "maxInclusive", "minExclusive", "maxExclusive":
			if !isNumeric(prim) && !isOrderedTime(prim) {
				return d.errf(c, "range facet xs:%s on a %s value is not supported", c.name.Local, orWord(prim, t.label()))
			}
			v = collapse(v)
			if why := builtinCheck(t.builtin, v, c); why != "" {
				return d.errf(c, "facet xs:%s value %q %s", c.name.Local, v, why)
			}
			switch c.name.Local {
			case "minInclusive":
				f.minIncl = v
			case "maxInclusive":
				f.maxIncl = v
			case "minExclusive":
				f.minExcl = v
			case "maxExclusive":
				f.maxExcl = v
			}
		default:
			return d.errf(c, "facet xs:%s is not supported", c.name.Local)
		}
		if err != nil {
			return err
		}
	}
	if any {
		t.facets = f
	}
	return nil
}

func orWord(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func occurs(d *schemaDoc, n *node) (int, int, error) {
	min, max := 1, 1
	if v, ok := n.attr("minOccurs"); ok {
		x, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || x < 0 {
			return 0, 0, d.errf(n, "minOccurs %q", v)
		}
		min = x
	}
	if v, ok := n.attr("maxOccurs"); ok {
		v = strings.TrimSpace(v)
		if v == "unbounded" {
			max = -1
		} else {
			x, err := strconv.Atoi(v)
			if err != nil || x < 0 {
				return 0, 0, d.errf(n, "maxOccurs %q", v)
			}
			max = x
		}
	}
	if max >= 0 && min > max {
		return 0, 0, d.errf(n, "minOccurs %d exceeds maxOccurs %d", min, max)
	}
	return min, max, nil
}

func (l *loader) buildComplex(t *xtype) error {
	d, n := t.doc, t.raw
	t.base, t.derivation = l.s.anyType, "restriction"
	if v, ok := n.attr("abstract"); ok {
		t.abstract = v == "true" || v == "1"
	}
	if v, ok := n.attr("mixed"); ok {
		t.mixed = v == "true" || v == "1"
	}
	kids, err := xsChildren(d, n)
	if err != nil {
		return err
	}
	if len(kids) > 0 && (kids[0].name.Local == "simpleContent" || kids[0].name.Local == "complexContent") {
		if len(kids) != 1 {
			return d.errf(kids[1], "unexpected xs:%s after xs:%s", kids[1].name.Local, kids[0].name.Local)
		}
		if kids[0].name.Local == "simpleContent" {
			err = l.simpleContent(t, kids[0])
		} else {
			err = l.complexContent(t, kids[0])
		}
		if err != nil {
			return err
		}
	} else {
		model, rest, err := l.modelAndRest(d, kids)
		if err != nil {
			return err
		}
		t.content = model
		if t.attrs, t.anyAttr, err = l.attributeDecls(d, rest); err != nil {
			return err
		}
	}
	t.leaves = collectLeaves(t.content)
	l.complex = append(l.complex, t)
	return nil
}

// modelAndRest splits a content model particle (if any) from the
// attribute declarations after it.
func (l *loader) modelAndRest(d *schemaDoc, kids []*node) (*particle, []*node, error) {
	if len(kids) == 0 {
		return nil, nil, nil
	}
	switch kids[0].name.Local {
	case "sequence", "choice", "all", "group":
		p, err := l.particle(d, kids[0])
		if err != nil {
			return nil, nil, err
		}
		return p, kids[1:], nil
	}
	return nil, kids, nil
}

func (l *loader) complexContent(t *xtype, cc *node) error {
	d := t.doc
	if v, ok := cc.attr("mixed"); ok {
		t.mixed = v == "true" || v == "1"
	}
	kids, err := xsChildren(d, cc)
	if err != nil {
		return err
	}
	if len(kids) != 1 || kids[0].name.Local != "extension" && kids[0].name.Local != "restriction" {
		return d.errf(cc, "xs:complexContent needs one extension or restriction")
	}
	c := kids[0]
	base, ok, err := l.typeRef(d, c, "base")
	if err != nil {
		return err
	}
	if !ok {
		return d.errf(c, "xs:%s needs a base", c.name.Local)
	}
	if err := l.buildType(base); err != nil {
		return err
	}
	if !base.complex {
		return d.errf(c, "complex content derived from simple type %s", base.label())
	}
	if base.simpleContent != nil {
		return d.errf(c, "complex content derived from %s, which has simple content", base.label())
	}
	inner, err := xsChildren(d, c)
	if err != nil {
		return err
	}
	model, rest, err := l.modelAndRest(d, inner)
	if err != nil {
		return err
	}
	own, wc, err := l.attributeDecls(d, rest)
	if err != nil {
		return err
	}
	t.base, t.derivation = base, c.name.Local
	if c.name.Local == "extension" {
		switch {
		case base.content == nil:
			t.content = model
		case model == nil:
			t.content = base.content
		default:
			t.content = &particle{kind: pSeq, min: 1, max: 1, items: []*particle{base.content, model}}
		}
		t.attrs = mergeAttrs(base.attrs, own, false)
		t.anyAttr = append(append([]*wildcard(nil), base.anyAttr...), wc...)
		return nil
	}
	t.content = model
	t.attrs = mergeAttrs(base.attrs, own, true)
	t.anyAttr = wc
	return nil
}

func (l *loader) simpleContent(t *xtype, sc *node) error {
	d := t.doc
	kids, err := xsChildren(d, sc)
	if err != nil {
		return err
	}
	if len(kids) != 1 || kids[0].name.Local != "extension" && kids[0].name.Local != "restriction" {
		return d.errf(sc, "xs:simpleContent needs one extension or restriction")
	}
	c := kids[0]
	base, ok, err := l.typeRef(d, c, "base")
	if err != nil {
		return err
	}
	if !ok {
		return d.errf(c, "xs:%s needs a base", c.name.Local)
	}
	if err := l.buildType(base); err != nil {
		return err
	}
	inner, err := xsChildren(d, c)
	if err != nil {
		return err
	}
	t.base, t.derivation = base, c.name.Local
	if c.name.Local == "extension" {
		own, wc, err := l.attributeDecls(d, inner)
		if err != nil {
			return err
		}
		switch {
		case !base.complex:
			t.simpleContent, t.attrs, t.anyAttr = base, own, wc
		case base.simpleContent != nil:
			t.simpleContent = base.simpleContent
			t.attrs = mergeAttrs(base.attrs, own, false)
			t.anyAttr = append(append([]*wildcard(nil), base.anyAttr...), wc...)
		default:
			return d.errf(c, "simple content extends %s, which has no simple content", base.label())
		}
		return nil
	}
	if !base.complex || base.simpleContent == nil {
		return d.errf(c, "simple content restriction of %s, which has no simple content", base.label())
	}
	st := &xtype{raw: c, doc: d, state: stDone, base: base.simpleContent, derivation: "restriction"}
	if len(inner) > 0 && inner[0].name.Local == "simpleType" {
		if st.base, err = l.anonymous(d, inner[0]); err != nil {
			return err
		}
		inner = inner[1:]
	}
	b := st.base
	st.variety, st.item, st.members, st.builtin, st.ws = b.variety, b.item, b.members, b.builtin, b.ws
	var facetNodes, attrNodes []*node
	for _, x := range inner {
		switch x.name.Local {
		case "attribute", "attributeGroup", "anyAttribute":
			attrNodes = append(attrNodes, x)
		default:
			facetNodes = append(facetNodes, x)
		}
	}
	if err := l.facets(st, facetNodes); err != nil {
		return err
	}
	own, wc, err := l.attributeDecls(d, attrNodes)
	if err != nil {
		return err
	}
	t.simpleContent = st
	t.attrs = mergeAttrs(base.attrs, own, true)
	t.anyAttr = wc
	return nil
}

// mergeAttrs combines inherited attribute uses with a derivation's own; a
// prohibited use (meaningful under restriction) removes the inherited one.
func mergeAttrs(base, own []*attrUse, _ bool) []*attrUse {
	out := make([]*attrUse, 0, len(base)+len(own))
	idx := map[qname]int{}
	for _, u := range base {
		idx[u.decl.name] = len(out)
		out = append(out, u)
	}
	for _, u := range own {
		if i, ok := idx[u.decl.name]; ok {
			out[i] = u
			continue
		}
		idx[u.decl.name] = len(out)
		out = append(out, u)
	}
	res := out[:0]
	for _, u := range out {
		if u.prohibited {
			continue
		}
		res = append(res, u)
	}
	return res
}

func (l *loader) particle(d *schemaDoc, n *node) (*particle, error) {
	min, max, err := occurs(d, n)
	if err != nil {
		return nil, err
	}
	switch n.name.Local {
	case "element":
		e, err := l.localElem(d, n)
		if err != nil {
			return nil, err
		}
		return &particle{kind: pElem, min: min, max: max, elem: e}, nil
	case "any":
		wc, err := wildcardOf(d, n)
		if err != nil {
			return nil, err
		}
		return &particle{kind: pAny, min: min, max: max, wc: wc}, nil
	case "group":
		q, ok, err := l.ref(d, n, "ref")
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, d.errf(n, "local xs:group needs ref")
		}
		g := l.s.groups[q]
		if g == nil {
			return nil, d.errf(n, "group %s is not defined", q)
		}
		model, err := l.buildGroup(g)
		if err != nil {
			return nil, err
		}
		return &particle{kind: pSeq, min: min, max: max, items: []*particle{model}}, nil
	case "sequence", "choice", "all":
		kind := map[string]int{"sequence": pSeq, "choice": pChoice, "all": pAll}[n.name.Local]
		p := &particle{kind: kind, min: min, max: max}
		kids, err := xsChildren(d, n)
		if err != nil {
			return nil, err
		}
		for _, c := range kids {
			switch c.name.Local {
			case "element", "any", "group", "sequence", "choice":
				if kind == pAll && c.name.Local != "element" {
					return nil, d.errf(c, "xs:all may hold only elements")
				}
			default:
				return nil, d.errf(c, "unexpected xs:%s in xs:%s", c.name.Local, n.name.Local)
			}
			it, err := l.particle(d, c)
			if err != nil {
				return nil, err
			}
			p.items = append(p.items, it)
		}
		return p, nil
	}
	return nil, d.errf(n, "xs:%s is not a particle", n.name.Local)
}

func wildcardOf(d *schemaDoc, n *node) (*wildcard, error) {
	w := &wildcard{process: "strict"}
	if v, ok := n.attr("processContents"); ok {
		switch v {
		case "strict", "lax", "skip":
			w.process = v
		default:
			return nil, d.errf(n, "processContents %q", v)
		}
	}
	ns, ok := n.attr("namespace")
	if !ok {
		ns = "##any"
	}
	switch strings.TrimSpace(ns) {
	case "##any":
		w.any = true
	case "##other":
		w.not = []string{d.tns}
	default:
		w.list = map[string]bool{}
		for _, x := range strings.Fields(ns) {
			switch x {
			case "##targetNamespace":
				w.list[d.tns] = true
			case "##local":
				w.list[""] = true
			default:
				if strings.HasPrefix(x, "##") {
					return nil, d.errf(n, "namespace token %s", x)
				}
				w.list[x] = true
			}
		}
	}
	return w, nil
}

func (l *loader) buildGroup(g *groupDef) (*particle, error) {
	switch g.state {
	case stDone:
		return g.model, nil
	case stBuilding:
		return nil, g.doc.errf(g.raw, "model group refers to itself")
	}
	g.state = stBuilding
	kids, err := xsChildren(g.doc, g.raw)
	if err != nil {
		return nil, err
	}
	if len(kids) != 1 {
		return nil, g.doc.errf(g.raw, "xs:group needs one sequence, choice or all")
	}
	switch kids[0].name.Local {
	case "sequence", "choice", "all":
	default:
		return nil, g.doc.errf(kids[0], "unexpected xs:%s in xs:group", kids[0].name.Local)
	}
	if _, ok := kids[0].attr("minOccurs"); ok {
		return nil, g.doc.errf(kids[0], "occurrence bounds on a group's model are not allowed")
	}
	if g.model, err = l.particle(g.doc, kids[0]); err != nil {
		return nil, err
	}
	g.state = stDone
	return g.model, nil
}

// elemCommon reads what global and local element declarations share.
func (l *loader) elemCommon(e *elemDecl) error {
	d, n := e.doc, e.raw
	for _, a := range n.attrs {
		if a.name.Space != "" {
			continue
		}
		switch a.name.Local {
		case "name", "type", "ref", "minOccurs", "maxOccurs", "substitutionGroup", "abstract", "nillable",
			"fixed", "default", "form", "block", "final", "id":
		default:
			return d.errf(n, "attribute %s on xs:element is not supported", a.name.Local)
		}
	}
	if v, ok := n.attr("nillable"); ok {
		e.nillable = v == "true" || v == "1"
	}
	if v, ok := n.attr("fixed"); ok {
		e.fixed = &v
	}
	if v, ok := n.attr("default"); ok {
		e.def = &v
	}
	t, hasType, err := l.typeRef(d, n, "type")
	if err != nil {
		return err
	}
	kids, err := xsChildren(d, n)
	if err != nil {
		return err
	}
	for _, c := range kids {
		switch c.name.Local {
		case "complexType", "simpleType":
			if hasType {
				return d.errf(c, "element %s has both a type attribute and an inline type", e.name)
			}
			if t, err = l.anonymous(d, c); err != nil {
				return err
			}
			hasType = true
		case idcUnique, idcKey, idcKeyref:
			ic, err := l.idcOf(d, c)
			if err != nil {
				return err
			}
			e.idcs = append(e.idcs, ic)
			l.idcs = append(l.idcs, idcAt{ic, d, c})
		default:
			return d.errf(c, "unexpected xs:%s in xs:element", c.name.Local)
		}
	}
	e.typ = t
	return nil
}

func (l *loader) buildElem(e *elemDecl) error {
	switch e.state {
	case stDone:
		return nil
	case stBuilding:
		return e.doc.errf(e.raw, "element %s takes its type from itself through its substitution group", e.name)
	}
	e.state = stBuilding
	if err := l.elemCommon(e); err != nil {
		return err
	}
	n := e.raw
	if v, ok := n.attr("abstract"); ok {
		e.abstract = v == "true" || v == "1"
	}
	hq, ok, err := l.ref(e.doc, n, "substitutionGroup")
	if err != nil {
		return err
	}
	if ok {
		h := l.s.elems[hq]
		if h == nil {
			return e.doc.errf(n, "substitution group head %s is not declared", hq)
		}
		if err := l.buildElem(h); err != nil {
			return err
		}
		e.head = h
		if e.typ == nil {
			e.typ = h.typ
		}
	}
	if e.typ == nil {
		e.typ = l.s.anyType
	}
	e.state = stDone
	return nil
}

func (l *loader) localElem(d *schemaDoc, n *node) (*elemDecl, error) {
	if q, ok, err := l.ref(d, n, "ref"); ok || err != nil {
		if err != nil {
			return nil, err
		}
		e := l.s.elems[q]
		if e == nil {
			return nil, d.errf(n, "element %s is not declared", q)
		}
		return e, nil
	}
	name, ok := n.attr("name")
	if !ok || !isNCName(name) {
		return nil, d.errf(n, "local xs:element needs a name or ref")
	}
	for _, a := range []string{"substitutionGroup", "abstract"} {
		if _, ok := n.attr(a); ok {
			return nil, d.errf(n, "%s on a local element", a)
		}
	}
	q := qname{Local: name}
	form, ok := n.attr("form")
	if form == "qualified" || !ok && d.elemQualified {
		q.Space = d.tns
	}
	e := &elemDecl{name: q, raw: n, doc: d, state: stBuilding}
	if err := l.elemCommon(e); err != nil {
		return nil, err
	}
	if e.typ == nil {
		e.typ = l.s.anyType
	}
	e.state = stDone
	return e, nil
}

func (l *loader) buildAttr(a *attrDecl) error {
	if a.state == stDone {
		return nil
	}
	a.state = stDone
	return l.attrCommon(a)
}

func (l *loader) attrCommon(a *attrDecl) error {
	d, n := a.doc, a.raw
	if v, ok := n.attr("fixed"); ok {
		a.fixed = &v
	}
	if v, ok := n.attr("default"); ok {
		a.def = &v
	}
	t, hasType, err := l.typeRef(d, n, "type")
	if err != nil {
		return err
	}
	kids, err := xsChildren(d, n)
	if err != nil {
		return err
	}
	for _, c := range kids {
		if c.name.Local != "simpleType" || hasType {
			return d.errf(c, "unexpected xs:%s in xs:attribute", c.name.Local)
		}
		if t, err = l.anonymous(d, c); err != nil {
			return err
		}
		hasType = true
	}
	if t == nil {
		t = l.s.types[qname{nsXS, "anySimpleType"}]
	}
	if t.complex {
		return d.errf(n, "attribute %s has complex type %s", a.name, t.label())
	}
	a.typ = t
	return nil
}

func (l *loader) buildAttrGroup(g *attrGroupDef) error {
	switch g.state {
	case stDone:
		return nil
	case stBuilding:
		return g.doc.errf(g.raw, "attribute group refers to itself")
	}
	g.state = stBuilding
	kids, err := xsChildren(g.doc, g.raw)
	if err != nil {
		return err
	}
	uses, wcs, err := l.attributeDecls(g.doc, kids)
	if err != nil {
		return err
	}
	g.uses = uses
	if len(wcs) > 0 {
		g.wc = wcs[0]
	}
	g.state = stDone
	return nil
}

// attributeDecls reads attribute uses, attribute group references and an
// attribute wildcard.
func (l *loader) attributeDecls(d *schemaDoc, kids []*node) ([]*attrUse, []*wildcard, error) {
	var uses []*attrUse
	var wcs []*wildcard
	seen := map[qname]bool{}
	add := func(n *node, u *attrUse) error {
		if seen[u.decl.name] {
			return d.errf(n, "attribute %s is declared twice", u.decl.name)
		}
		seen[u.decl.name] = true
		uses = append(uses, u)
		return nil
	}
	for _, c := range kids {
		switch c.name.Local {
		case "attribute":
			u := &attrUse{}
			use, _ := c.attr("use")
			switch use {
			case "", "optional":
			case "required":
				u.required = true
			case "prohibited":
				u.prohibited = true
			default:
				return nil, nil, d.errf(c, "use %q", use)
			}
			if v, ok := c.attr("fixed"); ok {
				u.fixed = &v
			}
			if v, ok := c.attr("default"); ok {
				u.def = &v
			}
			if q, ok, err := l.ref(d, c, "ref"); ok || err != nil {
				if err != nil {
					return nil, nil, err
				}
				a := l.s.attrs[q]
				if a == nil {
					return nil, nil, d.errf(c, "attribute %s is not declared", q)
				}
				if err := l.buildAttr(a); err != nil {
					return nil, nil, err
				}
				u.decl = a
			} else {
				name, ok := c.attr("name")
				if !ok || !isNCName(name) {
					return nil, nil, d.errf(c, "xs:attribute needs a name or ref")
				}
				q := qname{Local: name}
				form, ok := c.attr("form")
				if form == "qualified" || !ok && d.attrQualified {
					q.Space = d.tns
				}
				a := &attrDecl{name: q, raw: c, doc: d, state: stDone}
				if err := l.attrCommon(a); err != nil {
					return nil, nil, err
				}
				u.decl = a
			}
			if err := add(c, u); err != nil {
				return nil, nil, err
			}
		case "attributeGroup":
			q, ok, err := l.ref(d, c, "ref")
			if err != nil {
				return nil, nil, err
			}
			if !ok {
				return nil, nil, d.errf(c, "local xs:attributeGroup needs ref")
			}
			g := l.s.attrGroups[q]
			if g == nil {
				return nil, nil, d.errf(c, "attribute group %s is not defined", q)
			}
			if err := l.buildAttrGroup(g); err != nil {
				return nil, nil, err
			}
			for _, u := range g.uses {
				if err := add(c, u); err != nil {
					return nil, nil, err
				}
			}
			if g.wc != nil {
				wcs = append(wcs, g.wc)
			}
		case "anyAttribute":
			w, err := wildcardOf(d, c)
			if err != nil {
				return nil, nil, err
			}
			wcs = append(wcs, w)
		default:
			return nil, nil, d.errf(c, "unexpected xs:%s among attribute declarations", c.name.Local)
		}
	}
	if len(wcs) > 1 {
		return nil, nil, d.errf(kids[0], "more than one attribute wildcard in one definition (their intersection) is not supported")
	}
	return uses, wcs, nil
}

func collectLeaves(p *particle) []*particle {
	var out []*particle
	var walk func(p *particle)
	walk = func(p *particle) {
		if p == nil {
			return
		}
		switch p.kind {
		case pElem, pAny:
			out = append(out, p)
		default:
			for _, it := range p.items {
				walk(it)
			}
		}
	}
	walk(p)
	return out
}

var errNoSchema = errors.New("xsd: nil schema")
