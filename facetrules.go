// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package xsd

import "strconv"

// Constraints on facets (XSD 1.0 Part 2, the facets' schema component
// constraints): which facets a datatype admits, how facets of one
// restriction step must agree with each other, and that a restriction only
// narrows what its base allows (a derived maxLength no larger than the
// base's, a range inside the base's range, white space no less collapsed,
// a fixed facet unchanged).

// facetApplies reports whether a restriction of t's variety and primitive
// type admits the facet.
func facetApplies(t *xtype, facet string) bool {
	length := facet == "length" || facet == "minLength" || facet == "maxLength"
	rangeF := facet == "minInclusive" || facet == "maxInclusive" || facet == "minExclusive" || facet == "maxExclusive"
	digits := facet == "totalDigits" || facet == "fractionDigits"
	switch t.variety {
	case varList:
		return length || facet == "pattern" || facet == "enumeration" || facet == "whiteSpace"
	case varUnion:
		return facet == "pattern" || facet == "enumeration"
	}
	switch prim := primitive(t.builtin); prim {
	case "string", "anyURI", "QName", "NOTATION", "hexBinary", "base64Binary":
		return !rangeF && !digits
	case "boolean":
		return facet == "pattern" || facet == "whiteSpace"
	case "decimal":
		return !length
	default:
		if isNumeric(prim) || isOrderedTime(prim) {
			return !length && !digits
		}
	}
	return false
}

func facetSubject(t *xtype) string {
	switch t.variety {
	case varList:
		return "a list type"
	case varUnion:
		return "a union type"
	}
	return "a type derived from " + primitive(t.builtin)
}

// effective are the facets in force on a type: for each, the nearest
// step of its derivation that sets it, with whether that step fixed it.
type effective struct {
	f     *facetSet
	fixed map[string]bool
}

func effectiveFacets(t *xtype) effective {
	e := effective{f: newFacetSet(), fixed: map[string]bool{}}
	take := func(name string, set bool, fx map[string]bool) {
		if set && fx[name] {
			if _, done := e.fixed[name]; !done {
				e.fixed[name] = true
			}
		}
	}
	for cur := t; cur != nil; cur = cur.base {
		f := cur.facets
		if f == nil {
			continue
		}
		if e.f.length < 0 && f.length >= 0 {
			e.f.length = f.length
			take("length", true, f.fixed)
		}
		if e.f.minLength < 0 && f.minLength >= 0 {
			e.f.minLength = f.minLength
			take("minLength", true, f.fixed)
		}
		if e.f.maxLength < 0 && f.maxLength >= 0 {
			e.f.maxLength = f.maxLength
			take("maxLength", true, f.fixed)
		}
		if e.f.totalDigits < 0 && f.totalDigits >= 0 {
			e.f.totalDigits = f.totalDigits
			take("totalDigits", true, f.fixed)
		}
		if e.f.fractionDigits < 0 && f.fractionDigits >= 0 {
			e.f.fractionDigits = f.fractionDigits
			take("fractionDigits", true, f.fixed)
		}
		if e.f.ws < 0 && f.ws >= 0 {
			e.f.ws = f.ws
			take("whiteSpace", true, f.fixed)
		}
		for _, r := range []struct {
			name     string
			dst, src *string
		}{{"minInclusive", &e.f.minIncl, &f.minIncl}, {"maxInclusive", &e.f.maxIncl, &f.maxIncl},
			{"minExclusive", &e.f.minExcl, &f.minExcl}, {"maxExclusive", &e.f.maxExcl, &f.maxExcl}} {
			if *r.dst == "" && *r.src != "" {
				*r.dst = *r.src
				take(r.name, true, f.fixed)
			}
		}
	}
	// facets the built-in types carry: integers have no fraction digits
	// (fixed) and the sized integers have a range
	if t.variety == varAtomic {
		for cur := t; cur != nil; cur = cur.base {
			if !cur.isBuiltin() {
				continue
			}
			if derivesFromBuiltin(cur.name.Local, "integer") && e.f.fractionDigits < 0 {
				e.f.fractionDigits = 0
				e.fixed["fractionDigits"] = true
			}
			if b, ok := intBounds[cur.name.Local]; ok {
				if e.f.minIncl == "" && e.f.minExcl == "" && b[0] != "" {
					e.f.minIncl = b[0]
				}
				if e.f.maxIncl == "" && e.f.maxExcl == "" && b[1] != "" {
					e.f.maxIncl = b[1]
				}
			}
			break
		}
	}
	return e
}

// derivesFromBuiltin reports whether built-in type name is anc or derives
// from it.
func derivesFromBuiltin(name, anc string) bool {
	for name != "" {
		if name == anc {
			return true
		}
		name = builtinBase[name]
	}
	return false
}

