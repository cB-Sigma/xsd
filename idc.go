package xsd

import (
	"regexp"
	"strings"
)

// Identity constraints: xs:unique, xs:key and xs:keyref (XSD 1.0 Part 1,
// 3.11) with the restricted XPath subset the specification defines for
// selectors and fields: alternatives separated by "|", each an optional
// ".//" then steps separated by "/", a step being ".", a name test (QName,
// "*" or "prefix:*", optionally after "child::"), and for fields a final
// attribute step ("@name", "@*", "attribute::name"). Names in the paths are
// resolved with the namespace bindings of the constraint's schema
// document; an unprefixed name is in no namespace.
//
// Field values are compared in the value space of the field's simple type
// (valueKey): "1" and "1.0" are one xs:decimal value, " Tea " and "Tea" two
// xs:string values, and values of different primitive types never match.
// An absent attribute or an empty element with a default or fixed value
// takes that value, as validation would supply it.

const (
	idcUnique = "unique"
	idcKey    = "key"
	idcKeyref = "keyref"
)

type idc struct {
	kind     string
	name     qname
	referQ   qname
	refer    *idc
	selector []xpath
	fields   [][]xpath
	src      []string // field xpaths as written, for messages
}

// nameTest matches an element or attribute name; an empty local means any
// local name, anySpace any namespace.
type nameTest struct {
	anySpace bool
	space    string
	local    string
}

func (t nameTest) matches(q qname) bool {
	return (t.anySpace || t.space == q.Space) && (t.local == "" || t.local == q.Local)
}

type xpath struct {
	deep  bool
	steps []nameTest // "." steps are dropped
	attr  *nameTest  // fields only
}

// idcOf reads an xs:unique, xs:key or xs:keyref inside an element
// declaration.
func (l *loader) idcOf(d *schemaDoc, n *node) (*idc, error) {
	c := &idc{kind: n.name.Local}
	name, ok := n.attr("name")
	if !ok || !isNCName(name) {
		return nil, d.errf(n, "xs:%s needs a name", c.kind)
	}
	c.name = qname{d.tns, name}
	if c.kind == idcKeyref {
		q, ok, err := l.ref(d, n, "refer")
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, d.errf(n, "xs:keyref %s needs refer", name)
		}
		c.referQ = q
	}
	kids, err := xsChildren(d, n)
	if err != nil {
		return nil, err
	}
	for i, k := range kids {
		xp, _ := k.attr("xpath")
		switch {
		case k.name.Local == "selector" && i == 0:
			if c.selector, err = parseXPath(k, xp, false); err != nil {
				return nil, d.errf(k, "selector of %s: %v", name, err)
			}
		case k.name.Local == "field" && i > 0:
			f, err := parseXPath(k, xp, true)
			if err != nil {
				return nil, d.errf(k, "field of %s: %v", name, err)
			}
			c.fields = append(c.fields, f)
			c.src = append(c.src, strings.TrimSpace(xp))
		default:
			return nil, d.errf(k, "unexpected xs:%s in xs:%s", k.name.Local, c.kind)
		}
	}
	if c.selector == nil || len(c.fields) == 0 {
		return nil, d.errf(n, "xs:%s %s needs a selector and at least one field", c.kind, name)
	}
	return c, nil
}

type xpathError string

func (e xpathError) Error() string { return string(e) }

// parseXPath reads a selector or field expression.
func parseXPath(ctx *node, expr string, field bool) ([]xpath, error) {
	// white space may stand between tokens: drop it around the operators
	expr = reXPathSpace.ReplaceAllString(expr, "$1")
	var out []xpath
	for _, alt := range strings.Split(expr, "|") {
		p := strings.TrimSpace(alt)
		var x xpath
		if strings.HasPrefix(p, ".//") {
			x.deep, p = true, strings.TrimSpace(p[3:])
			if p == "" {
				return nil, xpathError("path " + quoteXP(alt) + " ends after .//")
			}
		}
		steps := strings.Split(p, "/")
		for i, st := range steps {
			st = strings.TrimSpace(st)
			last := i == len(steps)-1
			switch {
			case st == ".":
				continue
			case field && last && (strings.HasPrefix(st, "@") || strings.HasPrefix(st, "attribute::")):
				st = strings.TrimPrefix(strings.TrimPrefix(st, "@"), "attribute::")
				t, err := parseNameTest(ctx, strings.TrimSpace(st), true)
				if err != nil {
					return nil, err
				}
				x.attr = &t
			default:
				st = strings.TrimSpace(strings.TrimPrefix(st, "child::"))
				t, err := parseNameTest(ctx, st, false)
				if err != nil {
					return nil, err
				}
				x.steps = append(x.steps, t)
			}
		}
		out = append(out, x)
	}
	return out, nil
}

