// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package xsd

import "fmt"

// A complex type derived by restriction must allow nothing its base does
// not (XSD 1.0 Part 1, Derivation Valid (Restriction, Complex) and
// Particle Valid (Restriction)). The content models are compared particle
// by particle, by the cases the recommendation tabulates for each pair of
// kinds (element, wildcard, sequence, choice, all), after removing groups
// that change nothing and expanding substitution group heads into the
// choice of their members; the attribute uses and the attribute wildcard
// are compared too.

// rp is a particle as the restriction check sees it.
type rp struct {
	kind     int
	min, max int
	items    []*rp
	elem     *elemDecl
	wc       *wildcard
	subst    bool // a choice standing for a substitution group (its order is arbitrary)
}

// normalise builds the comparison form of a particle: references to
// named groups become their group, a model group with one particle and
// occurrence 1 becomes that particle, a sequence (choice) of occurrence 1
// directly inside a sequence (choice) is merged into it, particles that
// cannot occur are dropped and substitution group heads become a choice.
func normalise(p *particle) *rp {
	if p == nil || p.max == 0 {
		return nil
	}
	switch p.kind {
	case pElem:
		if len(p.elem.memberSeq) > 0 {
			ch := &rp{kind: pChoice, min: p.min, max: p.max, subst: true}
			ch.items = append(ch.items, &rp{kind: pElem, min: 1, max: 1, elem: p.elem})
			for _, m := range p.elem.memberSeq {
				ch.items = append(ch.items, &rp{kind: pElem, min: 1, max: 1, elem: m})
			}
			return ch
		}
		return &rp{kind: pElem, min: p.min, max: p.max, elem: p.elem}
	case pAny:
		return &rp{kind: pAny, min: p.min, max: p.max, wc: p.wc}
	}
	if p.groupRef && len(p.items) == 1 {
		inner := normalise(p.items[0])
		if inner == nil {
			return nil
		}
		if inner.min == 1 && inner.max == 1 {
			inner = &rp{kind: inner.kind, min: p.min, max: p.max, items: inner.items, elem: inner.elem, wc: inner.wc}
			return pointless(inner)
		}
		return pointless(&rp{kind: pSeq, min: p.min, max: p.max, items: []*rp{inner}})
	}
	g := &rp{kind: p.kind, min: p.min, max: p.max}
	for _, it := range p.items {
		x := normalise(it)
		if x == nil {
			continue
		}
		if x.min == 1 && x.max == 1 && x.kind == g.kind && (g.kind == pSeq || g.kind == pChoice) {
			g.items = append(g.items, x.items...)
			continue
		}
		g.items = append(g.items, x)
	}
	return pointless(g)
}

func pointless(g *rp) *rp {
	if g.kind == pElem || g.kind == pAny {
		return g
	}
	if len(g.items) == 0 && g.kind != pChoice {
		return nil // an empty sequence or all group matches only nothing
	}
	if g.min == 1 && g.max == 1 && len(g.items) == 1 {
		return g.items[0]
	}
	return g
}

// rangeOK: r's occurrence range lies within b's.
func rangeOK(rmin, rmax, bmin, bmax int) bool {
	if rmin < bmin {
		return false
	}
	if bmax < 0 {
		return true
	}
	return rmax >= 0 && rmax <= bmax
}

func (x *rp) emptiable() bool {
	if x == nil || x.min == 0 {
		return true
	}
	switch x.kind {
	case pSeq, pAll:
		for _, it := range x.items {
			if !it.emptiable() {
				return false
			}
		}
		return true
	case pChoice:
		for _, it := range x.items {
			if it.emptiable() {
				return true
			}
		}
		return len(x.items) == 0
	}
	return false
}

