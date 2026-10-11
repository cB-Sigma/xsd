package xsd

import (
	"fmt"
	"math/bits"
	"sort"
	"strings"
)

// run is one validation of an instance document.
type run struct {
	s      *Schema
	errs   Errors
	ids    map[string]int
	idrefs []idref
	decls  map[*node]*elemDecl // validated elements, for identity constraints
	types  map[*node]*xtype    // the type each element was validated against
	opt    Options
	el     map[*node]*Element // ValidateTree only
	byEl   map[*Element]*node
}

type idref struct {
	n *node
	v string
}

// Validate checks doc against the schema under the default Limits. It
// returns nil or *Errors.
func (s *Schema) Validate(doc []byte) error {
	return s.ValidateLimits(doc, Limits{})
}

func (r *run) validate(root *node) {
	r.decls = map[*node]*elemDecl{}
	r.types = map[*node]*xtype{}
	decl := r.s.elems[root.name]
	if decl == nil {
		r.report(root, IssueRoot, "the root element %s is not a global element of the schema", root.name)
		return
	}
	r.element(root, decl)
	for _, x := range r.idrefs {
		if _, ok := r.ids[x.v]; !ok {
			r.report(x.n, IssueID, "IDREF %q does not name an ID in the document", x.v)
		}
	}
	r.identity(root)
}

func (r *run) fail(n *node, format string, args ...any) {
	r.report(n, IssueOther, format, args...)
}

func (r *run) report(n *node, kind IssueKind, format string, args ...any) {
	r.add(Issue{Line: n.line, Path: n.path(), Msg: fmt.Sprintf(format, args...), Kind: kind, Elem: r.el[n]})
}

func (r *run) add(is Issue) {
	if len(r.errs) >= maxIssues {
		return
	}
	r.errs = append(r.errs, is)
}

// valueIssue reports a value outside its simple type.
func (r *run) valueIssue(n *node, a *attr, raw, why string) {
	is := Issue{Line: n.line, Path: n.path(), Kind: IssueValue, Elem: r.el[n], Value: raw, Why: why}
	if a != nil {
		is.Attr = attrLabel(*a)
		is.Msg = fmt.Sprintf("attribute %s=%q %s", is.Attr, clip(raw), why)
	} else {
		is.Msg = fmt.Sprintf("value %q %s", clip(raw), why)
	}
	r.add(is)
}

func parseBool(v string) (bool, bool) {
	switch collapse(v) {
	case "true", "1":
		return true, true
	case "false", "0":
		return false, true
	}
	return false, false
}

func derivedFrom(x, t *xtype) bool {
	for c := x; c != nil; c = c.base {
		if c == t {
			return true
		}
	}
	return false
}

// xsiType resolves an xsi:type attribute, if any.
func (r *run) xsiType(n *node) (*xtype, bool) {
	v, ok := n.attrNS(nsXSI, "type")
	if !ok {
		return nil, true
	}
	q, err := n.resolveQName(v)
	if err != nil {
		r.fail(n, "xsi:type %v", err)
		return nil, false
	}
	t := r.s.types[q]
	if t == nil {
		r.fail(n, "xsi:type names %s, which the schema does not define", q)
		return nil, false
	}
	return t, true
}

func (r *run) element(n *node, decl *elemDecl) {
	r.decls[n] = decl
	if decl.abstract {
		r.fail(n, "element %s is abstract: a member of its substitution group must be used instead", n.display())
		return
	}
	t := decl.typ
	xt, ok := r.xsiType(n)
	if !ok {
		return
	}
	if xt != nil {
		if !derivedFrom(xt, t) {
			r.fail(n, "xsi:type %s is not derived from the declared type %s", xt.label(), t.label())
			return
		}
		t = xt
	}
	r.typed(n, t, decl)
}