var reXPathSpace = regexp.MustCompile(`[ \t\r\n]*(::|//|/|\||@)[ \t\r\n]*`)

func quoteXP(s string) string { return `"` + strings.TrimSpace(s) + `"` }

func parseNameTest(ctx *node, s string, attr bool) (nameTest, error) {
	if s == "*" {
		return nameTest{anySpace: true}, nil
	}
	prefix, local, qualified := strings.Cut(s, ":")
	if !qualified {
		prefix, local = "", s
	}
	if local == "*" && qualified {
		local = ""
	} else if !isNCName(local) {
		return nameTest{}, xpathError(quoteXP(s) + " is not a name test of the XSD XPath subset")
	}
	if prefix == "" {
		return nameTest{local: local}, nil // unprefixed: no namespace (attributes and elements alike)
	}
	if !isNCName(prefix) {
		return nameTest{}, xpathError(quoteXP(s) + " is not a name test of the XSD XPath subset")
	}
	space, ok := ctx.ns.lookup(prefix)
	if !ok {
		return nameTest{}, xpathError("prefix " + quoteXP(prefix) + " is not declared")
	}
	return nameTest{space: space, local: local}, nil
}

// resolveIDCs links each keyref to the key or unique it refers to.
func (l *loader) resolveIDCs() error {
	byName := map[qname]*idc{}
	for _, c := range l.idcs {
		if byName[c.name] != nil {
			return c.docErr("identity constraint %s is declared twice", c.name)
		}
		byName[c.name] = c.idc
	}
	for _, c := range l.idcs {
		if c.kind != idcKeyref {
			continue
		}
		r := byName[c.referQ]
		if r == nil || r.kind == idcKeyref {
			return c.docErr("keyref %s refers to %s, which is not a key or unique constraint", c.name.Local, c.referQ)
		}
		if len(r.fields) != len(c.fields) {
			return c.docErr("keyref %s has %d fields, %s has %d", c.name.Local, len(c.fields), r.name.Local, len(r.fields))
		}
		c.refer = r
	}
	return nil
}

type idcAt struct {
	*idc
	d *schemaDoc
	n *node
}

func (c idcAt) docErr(format string, args ...any) error { return c.d.errf(c.n, format, args...) }

// ---- evaluation -------------------------------------------------------------

// keyTable is the node table of one key or unique constraint at one
// element: key tuple to the element that supplied it (nil once two
// descendant tables disagree, which excludes the tuple).
type keyTable map[string]*node

// identity evaluates the constraints bottom up and returns the key tables
// the element exposes to its ancestors.
func (r *run) identity(n *node) map[*idc]keyTable {
	var up map[*idc]keyTable
	for _, c := range n.children {
		for k, t := range r.identity(c) {
			if up == nil {
				up = map[*idc]keyTable{}
			}
			if up[k] == nil {
				up[k] = keyTable{}
			}
			for key, x := range t {
				if prev, dup := up[k][key]; dup && prev != x {
					up[k][key] = nil
				} else if !dup {
					up[k][key] = x
				}
			}
		}
	}
	decl := r.decls[n]
	if decl == nil || len(decl.idcs) == 0 {
		return up
	}
	own := map[*idc]keyTable{}
	for _, c := range decl.idcs {
		if c.kind == idcKeyref {
			continue
		}
		t := keyTable{}
		for _, x := range r.selectNodes(n, c.selector) {
			key, shown, ok := r.tuple(x, c)
			if !ok {
				if c.kind == idcKey {
					r.report(x, IssueIdentity, "%s lacks %s, required by key %s", x.display(), strings.Join(c.src, " and "), c.name.Local)
				}
				continue
			}
			if first, dup := t[key]; dup {
				r.report(x, IssueIdentity, "%s duplicates %s (line %d) in %s: %s must be unique", x.display(), shown, first.line, n.display(), c.name.Local)
				continue
			}
			t[key] = x
		}
		own[c] = t
	}
	for _, c := range decl.idcs {
		if c.kind != idcKeyref {
			continue
		}
		t, ok := own[c.refer]
		if !ok {
			t = up[c.refer]
		}
		for _, x := range r.selectNodes(n, c.selector) {
			key, shown, ok := r.tuple(x, c)
			if !ok {
				continue
			}
			if t[key] == nil {
				r.report(x, IssueIdentity, "%s refers to %s which is not defined (%s)", x.display(), shown, c.name.Local)
			}
		}
	}
	if up == nil {
		up = map[*idc]keyTable{}
	}
	for c, t := range own {
		up[c] = t // the element's own table replaces what descendants supplied
	}
	return up
}