// totalRange is a particle's effective total range: how many elements a
// reading of it may consume, at least and at most (-1: no bound).
func (x *rp) totalRange() (int, int) {
	mul := func(a, b int) int {
		if a < 0 || b < 0 {
			if a == 0 || b == 0 {
				return 0
			}
			return -1
		}
		return a * b
	}
	switch x.kind {
	case pElem, pAny:
		return x.min, x.max
	case pChoice:
		lo, hi := -2, 0
		for _, it := range x.items {
			a, b := it.totalRange()
			if lo == -2 || a < lo {
				lo = a
			}
			if hi >= 0 && (b < 0 || b > hi) {
				hi = b
			}
		}
		if lo == -2 {
			lo = 0
		}
		return mul(x.min, lo), mul(x.max, hi)
	}
	lo, hi := 0, 0
	for _, it := range x.items {
		a, b := it.totalRange()
		lo += a
		if hi >= 0 {
			if b < 0 {
				hi = -1
			} else {
				hi += b
			}
		}
	}
	return mul(x.min, lo), mul(x.max, hi)
}

// wcSubset reports whether every namespace r admits b admits too.
func wcSubset(r, b *wildcard) bool {
	switch {
	case b.any:
		return true
	case r.any:
		return false
	case r.not != nil:
		if b.not == nil {
			return false
		}
		for _, y := range b.not {
			found := false
			for _, x := range r.not {
				found = found || x == y
			}
			if !found {
				return false
			}
		}
		return true
	}
	for ns := range r.list {
		if !b.allows(ns) {
			return false
		}
	}
	return true
}

// intersectWildcards is the namespace constraint both wildcards admit, as
// a new wildcard; ok is false when XSD 1.0 cannot express it (two
// different ##other constraints).
func intersectWildcards(a, b *wildcard) (*wildcard, bool) {
	switch {
	case a.any:
		c := *b
		return &c, true
	case b.any:
		c := *a
		return &c, true
	case a.not != nil && b.not != nil:
		// both exclude the absent namespace; one more excluded namespace
		// at most can be written
		ex := map[string]bool{}
		for _, x := range append(append([]string(nil), a.not...), b.not...) {
			if x != "" {
				ex[x] = true
			}
		}
		switch len(ex) {
		case 0:
			return &wildcard{process: a.process, not: []string{""}}, true
		case 1:
			for x := range ex {
				return &wildcard{process: a.process, not: []string{x}}, true
			}
		}
		return nil, false
	}
	if a.list == nil {
		a, b = b, a // a is a list
	}
	out := &wildcard{process: a.process, list: map[string]bool{}}
	for ns := range a.list {
		if b.allows(ns) {
			out.list[ns] = true
		}
	}
	return out, true
}

// unionWildcards is the namespace constraint either wildcard admits; ok
// is false when XSD 1.0 cannot express it (a ##other constraint and a
// list holding the absent namespace but not the excluded one).
func unionWildcards(a, b *wildcard) (*wildcard, bool) {
	switch {
	case a.any || b.any:
		return &wildcard{any: true, process: a.process}, true
	case a.list != nil && b.list != nil:
		out := &wildcard{process: a.process, list: map[string]bool{}}
		for ns := range a.list {
			out.list[ns] = true
		}
		for ns := range b.list {
			out.list[ns] = true
		}
		return out, true
	case a.not != nil && b.not != nil:
		if wcSubset(a, b) && wcSubset(b, a) {
			c := *a
			return &c, true
		}
		return &wildcard{process: a.process, not: []string{""}}, true
	}
	if a.list != nil {
		a, b = b, a // a is the ##other constraint, b the list
	}
	x := ""
	for _, n := range a.not {
		if n != "" {
			x = n
		}
	}
	hasX, hasAbsent := x != "" && b.list[x], b.list[""]
	switch {
	case x == "" && hasAbsent, hasX && hasAbsent:
		return &wildcard{any: true, process: a.process}, true
	case hasX || x == "":
		return &wildcard{process: a.process, not: []string{""}}, true
	case hasAbsent:
		return nil, false
	}
	c := *a
	return &c, true
}

