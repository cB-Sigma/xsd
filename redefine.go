// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package xsd

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
)

// xs:redefine (XSD 1.0 Part 1, 4.2.2) includes a schema document and
// replaces some of its simple types, complex types, model groups and
// attribute groups by new definitions of the same name. The replacement
// is pervasive: every reference to the name, in the redefined document
// too, reaches the new definition. Inside a new definition the name can
// still reach the old one: a type's base must be the type being
// redefined, and a group or attribute group may refer to itself once,
// which means the old group. A group or attribute group that does not
// refer to itself must instead restrict the old one.
//
// Here the old component moves to a hidden name and the new one takes
// its place; references written inside the new definition that name the
// redefined component in the place the recommendation provides (a base,
// a group or attribute group reference) are pointed at the hidden name.

// redef is one redefined component.
type redef struct {
	kind     string // simpleType, complexType, group, attributeGroup
	name     qname  // the redefined name
	hidden   qname  // where the old component now is
	root     *node  // the new definition
	d        *schemaDoc
	selfRefs []*node // references in the new definition to the old one
	newType  *xtype
	newGroup *groupDef
	newAttrG *attrGroupDef
}

func (l *loader) exists(p string) bool {
	if _, ok := l.fetched[p]; ok {
		return true
	}
	_, err := fs.Stat(l.fsys, p)
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}

func (l *loader) redefine(d *schemaDoc, c *node) error {
	loc, _ := c.attr("schemaLocation")
	var kids []*node
	for _, k := range c.children {
		if k.name.Local != "annotation" {
			kids = append(kids, k)
		}
	}
	target, remote, err := l.locate(d, loc)
	if err != nil {
		return d.errf(c, "%v", err)
	}
	if remote && !l.fetchable(c, d.path, target) || !remote && !l.exists(target) {
		if len(kids) > 0 {
			return d.errf(c, "xs:redefine of %s: no such schema document, yet it redefines components", loc)
		}
		l.s.warnings = append(l.s.warnings, Issue{Line: c.line, Path: d.path, Kind: IssueOther,
			Msg: fmt.Sprintf("xs:redefine of %s: no such schema document (ignored)", loc)})
		return nil
	}
	if _, err := l.load(target, d.tns, true); err != nil {
		return err
	}
	for _, k := range kids {
		name, _ := k.attr("name")
		q := qname{d.tns, collapse(name)}
		l.redefs++
		hidden := qname{q.Space, q.Local + "\x00redefined" + strconv.Itoa(l.redefs)}
		switch k.name.Local {
		case "simpleType", "complexType":
			old := l.s.types[q]
			if old == nil || old.complex != (k.name.Local == "complexType") {
				return d.errf(k, "xs:redefine: %s %s is not in the redefined schema", k.name.Local, q.Local)
			}
			delete(l.s.types, q)
			l.s.types[hidden] = old
		case "group":
			old := l.s.groups[q]
			if old == nil {
				return d.errf(k, "xs:redefine: group %s is not in the redefined schema", q.Local)
			}
			delete(l.s.groups, q)
			l.s.groups[hidden] = old
		case "attributeGroup":
			old := l.s.attrGroups[q]
			if old == nil {
				return d.errf(k, "xs:redefine: attribute group %s is not in the redefined schema", q.Local)
			}
			delete(l.s.attrGroups, q)
			l.s.attrGroups[hidden] = old
		}
		if err := l.register(d, k); err != nil {
			return err
		}
		if l.redef == nil {
			l.redef = map[*node]*redef{}
		}
		l.redef[k] = &redef{kind: k.name.Local, name: q, hidden: hidden, root: k, d: d,
			newType: l.s.types[q], newGroup: l.s.groups[q], newAttrG: l.s.attrGroups[q]}
	}
	return nil
}

// redefined points a reference at the old component when it is written
// inside the new definition of that component, in the place the
// recommendation gives it.
func (l *loader) redefined(n *node, attrName string, q qname) qname {
	if len(l.redef) == 0 {
		return q
	}
	for p := n; p != nil; p = p.parent {
		r := l.redef[p]
		if r == nil || r.name != q {
			continue
		}
		switch r.kind {
		case "simpleType", "complexType":
			if attrName != "base" {
				return q
			}
		case "group", "attributeGroup":
			if attrName != "ref" || n.name.Local != r.kind {
				return q
			}
		}
		r.selfRefs = append(r.selfRefs, n)
		return r.hidden
	}
	return q
}