// typed validates an element against a type (decl gives nillable and
// fixed; it may be nil for elements validated by xsi:type alone).
func (r *run) typed(n *node, t *xtype, decl *elemDecl) {
	if t.complex && t.abstract {
		r.fail(n, "type %s is abstract: xsi:type must name a concrete type derived from it", t.label())
		return
	}
	r.types[n] = t
	nilled := false
	if v, ok := n.attrNS(nsXSI, "nil"); ok {
		b, valid := parseBool(v)
		switch {
		case !valid:
			r.fail(n, "xsi:nil=%q is not a boolean", v)
		case b && (decl == nil || !decl.nillable):
			r.fail(n, "xsi:nil is set but %s is not nillable", n.display())
		case b:
			nilled = true
		}
	}
	text := n.text.String()
	if nilled {
		if len(n.children) > 0 || strings.TrimSpace(text) != "" {
			r.fail(n, "%s is nil (xsi:nil) and must be empty", n.display())
		}
		if decl != nil && decl.fixed != nil {
			r.fail(n, "%s has a fixed value and cannot be nil", n.display())
		}
	}
	if !t.complex {
		r.attributes(n, nil)
		if len(n.children) > 0 {
			r.fail(n.children[0], "%s has simple type %s and may not contain elements", n.display(), t.label())
			return
		}
		if !nilled {
			r.value(n, t, text, decl)
		}
		return
	}
	r.attributes(n, t)
	if nilled {
		return
	}
	if t.simpleContent != nil {
		if len(n.children) > 0 {
			r.fail(n.children[0], "%s has simple content and may not contain elements", n.display())
			return
		}
		r.value(n, t.simpleContent, text, decl)
		return
	}
	if !t.mixed && strings.TrimSpace(text) != "" {
		r.report(n, IssueCharacters, "%s may not contain character data", n.display())
	}
	if decl != nil && decl.fixed != nil && t.mixed && len(n.children) == 0 && text != "" && text != *decl.fixed {
		r.fail(n, "%s must have the fixed value %q", n.display(), *decl.fixed)
	}
	if r.opt.Opaque != nil && r.el[n] != nil && r.opt.Opaque(r.el[n]) {
		return
	}
	kids := r.admitted(n, t)
	if t.content == nil {
		if len(kids) > 0 {
			r.report(kids[0], IssueContent, "%s may not contain %s", n.display(), kids[0].display())
		}
		return
	}
	r.content(n, t, kids)
	for _, c := range kids {
		d, wc := r.s.assign(t, c.name)
		switch {
		case d != nil:
			r.element(c, d)
		case wc != nil:
			r.wild(c, wc.process)
		}
	}
}

// value checks the character content of a simple-typed element.
func (r *run) value(n *node, t *xtype, text string, decl *elemDecl) {
	if v, ok := decl.supplied(); ok && text == "" && len(n.children) == 0 {
		text = v
	}
	v, why := r.s.checkSimple(t, text, n)
	if why != "" {
		r.valueIssue(n, nil, text, why)
		return
	}
	if decl != nil && decl.fixed != nil {
		fv, _ := r.s.checkSimple(t, *decl.fixed, n)
		if fv != v {
			r.fail(n, "value %q differs from the fixed value %q", clip(v), fv)
		}
	}
	r.track(n, t, v)
}

func clip(s string) string {
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}

// track records ID and IDREF values.
func (r *run) track(n *node, t *xtype, v string) {
	kind := ""
	for c := t; c != nil; c = c.base {
		if c.isBuiltin() {
			switch c.name.Local {
			case "ID", "IDREF", "IDREFS":
				kind = c.name.Local
			}
			break
		}
		if c.variety == varList && c.item != nil {
			for it := c.item; it != nil; it = it.base {
				if it.isBuiltin() {
					if it.name.Local == "IDREF" {
						kind = "IDREFS"
					}
					break
				}
			}
			break
		}
	}
	switch kind {
	case "ID":
		if line, dup := r.ids[v]; dup {
			r.report(n, IssueID, "ID %q is already used (line %d)", v, line)
			return
		}
		r.ids[v] = n.line
	case "IDREF":
		r.idrefs = append(r.idrefs, idref{n, v})
	case "IDREFS":
		for _, x := range strings.Fields(v) {
			r.idrefs = append(r.idrefs, idref{n, x})
		}
	}
}