// extendWildcards is the attribute wildcard of an extension: the union of
// the base's and its own, processed as its own says.
func extendWildcards(d *schemaDoc, n *node, base, own []*wildcard) ([]*wildcard, error) {
	all := append(append([]*wildcard(nil), base...), own...)
	if len(all) <= 1 {
		return all, nil
	}
	w := all[0]
	for _, x := range all[1:] {
		var ok bool
		if w, ok = unionWildcards(w, x); !ok {
			return nil, d.errf(n, "the attribute wildcards of an extension and its base have a union XML Schema 1.0 cannot express")
		}
	}
	w.process = all[len(all)-1].process
	return []*wildcard{w}, nil
}

var processRank = map[string]int{"skip": 0, "lax": 1, "strict": 2}

// restricts reports why particle r is not a valid restriction of b ("" if
// it is).
func restricts(r, b *rp) string {
	switch {
	case r.kind == pElem && b.kind == pElem:
		return nameAndTypeOK(r, b)
	case r.kind == pElem && b.kind == pAny:
		if !b.wc.allows(r.elem.name.Space) {
			return fmt.Sprintf("element %s is not admitted by the base's wildcard", r.elem.name.Local)
		}
		if !rangeOK(r.min, r.max, b.min, b.max) {
			return fmt.Sprintf("element %s occurs outside the base wildcard's bounds", r.elem.name.Local)
		}
		return ""
	case r.kind == pElem:
		// as if R were a group of B's kind holding R alone; the group may
		// also take R's occurrence bounds, R then occurring once in it
		// (the two readings admit the same documents)
		why := restricts(&rp{kind: b.kind, min: 1, max: 1, items: []*rp{r}}, b)
		if why != "" && (r.min != 1 || r.max != 1) {
			if restricts(&rp{kind: b.kind, min: r.min, max: r.max, items: []*rp{{kind: pElem, min: 1, max: 1, elem: r.elem}}}, b) == "" {
				return ""
			}
		}
		return why
	case r.kind == pAny && b.kind == pAny:
		if !rangeOK(r.min, r.max, b.min, b.max) {
			return "a wildcard occurs outside the base wildcard's bounds"
		}
		if !wcSubset(r.wc, b.wc) {
			return "a wildcard admits namespaces the base's wildcard does not"
		}
		if processRank[r.wc.process] < processRank[b.wc.process] {
			return "a wildcard is processed less strictly than the base's"
		}
		return ""
	case r.kind == pAny:
		return "a wildcard cannot restrict a model group"
	case b.kind == pAny:
		// every particle inside R against B's wildcard, then R's total range
		var leaves func(x *rp) string
		leaves = func(x *rp) string {
			switch x.kind {
			case pElem, pAny:
				return restricts(&rp{kind: x.kind, min: 0, max: -1, elem: x.elem, wc: x.wc}, &rp{kind: pAny, min: 0, max: -1, wc: b.wc})
			}
			for _, it := range x.items {
				if why := leaves(it); why != "" {
					return why
				}
			}
			return ""
		}
		if why := leaves(r); why != "" {
			return why
		}
		lo, hi := r.totalRange()
		if !rangeOK(lo, hi, b.min, b.max) {
			return "a group occurs outside the base wildcard's bounds"
		}
		return ""
	}
	// both model groups
	switch {
	case r.kind == b.kind && (r.kind == pAll || r.kind == pSeq):
		return recurse(r, b, false)
	case r.kind == pChoice && b.kind == pChoice:
		return recurse(r, b, true)
	case r.kind == pSeq && b.kind == pAll:
		return recurseUnordered(r, b)
	case r.kind == pSeq && b.kind == pChoice:
		return mapAndSum(r, b)
	}
	return fmt.Sprintf("a %s cannot restrict a %s", groupWord(r.kind), groupWord(b.kind))
}

func groupWord(k int) string {
	switch k {
	case pSeq:
		return "sequence"
	case pChoice:
		return "choice"
	case pAll:
		return "all group"
	}
	return "particle"
}

