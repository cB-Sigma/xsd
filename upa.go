package xsd

// Unique Particle Attribution (XSD 1.0 Part 1, 3.8.6): in a content model,
// every element of a document must be attributable to one particle without
// looking at what follows it. We check this when the schema loads, on the
// position automaton of each complex type's content model (Glushkov's
// construction): one position per element or wildcard particle, with the
// set of positions that may come first and, for each position, the set
// that may follow it. The model is ambiguous when one of these sets holds
// two positions of different particles that can match the same element
// name.
//
// Occurrence bounds are counted exactly by repeating a particle's positions
// (a{2,3} becomes a a a?), so a{2} followed by an optional a is accepted.
// Copies of one particle never conflict with each other. Bounds above
// upaMaxCount are shortened to upaMaxCount (a{500} as a{16}, a{2,500} as
// a{2,}), which keeps the same ambiguities for the models schemas use, and a
// model that still expands to more than upaMaxPositions positions or
// upaMaxLinks follow entries is not checked (it is accepted).

const (
	upaMaxCount     = 16
	upaMaxPositions = 20000
	upaMaxLinks     = 4000000
)

type upaBuild struct {
	origin []*particle // the particle each position is a copy of
	follow [][]int
	links  int
	tooBig bool
}

// glushkov is a subexpression: whether it matches the empty sequence and
// its first and last positions.
type glushkov struct {
	nullable    bool
	first, last []int
}

func (b *upaBuild) leaf(p *particle) glushkov {
	if len(b.origin) >= upaMaxPositions {
		b.tooBig = true
		return glushkov{nullable: true}
	}
	i := len(b.origin)
	b.origin = append(b.origin, p)
	b.follow = append(b.follow, nil)
	return glushkov{first: []int{i}, last: []int{i}}
}

func (b *upaBuild) link(from, to []int) {
	b.links += len(from) * len(to)
	if b.links > upaMaxLinks {
		b.tooBig = true
		return
	}
	for _, f := range from {
		b.follow[f] = append(b.follow[f], to...)
	}
}

func (b *upaBuild) concat(x, y glushkov) glushkov {
	b.link(x.last, y.first)
	out := glushkov{nullable: x.nullable && y.nullable}
	out.first = append([]int(nil), x.first...)
	if x.nullable {
		out.first = append(out.first, y.first...)
	}
	out.last = append([]int(nil), y.last...)
	if y.nullable {
		out.last = append(out.last, x.last...)
	}
	return out
}

func optional(x glushkov) glushkov {
	x.nullable = true
	return x
}

// particle expands p with its occurrence bounds.
func (b *upaBuild) particle(p *particle) glushkov {
	lo, hi := p.min, p.max
	switch {
	case hi < 0:
		lo = min(lo, upaMaxCount)
	case lo == hi:
		lo = min(lo, upaMaxCount)
		hi = lo
	case hi > upaMaxCount:
		lo, hi = min(lo, upaMaxCount), -1
	}
	if hi == 0 {
		return glushkov{nullable: true}
	}
	res := glushkov{nullable: true}
	for range lo {
		if b.tooBig {
			return res
		}
		res = b.concat(res, b.term(p))
	}
	switch {
	case hi < 0:
		// one more copy, repeatable: x*
		x := b.term(p)
		b.link(x.last, x.first)
		res = b.concat(res, optional(x))
	default:
		for range hi - lo {
			if b.tooBig {
				return res
			}
			res = b.concat(res, optional(b.term(p)))
		}
	}
	return res
}

// term expands p once, without its occurrence bounds.
func (b *upaBuild) term(p *particle) glushkov {
	switch p.kind {
	case pElem, pAny:
		return b.leaf(p)
	case pSeq:
		res := glushkov{nullable: true}
		for _, it := range p.items {
			res = b.concat(res, b.particle(it))
		}
		return res
	case pChoice:
		res := glushkov{}
		for _, it := range p.items {
			x := b.particle(it)
			res.nullable = res.nullable || x.nullable
			res.first = append(res.first, x.first...)
			res.last = append(res.last, x.last...)
		}
		return res
	case pAll:
		// any order: each item may be followed by any other
		parts := make([]glushkov, len(p.items))
		res := glushkov{nullable: true}
		for i, it := range p.items {
			parts[i] = b.particle(it)
			res.nullable = res.nullable && parts[i].nullable
			res.first = append(res.first, parts[i].first...)
			res.last = append(res.last, parts[i].last...)
		}
		for i := range parts {
			for j := range parts {
				if i != j {
					b.link(parts[i].last, parts[j].first)
				}
			}
		}
		return res
	}
	return glushkov{nullable: true}
}

// elementNames are the names an element particle matches.
func elementNames(p *particle) []qname {
	var out []qname
	if !p.elem.abstract {
		out = append(out, p.elem.name)
	}
	for _, m := range p.elem.memberSeq {
		if !m.abstract {
			out = append(out, m.name)
		}
	}
	return out
}

// wildcardsOverlap reports whether some namespace is admitted by both.
func wildcardsOverlap(a, b *wildcard) bool {
	switch {
	case a.any || b.any:
		return true
	case a.not != nil && b.not != nil:
		return true // both admit every namespace but a few
	case a.not != nil:
		a, b = b, a
	}
	// a is a list
	for ns := range a.list {
		if b.allows(ns) {
			return true
		}
	}
	return false
}

// ambiguity returns a description of two particles that compete in a set
// of positions, or "".
func ambiguity(origin []*particle, set []int) string {
	byName := map[qname]*particle{}
	var wild []*particle
	seen := map[*particle]bool{}
	for _, i := range set {
		p := origin[i]
		if seen[p] {
			continue
		}
		seen[p] = true
		if p.kind == pAny {
			wild = append(wild, p)
			continue
		}
		for _, q := range elementNames(p) {
			if o := byName[q]; o != nil && o != p {
				return "element " + q.String() + " matches two element particles"
			}
			byName[q] = p
		}
	}
	for i, w := range wild {
		for q := range byName {
			if w.wc.allows(q.Space) {
				return "element " + q.String() + " matches an element particle and a wildcard"
			}
		}
		for _, v := range wild[i+1:] {
			if wildcardsOverlap(w.wc, v.wc) {
				return "two wildcards admit the same elements"
			}
		}
	}
	return ""
}

// checkUPA refuses a complex type whose content model is ambiguous.
func checkUPA(t *xtype) error {
	if t.content == nil || t.raw == nil {
		return nil
	}
	b := &upaBuild{}
	g := b.particle(t.content)
	if b.tooBig {
		return nil
	}
	sets := append([][]int{g.first}, b.follow...)
	for _, set := range sets {
		if why := ambiguity(b.origin, set); why != "" {
			return t.doc.errf(t.raw, "the content model of %s is ambiguous (Unique Particle Attribution): %s at the same point", t.label(), why)
		}
	}
	return nil
}
