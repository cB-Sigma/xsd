// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package xsd

import (
	"sort"
	"strings"
)

// Constraints on schema components that concern the schema's own
// correctness (XSD 1.0 Part 1, the "schema component constraints" of
// each component): checked once every component is built, so that a
// schema breaking them is refused at load instead of validating documents
// in ways the schema's author cannot have meant.
//
// Checked here: value constraints (default and fixed) of elements and
// attributes against their types; that a substitution group member's type
// derives from the head's type without a derivation the head excludes;
// the final sets of simple and complex types; that list item types are
// atomic or unions; attribute uses consistent with their declarations; at
// most one ID attribute per complex type; and that an extension keeps the
// base's mixed or element-only content. Restrictions of complex types are
// checked in restrict.go.

// derivation sets are words: extension, restriction, list, union,
// substitution.
type derivSet map[string]bool

// finalOf reads a final or block attribute, falling back on the schema
// document's default (finalDefault or blockDefault), keeping only the
// words that apply.
func finalOf(n *node, d *schemaDoc, attr, def string, words ...string) derivSet {
	v, ok := n.attr(attr)
	if !ok && def != "" && d != nil && d.root != nil {
		v, _ = d.root.attr(def)
	}
	out := derivSet{}
	v = collapse(v)
	for _, w := range words {
		if v == "#all" || containsWord(v, w) {
			out[w] = true
		}
	}
	return out
}

func containsWord(list, w string) bool {
	for _, f := range strings.Fields(list) {
		if f == w {
			return true
		}
	}
	return false
}

// typeFinal is a named type's {final}; anonymous and built-in types have
// none.
func typeFinal(t *xtype) derivSet {
	if t == nil || t.raw == nil || t.name.Local == "" || t.isBuiltin() {
		return derivSet{}
	}
	if t.complex {
		return finalOf(t.raw, t.doc, "final", "finalDefault", "extension", "restriction")
	}
	return finalOf(t.raw, t.doc, "final", "finalDefault", "restriction", "list", "union")
}

// derivesOK reports whether type d derives from type b without passing a
// derivation in blocked (XSD 1.0 Part 1, Type Derivation OK): through the
// chain of bases, or, for simple types, by membership of a union b.
func derivesOK(d, b *xtype, blocked derivSet) bool {
	if d == b {
		return true
	}
	for c := d; c != nil && c.base != nil; c = c.base {
		if blocked[c.derivation] {
			return false
		}
		if c.base == b {
			return true
		}
	}
	if !b.complex && b.variety == varUnion && !d.complex {
		for _, m := range b.members {
			if derivesOK(d, m, blocked) {
				return true
			}
		}
	}
	if !b.complex && b.isBuiltin() && b.name.Local == "anySimpleType" && !d.complex {
		return true
	}
	return false
}

// elemBlock is an element declaration's {disallowed substitutions}.
func elemBlock(e *elemDecl) derivSet {
	if e == nil || e.raw == nil {
		return derivSet{}
	}
	return finalOf(e.raw, e.doc, "block", "blockDefault", "extension", "restriction", "substitution")
}

// typeBlock is a complex type's {prohibited substitutions}.
func typeBlock(t *xtype) derivSet {
	if t == nil || !t.complex || t.raw == nil || t.isBuiltin() {
		return derivSet{}
	}
	return finalOf(t.raw, t.doc, "block", "blockDefault", "extension", "restriction")
}

// substitutable reports whether member m may stand for head h in a
// document: h does not block substitution, and m's type derives from
// h's without a derivation h or its type blocks.
func substitutable(m, h *elemDecl) bool {
	hb := elemBlock(h)
	if hb["substitution"] {
		return false
	}
	blocked := derivSet{"extension": hb["extension"], "restriction": hb["restriction"]}
	for w := range typeBlock(h.typ) {
		blocked[w] = true
	}
	return m.typ == nil || h.typ == nil || derivesOK(m.typ, h.typ, blocked)
}

// isID reports whether a simple type is xs:ID or derived from it.
func isID(t *xtype) bool {
	for c := t; c != nil; c = c.base {
		if c.isBuiltin() && c.name.Local == "ID" {
			return true
		}
	}
	return false
}

// emptiable reports whether a particle can match nothing.
func emptiable(p *particle) bool {
	if p == nil || p.min == 0 {
		return true
	}
	switch p.kind {
	case pSeq, pAll:
		for _, it := range p.items {
			if !emptiable(it) {
				return false
			}
		}
		return true
	case pChoice:
		for _, it := range p.items {
			if emptiable(it) {
				return true
			}
		}
		return len(p.items) == 0
	}
	return false
}