// checkRedefines applies the rules a new definition has to follow.
func (l *loader) checkRedefines() error {
	for _, r := range l.redef {
		d, n := r.d, r.root
		switch r.kind {
		case "simpleType", "complexType":
			t := r.newType
			if t == nil || t.base == nil || t.base != l.s.types[r.hidden] {
				return d.errf(n, "the redefinition of %s must derive from the type it redefines", r.name.Local)
			}
		case "group":
			if len(r.selfRefs) > 1 {
				return d.errf(n, "the redefinition of group %s refers to itself more than once", r.name.Local)
			}
			if len(r.selfRefs) == 1 {
				ref := r.selfRefs[0]
				for _, a := range []string{"minOccurs", "maxOccurs"} {
					if v, ok := ref.attr(a); ok && collapse(v) != "1" {
						return d.errf(ref, "in the redefinition of group %s the reference to itself must occur exactly once", r.name.Local)
					}
				}
				continue
			}
			nw, old := r.newGroup, l.s.groups[r.hidden]
			if nw == nil || old == nil || nw.model == nil || old.model == nil {
				continue
			}
			rc, bc := normalise(nw.model), normalise(old.model)
			switch {
			case rc == nil:
				if bc != nil && !bc.emptiable() {
					return d.errf(n, "the redefinition of group %s does not restrict the group it redefines", r.name.Local)
				}
			case bc == nil:
				return d.errf(n, "the redefinition of group %s does not restrict the group it redefines", r.name.Local)
			default:
				if why := restricts(rc, bc); why != "" {
					return d.errf(n, "the redefinition of group %s does not restrict the group it redefines: %s", r.name.Local, why)
				}
			}
		case "attributeGroup":
			if len(r.selfRefs) > 1 {
				return d.errf(n, "the redefinition of attribute group %s refers to itself more than once", r.name.Local)
			}
			if len(r.selfRefs) == 1 {
				continue
			}
			nw, old := r.newAttrG, l.s.attrGroups[r.hidden]
			if nw == nil || old == nil {
				continue
			}
			if why := attrsRestrict(nw.uses, nw.wc, old.uses, old.wc); why != "" {
				return d.errf(n, "the redefinition of attribute group %s does not restrict the group it redefines: %s", r.name.Local, why)
			}
		}
	}
	return nil
}

func firstFixed(u *attrUse) *string {
	if u.fixed != nil {
		return u.fixed
	}
	return u.decl.fixed
}

// attrsRestrict reports why attribute uses and a wildcard do not restrict
// others ("" if they do).
func attrsRestrict(uses []*attrUse, wc *wildcard, buses []*attrUse, bwc *wildcard) string {
	base := map[qname]*attrUse{}
	for _, u := range buses {
		if !u.prohibited {
			base[u.decl.name] = u
		}
	}
	have := map[qname]bool{}
	for _, u := range uses {
		if u.prohibited {
			continue
		}
		have[u.decl.name] = true
		b := base[u.decl.name]
		if b == nil {
			if bwc == nil || !bwc.allows(u.decl.name.Space) {
				return "attribute " + u.decl.name.Local + " is not allowed by the original"
			}
			continue
		}
		if b.required && !u.required {
			return "attribute " + u.decl.name.Local + " is required in the original"
		}
		if u.decl.typ != nil && b.decl.typ != nil && !derivesOK(u.decl.typ, b.decl.typ, derivSet{"extension": true}) {
			return "the type of attribute " + u.decl.name.Local + " does not restrict the original's"
		}
		if bf, rf := firstFixed(b), firstFixed(u); bf != nil && (rf == nil || *rf != *bf) {
			return "attribute " + u.decl.name.Local + " must keep the original's fixed value"
		}
	}
	for _, b := range buses {
		if b.required && !have[b.decl.name] {
			return "required attribute " + b.decl.name.Local + " is left out"
		}
	}
	if wc != nil && (bwc == nil || !wcSubset(wc, bwc)) {
		return "the attribute wildcard is wider than the original's"
	}
	return ""
}