// selectNodes evaluates a selector from ctx; each element once, in
// document order of the alternatives.
func (r *run) selectNodes(ctx *node, paths []xpath) []*node {
	var out []*node
	seen := map[*node]bool{}
	for _, p := range paths {
		for _, x := range r.walkPath(ctx, p) {
			if !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
		}
	}
	return out
}

func (r *run) walkPath(ctx *node, p xpath) []*node {
	cur := []*node{ctx}
	if p.deep {
		// ".//" is descendant-or-self: the first step applies to ctx's
		// descendants at any depth (a leading "." step was dropped).
		var all []*node
		var desc func(n *node)
		desc = func(n *node) {
			all = append(all, n)
			for _, c := range n.children {
				desc(c)
			}
		}
		desc(ctx)
		cur = all
	}
	for _, st := range p.steps {
		var next []*node
		for _, c := range cur {
			for _, ch := range c.children {
				if st.matches(ch.name) {
					next = append(next, ch)
				}
			}
		}
		cur = next
	}
	return cur
}

// tuple evaluates the fields of c on x: the key compared and the values
// as written, for messages; ok is false when a field selects nothing. A
// field selecting more than one node is reported.
func (r *run) tuple(x *node, c *idc) (key, shown string, ok bool) {
	keys := make([]string, 0, len(c.fields))
	vals := make([]string, 0, len(c.fields))
	for i, f := range c.fields {
		var got []fieldValue
		for _, p := range f {
			if p.attr == nil {
				for _, e := range r.walkPath(x, p) {
					if t := r.types[e]; t != nil && t.complex && t.simpleContent == nil {
						r.report(x, IssueIdentity, "field %s of %s selects %s, whose type has no simple value", c.src[i], c.name.Local, e.display())
						return "", "", false
					}
					got = append(got, r.elementValue(e))
				}
				continue
			}
			for _, e := range r.walkPath(x, xpath{deep: p.deep, steps: p.steps}) {
				got = append(got, r.attrValues(e, p.attr)...)
			}
		}
		switch len(got) {
		case 0:
			return "", "", false
		case 1:
			keys = append(keys, got[0].key)
			vals = append(vals, got[0].shown)
		default:
			r.report(x, IssueIdentity, "field %s of %s selects %d values on %s (at most one)", c.src[i], c.name.Local, len(got), x.display())
			return "", "", false
		}
	}
	return strings.Join(keys, "\x00"), strings.Join(vals, "/"), true
}

// fieldValue is one field's value: its key in the value space and the
// value as written.
type fieldValue struct{ key, shown string }

// typedValue makes the field value of raw under type t (nil when the
// type is unknown or not simple: the collapsed string is compared then).
func (r *run) typedValue(t *xtype, raw string, ctx *node) fieldValue {
	shown := collapse(raw)
	if t != nil {
		if k, ok := r.s.valueKey(t, raw, ctx); ok {
			return fieldValue{k, shown}
		}
	}
	return fieldValue{"\x01" + shown, shown}
}

func (r *run) elementValue(e *node) fieldValue {
	raw := e.text.String()
	if v, ok := r.decls[e].supplied(); ok && raw == "" && len(e.children) == 0 {
		raw = v
	}
	t := r.types[e]
	if t != nil && t.complex {
		t = t.simpleContent
	}
	return r.typedValue(t, raw, e)
}

// attrValues are the values of e's attributes that match the name test,
// those supplied by a default or fixed value included.
func (r *run) attrValues(e *node, test *nameTest) []fieldValue {
	var out []fieldValue
	t := r.types[e]
	have := map[qname]bool{}
	for _, a := range e.attrs {
		if a.name.Space == nsXSI || !test.matches(a.name) {
			continue
		}
		have[a.name] = true
		out = append(out, r.typedValue(r.attrType(t, a.name), a.value, e))
	}
	if t == nil {
		return out
	}
	for _, u := range t.attrs {
		if have[u.decl.name] || !test.matches(u.decl.name) {
			continue
		}
		if v, ok := u.supplied(); ok {
			out = append(out, r.typedValue(u.decl.typ, v, e))
		}
	}
	return out
}

// attrType is the declared type of an attribute of an element of type t:
// its attribute use, else a global declaration (through a wildcard).
func (r *run) attrType(t *xtype, q qname) *xtype {
	if t != nil {
		for _, u := range t.attrs {
			if u.decl.name == q {
				return u.decl.typ
			}
		}
	}
	if d := r.s.attrs[q]; d != nil {
		return d.typ
	}
	return nil
}