func (r *run) attributes(n *node, t *xtype) {
	have := map[qname]bool{}
	for _, a := range n.attrs {
		if a.name.Space == nsXSI {
			switch a.name.Local {
			case "type", "nil", "schemaLocation", "noNamespaceSchemaLocation":
			default:
				r.fail(n, "unknown attribute xsi:%s", a.name.Local)
			}
			continue
		}
		have[a.name] = true
		var use *attrUse
		if t != nil {
			for _, u := range t.attrs {
				if u.decl.name == a.name {
					use = u
					break
				}
			}
		}
		if use != nil {
			fixed := use.fixed
			if fixed == nil {
				fixed = use.decl.fixed
			}
			if fixed != nil && r.noFixed(n, a.name) {
				fixed = nil
			}
			r.attrValue(n, a, use.decl, fixed)
			continue
		}
		var wc *wildcard
		if t != nil {
			for _, w := range t.anyAttr {
				if w.allows(a.name.Space) {
					wc = w
					break
				}
			}
		}
		if r.opt.Undeclared != nil && r.el[n] != nil && r.opt.Undeclared(r.el[n], publicAttr(a), wc != nil) {
			continue
		}
		if wc == nil {
			r.report(n, IssueAttribute, "attribute %s is not allowed on %s", attrLabel(a), n.display())
			continue
		}
		if wc.process == "skip" {
			continue
		}
		d := r.s.attrs[a.name]
		if d == nil {
			if wc.process == "strict" {
				r.fail(n, "attribute %s matches a strict attribute wildcard but is not declared globally", attrLabel(a))
			}
			continue
		}
		r.attrValue(n, a, d, d.fixed)
	}
	if t == nil {
		return
	}
	for _, u := range t.attrs {
		if u.required && !have[u.decl.name] {
			if (u.fixed != nil || u.decl.fixed != nil) && r.noFixed(n, u.decl.name) {
				continue
			}
			r.report(n, IssueAttribute, "required attribute %s is missing", u.decl.name.Local)
		}
	}
}

func attrLabel(a attr) string {
	if a.prefix != "" {
		return a.prefix + ":" + a.name.Local
	}
	return a.name.Local
}

func (r *run) noFixed(n *node, q qname) bool {
	return r.opt.NoFixed != nil && r.el[n] != nil && q.Space == "" && r.opt.NoFixed(r.el[n], q.Local)
}

func (r *run) attrValue(n *node, a attr, d *attrDecl, fixed *string) {
	v, why := r.s.checkSimple(d.typ, a.value, n)
	if r.opt.Check != nil {
		tn := ""
		if d.typ.name.Space != nsXS {
			tn = d.typ.name.Local
		}
		if w := r.opt.Check(tn, a.value); w != "" {
			why = w
		}
	}
	if why != "" {
		r.valueIssue(n, &a, a.value, why)
		return
	}
	if fixed != nil {
		fv, _ := r.s.checkSimple(d.typ, *fixed, n)
		if fv != v {
			r.report(n, IssueAttribute, "attribute %s=%q differs from the fixed value %q", attrLabel(a), clip(v), fv)
			return
		}
	}
	r.track(n, d.typ, v)
}

// wild validates an element matched by a wildcard.
func (r *run) wild(n *node, process string) {
	if process == "skip" {
		return
	}
	if d := r.s.elems[n.name]; d != nil {
		r.element(n, d)
		return
	}
	xt, ok := r.xsiType(n)
	if !ok {
		return
	}
	if xt != nil {
		r.typed(n, xt, nil)
		return
	}
	if process == "strict" {
		r.fail(n, "element %s matches a strict wildcard but is not declared globally", n.display())
		return
	}
	for _, c := range n.children {
		r.wild(c, "lax")
	}
}

// ---- content models -----------------------------------------------------------

// matchElem finds the declaration an element name takes in place of decl:
// decl itself or a member of its substitution group; abstract ones never.
func matchElem(decl *elemDecl, q qname) *elemDecl {
	if decl.name == q {
		if decl.abstract {
			return nil
		}
		return decl
	}
	if m := decl.members[q]; m != nil && !m.abstract {
		return m
	}
	return nil
}

func leafMatches(p *particle, q qname) bool {
	if p.kind == pElem {
		return matchElem(p.elem, q) != nil
	}
	return p.wc.allows(q.Space)
}