// facetRules checks the facets one restriction step of t declares (f, by
// facet name in nodes) against each other and against t's base.
func (l *loader) facetRules(t *xtype, f *facetSet, nodes map[string]*node) error {
	d := t.doc
	at := func(facet string) *node {
		if n := nodes[facet]; n != nil {
			return n
		}
		return t.raw
	}
	fail := func(facet, format string, args ...any) error {
		return d.errf(at(facet), format, args...)
	}
	base := t.base
	if base == nil {
		return nil
	}
	be := effectiveFacets(base)
	b := be.f
	prim := ""
	if t.variety == varAtomic {
		prim = primitive(t.builtin)
	}

	// lengths
	// length beside minLength or maxLength in one step only restates a
	// bound the base already has
	if f.length >= 0 && f.minLength >= 0 && !(f.minLength == b.minLength && f.minLength <= f.length) ||
		f.length >= 0 && f.maxLength >= 0 && !(f.maxLength == b.maxLength && f.maxLength >= f.length) {
		return fail("length", "length and minLength or maxLength in one restriction")
	}
	if f.length >= 0 {
		switch {
		case b.length >= 0 && f.length != b.length:
			return fail("length", "length %d differs from the base type's %d", f.length, b.length)
		case b.minLength >= 0 && f.length < b.minLength:
			return fail("length", "length %d is below the base type's minLength %d", f.length, b.minLength)
		case b.maxLength >= 0 && f.length > b.maxLength:
			return fail("length", "length %d is above the base type's maxLength %d", f.length, b.maxLength)
		}
	}
	if f.minLength >= 0 {
		switch {
		case b.minLength >= 0 && f.minLength < b.minLength:
			return fail("minLength", "minLength %d is below the base type's %d", f.minLength, b.minLength)
		case b.maxLength >= 0 && f.minLength > b.maxLength:
			return fail("minLength", "minLength %d is above the base type's maxLength %d", f.minLength, b.maxLength)
		case b.length >= 0 && f.minLength > b.length:
			return fail("minLength", "minLength %d is above the base type's length %d", f.minLength, b.length)
		case f.maxLength >= 0 && f.minLength > f.maxLength:
			return fail("minLength", "minLength %d is above maxLength %d", f.minLength, f.maxLength)
		}
	}
	if f.maxLength >= 0 {
		switch {
		case b.maxLength >= 0 && f.maxLength > b.maxLength:
			return fail("maxLength", "maxLength %d is above the base type's %d", f.maxLength, b.maxLength)
		case b.minLength >= 0 && f.maxLength < b.minLength:
			return fail("maxLength", "maxLength %d is below the base type's minLength %d", f.maxLength, b.minLength)
		case b.length >= 0 && f.maxLength < b.length:
			return fail("maxLength", "maxLength %d is below the base type's length %d", f.maxLength, b.length)
		}
	}

	// digits
	if f.totalDigits >= 0 && b.totalDigits >= 0 && f.totalDigits > b.totalDigits {
		return fail("totalDigits", "totalDigits %d is above the base type's %d", f.totalDigits, b.totalDigits)
	}
	if f.fractionDigits >= 0 && b.fractionDigits >= 0 && f.fractionDigits > b.fractionDigits {
		return fail("fractionDigits", "fractionDigits %d is above the base type's %d", f.fractionDigits, b.fractionDigits)
	}
	td, fd := f.totalDigits, f.fractionDigits
	if td < 0 {
		td = b.totalDigits
	}
	if fd < 0 {
		fd = b.fractionDigits
	}
	if td >= 0 && fd >= 0 && fd > td && (f.totalDigits >= 0 || f.fractionDigits >= 0) {
		return fail("fractionDigits", "fractionDigits %d is above totalDigits %d", fd, td)
	}

	// white space
	if f.ws >= 0 {
		bws := base.ws
		if base.variety != varAtomic {
			bws = wsCollapse
		}
		if f.ws < bws {
			return fail("whiteSpace", "whiteSpace may not be less collapsed than the base type's")
		}
	}

	// ranges
	if f.minIncl != "" && f.minExcl != "" {
		return fail("minInclusive", "minInclusive and minExclusive in one restriction")
	}
	if f.maxIncl != "" && f.maxExcl != "" {
		return fail("maxInclusive", "maxInclusive and maxExclusive in one restriction")
	}
	cmp := func(a, b string) (int, bool) { return compare(prim, a, b) }
	// rel checks a op b where both are set and comparable.
	type rule struct {
		facet string
		a, b  string
		ok    func(int) bool
		what  string
	}
	le := func(c int) bool { return c <= 0 }
	lt := func(c int) bool { return c < 0 }
	ge := func(c int) bool { return c >= 0 }
	gt := func(c int) bool { return c > 0 }
	rules := []rule{
		// within the step
		{"minInclusive", f.minIncl, f.maxIncl, le, "above maxInclusive"},
		{"minExclusive", f.minExcl, f.maxExcl, le, "above maxExclusive"},
		{"minExclusive", f.minExcl, f.maxIncl, lt, "not below maxInclusive"},
		{"minInclusive", f.minIncl, f.maxExcl, lt, "not below maxExclusive"},
		// against the base
		{"minInclusive", f.minIncl, b.minIncl, ge, "below the base type's minInclusive"},
		{"minInclusive", f.minIncl, b.maxIncl, le, "above the base type's maxInclusive"},
		{"minInclusive", f.minIncl, b.minExcl, gt, "not above the base type's minExclusive"},
		{"minInclusive", f.minIncl, b.maxExcl, lt, "not below the base type's maxExclusive"},
		{"maxInclusive", f.maxIncl, b.maxIncl, le, "above the base type's maxInclusive"},
		{"maxInclusive", f.maxIncl, b.minIncl, ge, "below the base type's minInclusive"},
		{"maxInclusive", f.maxIncl, b.minExcl, gt, "not above the base type's minExclusive"},
		{"maxInclusive", f.maxIncl, b.maxExcl, lt, "not below the base type's maxExclusive"},
		{"minExclusive", f.minExcl, b.minExcl, ge, "below the base type's minExclusive"},
		{"minExclusive", f.minExcl, b.maxIncl, lt, "not below the base type's maxInclusive"},
		{"minExclusive", f.minExcl, b.minIncl, ge, "below the base type's minInclusive"},
		{"minExclusive", f.minExcl, b.maxExcl, lt, "not below the base type's maxExclusive"},
		{"maxExclusive", f.maxExcl, b.maxExcl, le, "above the base type's maxExclusive"},
		{"maxExclusive", f.maxExcl, b.maxIncl, le, "above the base type's maxInclusive"},
		{"maxExclusive", f.maxExcl, b.minIncl, gt, "not above the base type's minInclusive"},
		{"maxExclusive", f.maxExcl, b.minExcl, gt, "not above the base type's minExclusive"},
	}
	for _, r := range rules {
		if r.a == "" || r.b == "" {
			continue
		}
		if c, ok := cmp(r.a, r.b); ok && !r.ok(c) {
			return fail(r.facet, "%s %s is %s %s", r.facet, r.a, r.what, r.b)
		}
	}

	// fixed facets of the base keep their value
	for name, fx := range be.fixed {
		if !fx || nodes[name] == nil {
			continue
		}
		same := true
		switch name {
		case "length":
			same = f.length == b.length
		case "minLength":
			same = f.minLength == b.minLength
		case "maxLength":
			same = f.maxLength == b.maxLength
		case "totalDigits":
			same = f.totalDigits == b.totalDigits
		case "fractionDigits":
			same = f.fractionDigits == b.fractionDigits
		case "whiteSpace":
			same = f.ws == b.ws
		case "minInclusive", "maxInclusive", "minExclusive", "maxExclusive":
			mine := map[string]string{"minInclusive": f.minIncl, "maxInclusive": f.maxIncl, "minExclusive": f.minExcl, "maxExclusive": f.maxExcl}[name]
			theirs := map[string]string{"minInclusive": b.minIncl, "maxInclusive": b.maxIncl, "minExclusive": b.minExcl, "maxExclusive": b.maxExcl}[name]
			c, ok := cmp(mine, theirs)
			same = ok && c == 0
		}
		if !same {
			return fail(name, "facet %s is fixed in the base type and may not change", name)
		}
	}

	// enumeration values must be values of the base type
	if len(f.enum) > 0 {
		for _, c := range enumNodes(nodes) {
			v, _ := c.attr("value")
			if prim == "NOTATION" {
				q, err := c.resolveQName(v)
				if err != nil || !l.notations[q] {
					return d.errf(c, "enumeration value %s does not name a declared notation", strconv.Quote(v))
				}
			}
			if _, why := l.s.checkSimple(base, v, c); why != "" {
				return d.errf(c, "enumeration value %s %s in the base type", strconv.Quote(v), why)
			}
		}
	}
	return nil
}

// enumNodes are the xs:enumeration facets of t's restriction step.
func enumNodes(nodes map[string]*node) []*node {
	n := nodes["enumeration"]
	if n == nil || n.parent == nil {
		return nil
	}
	var out []*node
	for _, c := range n.parent.children {
		if c.name.Space == nsXS && c.name.Local == "enumeration" {
			out = append(out, c)
		}
	}
	return out
}