func nameAndTypeOK(r, b *rp) string {
	re, be := r.elem, b.elem
	if re.name != be.name {
		return fmt.Sprintf("element %s does not correspond to the base's %s", re.name.Local, be.name.Local)
	}
	if !rangeOK(r.min, r.max, b.min, b.max) {
		return fmt.Sprintf("element %s occurs outside the base's bounds", re.name.Local)
	}
	if re == be {
		return ""
	}
	if re.nillable && !be.nillable {
		return fmt.Sprintf("element %s is nillable where the base's is not", re.name.Local)
	}
	if be.fixed != nil {
		if re.fixed == nil {
			return fmt.Sprintf("element %s must keep the base's fixed value", re.name.Local)
		}
		t := be.typ
		if t != nil && t.complex {
			t = t.simpleContent
		}
		if t != nil {
			x, _ := (&Schema{}).checkSimple(t, *re.fixed, re.raw)
			y, _ := (&Schema{}).checkSimple(t, *be.fixed, be.raw)
			if !sameValue(primOf(t), x, y) {
				return fmt.Sprintf("element %s changes the base's fixed value", re.name.Local)
			}
		} else if *re.fixed != *be.fixed {
			return fmt.Sprintf("element %s changes the base's fixed value", re.name.Local)
		}
	}
	rb := finalOf(re.raw, re.doc, "block", "blockDefault", "extension", "restriction", "substitution")
	bb := finalOf(be.raw, be.doc, "block", "blockDefault", "extension", "restriction", "substitution")
	for w := range bb {
		if !rb[w] {
			return fmt.Sprintf("element %s blocks less than the base's", re.name.Local)
		}
	}
	if len(re.idcs) > 0 {
		have := map[qname]bool{}
		for _, c := range be.idcs {
			have[c.name] = true
		}
		for _, c := range re.idcs {
			if !have[c.name] {
				return fmt.Sprintf("element %s adds an identity constraint", re.name.Local)
			}
		}
	}
	if re.typ != nil && be.typ != nil && !derivesOK(re.typ, be.typ, derivSet{"extension": true}) {
		return fmt.Sprintf("the type of element %s does not restrict the base's", re.name.Local)
	}
	return ""
}

// recurse maps R's particles in order onto B's (lax: B's unmapped
// particles need not be emptiable, as for a choice).
func recurse(r, b *rp, lax bool) string {
	if !rangeOK(r.min, r.max, b.min, b.max) {
		return fmt.Sprintf("a %s occurs outside the base's bounds", groupWord(r.kind))
	}
	if lax && (r.subst || b.subst) {
		// a substitution group has no order of its own, so its branches
		// map in any order
		for _, x := range r.items {
			ok := false
			var last string
			for _, y := range b.items {
				if why := restricts(x, y); why == "" {
					ok = true
					break
				} else {
					last = why
				}
			}
			if !ok {
				if last == "" {
					last = "a branch has no counterpart in the base's choice"
				}
				return last
			}
		}
		return ""
	}
	j := 0
	for _, x := range r.items {
		matched := false
		var last string
		for j < len(b.items) {
			y := b.items[j]
			j++
			if why := restricts(x, y); why == "" {
				matched = true
				break
			} else {
				last = why
			}
			if !lax && !y.emptiable() {
				break
			}
		}
		if !matched {
			if last == "" {
				last = "a particle has no counterpart in the base"
			}
			return last
		}
	}
	if !lax {
		for ; j < len(b.items); j++ {
			if !b.items[j].emptiable() {
				return "a required particle of the base is left out"
			}
		}
	}
	return ""
}

func recurseUnordered(r, b *rp) string {
	if !rangeOK(r.min, r.max, b.min, b.max) {
		return "a sequence occurs outside the base's bounds"
	}
	used := make([]bool, len(b.items))
	for _, x := range r.items {
		found := false
		for j, y := range b.items {
			if used[j] {
				continue
			}
			if restricts(x, y) == "" {
				used[j], found = true, true
				break
			}
		}
		if !found {
			return "a particle of the sequence has no counterpart in the base's all group"
		}
		if x.max < 0 || x.max > 1 {
			return "a particle restricting an all group may occur at most once"
		}
	}
	for j, y := range b.items {
		if !used[j] && !y.emptiable() {
			return "a required particle of the base's all group is left out"
		}
	}
	return ""
}