// assign names the declaration or wildcard a child element is validated
// by: element particles first, then wildcards.
func (s *Schema) assign(t *xtype, q qname) (*elemDecl, *wildcard) {
	for _, lf := range t.leaves {
		if lf.kind == pElem {
			if d := matchElem(lf.elem, q); d != nil {
				return d, nil
			}
		}
	}
	for _, lf := range t.leaves {
		if lf.kind == pAny && lf.wc.allows(q.Space) {
			return nil, lf.wc
		}
	}
	return nil, nil
}

// admitted returns the children that take part in content matching: all
// of them unless Options.Child leaves some out.
func (r *run) admitted(n *node, t *xtype) []*node {
	pe := r.el[n]
	if r.opt.Child == nil || pe == nil {
		return n.children
	}
	wild := false
	for _, lf := range t.leaves {
		wild = wild || lf.kind == pAny
	}
	var out []*node
	for _, c := range n.children {
		d, _ := r.s.assign(t, c.name)
		if r.opt.Child(Child{Parent: pe, Elem: r.el[c], Declared: d != nil, Wildcard: wild}) {
			out = append(out, c)
		}
	}
	return out
}

// content matches the children against the content model. The diagnosis
// of a mismatch: first a required element that is missing, then the first
// child no reading of the model gets past (too many of it, out of order or
// not expected at all), else the content ends too early.
func (r *run) content(n *node, t *xtype, kids []*node) {
	names := make([]qname, len(kids))
	for i, c := range kids {
		names[i] = c.name
	}
	p := t.content
	w := newWalker(names)
	ends, _ := w.rep(p, []int{0})
	if w.over {
		r.report(n, IssueLimit, "%s: checking %d child elements against its content model would take too long; not validated", n.display(), len(names))
		return
	}
	if containsInt(ends, len(names)) {
		return
	}
	hint := w.reached
	req := map[qname]int{}
	minRequired(p, 1, req)
	for _, q := range sortedQNames(req) {
		if count(names, q) < req[q] {
			r.report(n, IssueContent, "%s needs at least %d %s", n.display(), req[q], q.Local)
			return
		}
	}
	far := furthest(p, names, hint)
	if far < len(names) {
		bad := kids[far]
		inModel := false
		for _, lf := range t.leaves {
			if lf.kind == pElem && lf.elem.name == bad.name && lf.elem.abstract {
				r.report(bad, IssueContent, "%s is abstract: use a member of its substitution group in %s", bad.display(), n.display())
				return
			}
			inModel = inModel || leafMatches(lf, bad.name)
		}
		exp := expected(t, names[:far])
		if limit := maxOccurs(p, bad.name); limit > 0 && count(names, bad.name) > limit {
			r.report(n, IssueContent, "%s has %d %s elements (at most %d)", n.display(), count(names, bad.name), bad.display(), limit)
			return
		}
		if inModel {
			r.report(bad, IssueContent, "%s is out of order in %s (expected %s)", bad.display(), n.display(), orNothing(exp))
			return
		}
		r.report(bad, IssueContent, "%s is not expected in %s (expected %s)", bad.display(), n.display(), orNothing(exp))
		return
	}
	r.report(n, IssueContent, "%s needs at least one more child element: %s", n.display(), orNothing(expected(t, names)))
}

// minRequired adds the elements every valid content has at least times
// (through required sequences and all groups; a choice requires nothing
// particular unless it has a single branch).
func minRequired(p *particle, times int, out map[qname]int) {
	if p.min == 0 {
		return
	}
	times *= p.min
	switch p.kind {
	case pElem:
		if !p.elem.abstract && len(p.elem.members) == 0 {
			out[p.elem.name] += times
		}
	case pSeq, pAll:
		for _, it := range p.items {
			minRequired(it, times, out)
		}
	case pChoice:
		if len(p.items) == 1 {
			minRequired(p.items[0], times, out)
		}
	}
}