// checkComponents runs the component checks.
func (l *loader) checkComponents() error {
	for _, t := range l.built {
		if err := l.checkType(t); err != nil {
			return err
		}
	}
	var elems []*elemDecl
	for _, e := range l.s.elems {
		elems = append(elems, e)
	}
	sort.Slice(elems, func(i, j int) bool { return elems[i].name.String() < elems[j].name.String() })
	elems = append(elems, l.locals...)
	for _, e := range elems {
		if err := l.checkElem(e); err != nil {
			return err
		}
	}
	var attrs []*attrDecl
	for _, a := range l.s.attrs {
		if a.raw != nil {
			attrs = append(attrs, a)
		}
	}
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].name.String() < attrs[j].name.String() })
	for _, a := range attrs {
		if a.name.Space == nsXSI {
			return a.doc.errf(a.raw, "an attribute may not be declared in the XML Schema instance namespace")
		}
		if err := l.valueConstraint(a.doc, a.raw, a.typ, a.def, a.fixed, "attribute "+a.name.Local); err != nil {
			return err
		}
	}
	for _, x := range l.uses {
		u := x.u
		if u.decl.raw == x.n { // a local declaration
			if u.decl.name.Space == nsXSI {
				return x.d.errf(x.n, "an attribute may not be declared in the XML Schema instance namespace")
			}
			if err := l.valueConstraint(x.d, x.n, u.decl.typ, u.decl.def, u.decl.fixed, "attribute "+u.decl.name.Local); err != nil {
				return err
			}
			continue
		}
		// a reference: its own default or fixed value
		if err := l.valueConstraint(x.d, x.n, u.decl.typ, u.def, u.fixed, "attribute "+u.decl.name.Local); err != nil {
			return err
		}
		if df := u.decl.fixed; df != nil {
			if u.def != nil {
				return x.d.errf(x.n, "attribute %s has a fixed value in its declaration, so its use may not give a default", u.decl.name.Local)
			}
			if u.fixed != nil {
				a, _ := l.s.checkSimple(u.decl.typ, *u.fixed, x.n)
				b, _ := l.s.checkSimple(u.decl.typ, *df, u.decl.raw)
				if !sameValue(primOf(u.decl.typ), a, b) {
					return x.d.errf(x.n, "attribute %s: the fixed value %q differs from the declaration's %q", u.decl.name.Local, *u.fixed, *df)
				}
			}
		}
	}
	return nil
}

func primOf(t *xtype) string {
	if t.variety == varAtomic {
		return primitive(t.builtin)
	}
	return ""
}

// valueConstraint checks a default or fixed value against a simple type.
func (l *loader) valueConstraint(d *schemaDoc, n *node, t *xtype, def, fixed *string, what string) error {
	v := def
	if v == nil {
		v = fixed
	}
	if t != nil && t.isBuiltin() && t.name.Local == "NOTATION" {
		return d.errf(n, "%s may not have type xs:NOTATION itself, only a type restricting it by enumeration", what)
	}
	if v == nil || t == nil {
		return nil
	}
	if isID(t) {
		return d.errf(n, "%s has type ID (or one derived from it) and may not have a default or fixed value", what)
	}
	if _, why := l.s.checkSimple(t, *v, n); why != "" {
		return d.errf(n, "%s: the default or fixed value %q %s", what, *v, why)
	}
	return nil
}

func (l *loader) checkElem(e *elemDecl) error {
	if e.raw == nil {
		return nil
	}
	d, n := e.doc, e.raw
	what := "element " + e.name.Local
	if t := e.typ; t != nil && t.isBuiltin() && t.name.Local == "NOTATION" {
		return d.errf(n, "%s may not have type xs:NOTATION itself, only a type restricting it by enumeration", what)
	}
	if v := e.def; v != nil || e.fixed != nil {
		t := e.typ
		switch {
		case t == nil:
		case !t.complex:
			if err := l.valueConstraint(d, n, t, e.def, e.fixed, what); err != nil {
				return err
			}
		case t.simpleContent != nil:
			if err := l.valueConstraint(d, n, t.simpleContent, e.def, e.fixed, what); err != nil {
				return err
			}
		case t.mixed && emptiable(t.content):
		default:
			return d.errf(n, "%s may not have a default or fixed value: its type has element content", what)
		}
	}
	if h := e.head; h != nil {
		excl := finalOf(h.raw, h.doc, "final", "finalDefault", "extension", "restriction")
		if !derivesOK(e.typ, h.typ, excl) {
			return d.errf(n, "%s cannot be in the substitution group of %s: its type does not derive from the head's type in a way the head allows", what, h.name.Local)
		}
	}
	return nil
}