func mapAndSum(r, b *rp) string {
	n := len(r.items)
	lo, hi := r.min*n, r.max*n
	if r.max < 0 {
		hi = -1
	}
	if !rangeOK(lo, hi, b.min, b.max) {
		return "a sequence occurs outside the base choice's bounds"
	}
	for _, x := range r.items {
		ok := false
		for _, y := range b.items {
			if restricts(x, y) == "" {
				ok = true
				break
			}
		}
		if !ok {
			return "a particle of the sequence matches no branch of the base's choice"
		}
	}
	return ""
}

// checkRestriction checks a complex type derived by restriction against
// its base.
func (l *loader) checkRestriction(t *xtype) error {
	b := t.base
	d, n := t.doc, t.raw
	if b == nil || !b.complex || b == l.s.anyType {
		return nil
	}
	// attributes
	baseUse := map[qname]*attrUse{}
	for _, u := range b.attrs {
		baseUse[u.decl.name] = u
	}
	have := map[qname]bool{}
	for _, u := range t.attrs {
		have[u.decl.name] = true
		bu := baseUse[u.decl.name]
		if bu == u {
			continue
		}
		if bu == nil {
			ok := false
			for _, w := range b.anyAttr {
				ok = ok || w.allows(u.decl.name.Space)
			}
			if !ok {
				return d.errf(n, "%s adds attribute %s, which its base %s does not allow", t.label(), u.decl.name.Local, b.label())
			}
			continue
		}
		if bu.required && !u.required {
			return d.errf(n, "%s makes attribute %s optional where its base requires it", t.label(), u.decl.name.Local)
		}
		if u.decl.typ != nil && bu.decl.typ != nil && !derivesOK(u.decl.typ, bu.decl.typ, derivSet{"extension": true}) {
			return d.errf(n, "the type of attribute %s in %s does not restrict its base's", u.decl.name.Local, t.label())
		}
		bf := bu.fixed
		if bf == nil {
			bf = bu.decl.fixed
		}
		if bf != nil {
			rf := u.fixed
			if rf == nil {
				rf = u.decl.fixed
			}
			if rf == nil {
				return d.errf(n, "attribute %s in %s must keep its base's fixed value", u.decl.name.Local, t.label())
			}
			x, _ := l.s.checkSimple(u.decl.typ, *rf, n)
			y, _ := l.s.checkSimple(bu.decl.typ, *bf, n)
			if !sameValue(primOf(bu.decl.typ), x, y) {
				return d.errf(n, "attribute %s in %s changes its base's fixed value", u.decl.name.Local, t.label())
			}
		}
	}
	for _, u := range b.attrs {
		if u.required && !have[u.decl.name] {
			return d.errf(n, "%s leaves out attribute %s, which its base requires", t.label(), u.decl.name.Local)
		}
	}
	for _, w := range t.anyAttr {
		ok := false
		for _, bw := range b.anyAttr {
			if wcSubset(w, bw) && processRank[w.process] >= processRank[bw.process] {
				ok = true
			}
		}
		if !ok {
			return d.errf(n, "the attribute wildcard of %s is not a subset of its base's", t.label())
		}
	}
	// content
	if t.simpleContent != nil {
		return nil // the simple type was checked as a restriction of the base's
	}
	if b.simpleContent != nil {
		return d.errf(n, "%s restricts %s, which has simple content", t.label(), b.label())
	}
	if t.mixed && !b.mixed {
		return d.errf(n, "%s is mixed but its base %s is not", t.label(), b.label())
	}
	rc, bc := normalise(t.content), normalise(b.content)
	switch {
	case rc == nil:
		if bc != nil && !bc.emptiable() {
			return d.errf(n, "%s has empty content but its base %s requires elements", t.label(), b.label())
		}
		return nil
	case bc == nil:
		return d.errf(n, "%s has content but its base %s is empty", t.label(), b.label())
	}
	if why := restricts(rc, bc); why != "" {
		return d.errf(n, "%s is not a valid restriction of %s: %s", t.label(), b.label(), why)
	}
	return nil
}