func sortedQNames(m map[qname]int) []qname {
	out := make([]qname, 0, len(m))
	for q := range m {
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func count(names []qname, q qname) int {
	k := 0
	for _, x := range names {
		if x == q {
			k++
		}
	}
	return k
}

func orNothing(xs []string) string {
	if len(xs) == 0 {
		return "no further element"
	}
	if len(xs) > 8 {
		return strings.Join(xs[:8], " or ") + fmt.Sprintf(" (or %d others)", len(xs)-8)
	}
	return strings.Join(xs, " or ")
}

// expected lists what could continue a prefix of the content.
func expected(t *xtype, prefix []qname) []string {
	var out []string
	seen := map[string]bool{}
	try := func(q qname, label string) {
		if seen[label] {
			return
		}
		seen[label] = true
		if viable(t.content, append(append([]qname(nil), prefix...), q)) {
			out = append(out, label)
		}
	}
	for _, lf := range t.leaves {
		if lf.kind == pElem {
			if !lf.elem.abstract {
				try(lf.elem.name, lf.elem.name.Local)
			}
			for _, m := range lf.elem.memberSeq {
				if !m.abstract {
					try(m.name, m.name.Local)
				}
			}
			continue
		}
		if ns, ok := lf.wc.sample(); ok {
			try(qname{ns, "x"}, lf.wc.describe())
		}
	}
	sort.Strings(out)
	return out
}

// viable reports whether names can begin (or be) a content of p. Past the
// work budget it answers true, so error reporting stays bounded too.
func viable(p *particle, names []qname) bool {
	w := newWalker(names)
	ends, exh := w.rep(p, []int{0})
	return w.over || exh || containsInt(ends, len(names))
}

// furthest is the length of the longest viable prefix of names. A prefix
// of a viable prefix is viable, so a binary search finds it. hint (the
// furthest position a reading of the whole list reached) is tried first,
// which settles it in two walks.
func furthest(p *particle, names []qname, hint int) int {
	if hint >= 0 && hint < len(names) && viable(p, names[:hint]) && !viable(p, names[:hint+1]) {
		return hint
	}
	lo, hi := 0, len(names) // names[:lo] is viable (the empty prefix always is)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if viable(p, names[:mid]) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// posSet is a set of input positions in insertion order.
type posSet struct {
	list []int
	has  map[int]bool
}

func (s *posSet) add(x int) bool {
	if s.has == nil {
		// small sets (the usual case) are searched, not hashed
		for _, y := range s.list {
			if y == x {
				return false
			}
		}
		s.list = append(s.list, x)
		if len(s.list) > 16 {
			s.has = make(map[int]bool, 2*len(s.list))
			for _, y := range s.list {
				s.has[y] = true
			}
		}
		return true
	}
	if s.has[x] {
		return false
	}
	s.has[x] = true
	s.list = append(s.list, x)
	return true
}

// walkBudget bounds the work of one walk over a content (positions
// expanded and merged), so a document cannot make validation run for
// minutes: nested unbounded groups over thousands of children otherwise
// cost a power of the number of children.
const walkBudget = 20_000_000

// walker reads one list of child names against content models. Readings
// of a particle from a position are remembered, so each (particle,
// position) is worked out once per walk.
type walker struct {
	names []qname
	one   map[walkKey]walkRes
	reps  map[walkKey]walkRes
	clos  map[walkKey]closRes
	steps int
	over  bool // the budget ran out; results are incomplete
	// reached is the furthest position at which a leaf was tried: the
	// names before it are a prefix some reading consumed.
	reached int
}

type walkKey struct {
	p   *particle
	pos int
}

type walkRes struct {
	ends []int
	exh  bool
}

func newWalker(names []qname) *walker {
	return &walker{names: names, one: map[walkKey]walkRes{}, reps: map[walkKey]walkRes{}}
}

// spend counts n units of work and reports whether the budget is left.
func (w *walker) spend(n int) bool {
	w.steps += n
	if w.steps > walkBudget {
		w.over = true
	}
	return !w.over
}

// rep reads p with its occurrence bounds from each start position. It
// returns the positions where a complete reading can end, sorted, and
// whether some reading ran out of input inside p (the input is then a
// viable prefix). The result for several starts is the union of the
// results for each, which are remembered.
func (w *walker) rep(p *particle, starts []int) ([]int, bool) {
	if len(starts) == 1 {
		return w.rep1(p, starts[0])
	}
	var out posSet
	exh := false
	for _, s := range starts {
		ends, x := w.rep1(p, s)
		exh = exh || x
		if !w.spend(len(ends) + 1) {
			return nil, exh
		}
		for _, e := range ends {
			out.add(e)
		}
	}
	res := out.list
	sort.Ints(res)
	return res, exh
}

func (w *walker) rep1(p *particle, start int) ([]int, bool) {
	k := walkKey{p, start}
	if r, ok := w.reps[k]; ok {
		return r.ends, r.exh
	}
	ends, exh := w.walkRep(p, start)
	w.reps[k] = walkRes{ends, exh}
	return ends, exh
}

// closureLimit is the most children for which closures are kept as bit
// sets (their memory grows with the square of the number of children).
const closureLimit = 4096

type bitSet []uint64

func (b bitSet) set(i int)      { b[i/64] |= 1 << (i % 64) }
func (b bitSet) has(i int) bool { return b[i/64]&(1<<(i%64)) != 0 }
func (b bitSet) or(c bitSet) {
	for i := range c {
		b[i] |= c[i]
	}
}
func (b bitSet) each(f func(int)) {
	for i, x := range b {
		for x != 0 {
			j := bits.TrailingZeros64(x)
			f(i*64 + j)
			x &^= 1 << j
		}
	}
}

// closure is the set of positions that one or more readings of p, each
// consuming something, reach from pos; remembered per position, so the
// closures of an unbounded repetition cost one pass over the input.
func (w *walker) closure(p *particle, pos int) (bitSet, bool) {
	k := walkKey{p, pos}
	if r, ok := w.clos[k]; ok {
		return r.b, r.exh
	}
	b := make(bitSet, len(w.names)/64+1)
	ends, exh := w.oneAt(p, pos)
	if !w.spend(len(ends) + 1) {
		return b, exh
	}
	if !sort.IntsAreSorted(ends) {
		ends = append([]int(nil), ends...)
		sort.Ints(ends)
	}
	for _, e := range ends {
		// a position already in the set came with its closure (closures
		// are transitive), so nearer ends first save most of the work
		if e <= pos || b.has(e) {
			continue
		}
		b.set(e)
		c, x := w.closure(p, e)
		exh = exh || x
		if !w.spend(len(b)) {
			return b, exh
		}
		b.or(c)
	}
	if w.clos == nil {
		w.clos = map[walkKey]closRes{}
	}
	w.clos[k] = closRes{b, exh}
	return b, exh
}

type closRes struct {
	b   bitSet
	exh bool
}

// one reads p once (without its occurrence bounds) from pos, remembered.
func (w *walker) oneAt(p *particle, pos int) ([]int, bool) {
	k := walkKey{p, pos}
	if r, ok := w.one[k]; ok {
		return r.ends, r.exh
	}
	ends, exh := w.walkOne(p, pos)
	w.one[k] = walkRes{ends, exh}
	return ends, exh
}

// walkRep reads p with its occurrence bounds from one start position.
// Beyond the minimum of an unbounded particle the number of repetitions
// no longer matters, so each position is expanded once: the cost stays
// linear in the input for repeated elements and choices.
func (w *walker) walkRep(p *particle, start int) ([]int, bool) {
	starts := []int{start}
	var out posSet
	if !w.spend(1) {
		return nil, false
	}
	pmin, pmax := p.min, p.max
	if pmax >= 0 && pmax-pmin > len(w.names)-start {
		// repetitions past the minimum each consume an element, so a
		// maximum above what is left of the input bounds nothing
		pmax = -1
	}
	exh := false
	cur := starts
	if pmin == 0 {
		for _, s := range starts {
			out.add(s)
		}
	}
	k := 1
	// Past the minimum, a position already expanded at an earlier
	// repetition need not be expanded again: what more repetitions reach
	// from it, fewer reached first, within the same maximum.
	var done posSet
	for ; len(cur) > 0 && (pmax < 0 || k <= pmax) && !(pmax < 0 && k > pmin); k++ {
		var next posSet
		for _, s := range cur {
			if k > pmin && !done.add(s) {
				continue
			}
			ends, x := w.oneAt(p, s)
			exh = exh || x
			if !w.spend(len(ends) + 1) {
				return nil, exh
			}
			for _, e := range ends {
				// beyond the minimum, a repetition must consume something
				if e > s || k <= pmin {
					next.add(e)
				}
			}
		}
		// a set that one more repetition maps onto itself is what any
		// number of further repetitions reach, the minimum included
		stable := sameInts(next.list, cur)
		if k >= pmin || stable {
			for _, e := range next.list {
				out.add(e)
			}
		}
		if stable {
			cur = nil
			break
		}
		cur = next.list
	}
	if pmax < 0 && len(cur) > 0 && len(w.names) <= closureLimit {
		// unbounded, past the minimum: every position reachable by
		// further repetitions that consume something, from each
		// position's remembered closure
		acc := make(bitSet, len(w.names)/64+1)
		if !sort.IntsAreSorted(cur) {
			cur = append([]int(nil), cur...)
			sort.Ints(cur)
		}
		for _, s := range cur {
			if acc.has(s) {
				continue // its closure is in acc already
			}
			acc.set(s)
			b, x := w.closure(p, s)
			exh = exh || x
			if w.over || !w.spend(len(acc)) {
				return nil, exh
			}
			acc.or(b)
		}
		acc.each(func(e int) { out.add(e) })
		cur = nil
	}
	if pmax < 0 && len(cur) > 0 {
		// unbounded, past the minimum: every position reachable by
		// further repetitions that consume something
		seen := posSet{}
		for _, s := range cur {
			seen.add(s)
		}
		work := append([]int(nil), cur...)
		for len(work) > 0 {
			s := work[len(work)-1]
			work = work[:len(work)-1]
			ends, x := w.oneAt(p, s)
			exh = exh || x
			if !w.spend(len(ends) + 1) {
				return nil, exh
			}
			for _, e := range ends {
				if e > s && seen.add(e) {
					out.add(e)
					work = append(work, e)
				}
			}
		}
	}
	res := out.list
	sort.Ints(res)
	return res, exh
}

func (w *walker) walkOne(p *particle, pos int) ([]int, bool) {
	names := w.names
	switch p.kind {
	case pElem, pAny:
		if pos > w.reached {
			w.reached = pos
		}
		if pos == len(names) {
			return nil, true
		}
		if leafMatches(p, names[pos]) {
			return []int{pos + 1}, false
		}
		return nil, false
	case pSeq:
		cur := []int{pos}
		exh := false
		for _, it := range p.items {
			next, x := w.rep(it, cur)
			exh = exh || x
			cur = next
			if len(cur) == 0 {
				return nil, exh
			}
		}
		return cur, exh
	case pChoice:
		var out posSet
		exh := false
		for _, it := range p.items {
			ends, x := w.rep1(it, pos)
			exh = exh || x
			for _, e := range ends {
				out.add(e)
			}
		}
		return out.list, exh
	case pAll:
		used := make([]bool, len(p.items))
		at := pos
		defer func() {
			if at > w.reached {
				w.reached = at
			}
		}()
		for at < len(names) {
			hit := false
			for i, it := range p.items {
				if !used[i] && leafMatches(it, names[at]) {
					used[i], hit = true, true
					break
				}
			}
			if !hit {
				break
			}
			at++
		}
		for i, it := range p.items {
			if !used[i] && it.min > 0 {
				return nil, at == len(names)
			}
		}
		return []int{at}, at == len(names)
	}
	return nil, false
}

// maxOccurs is how often an element name may occur under p (-1: no bound).
func maxOccurs(p *particle, q qname) int {
	n := 0
	switch p.kind {
	case pElem, pAny:
		if leafMatches(p, q) {
			n = 1
		}
	case pChoice:
		for _, it := range p.items {
			k := maxOccurs(it, q)
			if k < 0 {
				return -1
			}
			n = max(n, k)
		}
	default:
		for _, it := range p.items {
			k := maxOccurs(it, q)
			if k < 0 {
				return -1
			}
			n += k
		}
	}
	if n == 0 {
		return 0
	}
	if p.max < 0 {
		return -1
	}
	return n * p.max
}

func addInt(xs []int, x int) []int {
	for _, y := range xs {
		if y == x {
			return xs
		}
	}
	return append(xs, x)
}

func containsInt(xs []int, x int) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[int]bool{}
	for _, x := range a {
		m[x] = true
	}
	for _, x := range b {
		if !m[x] {
			return false
		}
	}
	return true
}