func (l *loader) checkType(t *xtype) error {
	if t.raw == nil || t.isBuiltin() {
		return nil
	}
	d, n := t.doc, t.raw
	if !t.complex {
		switch t.derivation {
		case "restriction":
			if t.base != nil && typeFinal(t.base)["restriction"] {
				return d.errf(n, "%s restricts %s, whose final forbids restriction", t.label(), t.base.label())
			}
		case "list":
			it := t.item
			if typeFinal(it)["list"] {
				return d.errf(n, "%s is a list of %s, whose final forbids lists", t.label(), it.label())
			}
			if it.variety == varList || it.variety == varUnion && unionHasList(it, map[*xtype]bool{}) {
				return d.errf(n, "the item type of list %s must be atomic or a union of atomic types", t.label())
			}
		case "union":
			for _, m := range t.members {
				if typeFinal(m)["union"] {
					return d.errf(n, "%s has member %s, whose final forbids unions", t.label(), m.label())
				}
				if m == t || unionReaches(m, t, map[*xtype]bool{}) {
					return d.errf(n, "union %s has itself as a member", t.label())
				}
			}
		}
		return nil
	}
	if t.base != nil && t.base != l.s.anyType && typeFinal(t.base)[t.derivation] {
		return d.errf(n, "%s derives from %s by %s, which its final forbids", t.label(), t.base.label(), t.derivation)
	}
	ids := 0
	for _, u := range t.attrs {
		if isID(u.decl.typ) {
			ids++
		}
	}
	if ids > 1 {
		return d.errf(n, "%s has more than one attribute of type ID", t.label())
	}
	// Element Declarations Consistent: one name, one type, throughout the
	// content model (substitution group members included)
	byName := map[qname]*xtype{}
	for _, lf := range t.leaves {
		if lf.kind != pElem {
			continue
		}
		for _, e := range append([]*elemDecl{lf.elem}, lf.elem.memberSeq...) {
			if prev, ok := byName[e.name]; ok && prev != e.typ {
				return d.errf(n, "in %s two element declarations named %s have different types", t.label(), e.name.Local)
			}
			byName[e.name] = e.typ
		}
	}
	eff := t.content
	if t.derivation == "extension" && eff != nil && t.base != nil && len(eff.items) == 2 && eff.items[0] == t.base.content && isEmptyGroup(eff.items[1]) {
		eff = t.base.content
	}
	if !allLimited(eff, true) {
		return d.errf(n, "in %s an all group is not alone at the top of the content model, or may occur more than once", t.label())
	}
	switch t.derivation {
	case "restriction":
		return l.checkRestriction(t)
	case "extension":
		b := t.base
		if b == nil || !b.complex || b == l.s.anyType {
			return nil
		}
		if b.content == nil || isEmptyGroup(b.content) {
			return nil
		}
		if b.mixed != t.mixed {
			return d.errf(n, "%s extends %s but changes whether its content is mixed", t.label(), b.label())
		}
	}
	return nil
}

// isEmptyGroup reports a model group with no particles in it.
func isEmptyGroup(p *particle) bool {
	if p == nil {
		return true
	}
	if p.kind == pElem || p.kind == pAny {
		return false
	}
	for _, it := range p.items {
		if !isEmptyGroup(it) {
			return false
		}
	}
	return true
}

// allLimited checks that an all group stands alone at the top of a
// content model, with maxOccurs 1 (XSD 1.0 Part 1, all-group limits).
func allLimited(p *particle, top bool) bool {
	if p == nil {
		return true
	}
	switch p.kind {
	case pAll:
		if !top || p.max != 1 {
			return false
		}
		for _, it := range p.items {
			if it.kind != pElem {
				return false
			}
		}
		return true
	case pSeq, pChoice:
		if p.groupRef && len(p.items) == 1 && p.items[0].kind == pAll {
			// a reference to a group whose model is an all group
			return top && p.max == 1 && allLimited(p.items[0], true)
		}
		for _, it := range p.items {
			if !allLimited(it, false) {
				return false
			}
		}
	}
	return true
}

func unionHasList(t *xtype, seen map[*xtype]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	for _, m := range t.members {
		if m.variety == varList || m.variety == varUnion && unionHasList(m, seen) {
			return true
		}
	}
	return false
}

func unionReaches(from, target *xtype, seen map[*xtype]bool) bool {
	if seen[from] {
		return false
	}
	seen[from] = true
	for _, m := range from.members {
		if m == target || unionReaches(m, target, seen) {
			return true
		}
	}
	return false
}
