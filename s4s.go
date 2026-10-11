// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package xsd

import (
	"regexp"
	"strings"
)

// The XML representation of a schema document is itself constrained: XSD
// 1.0 Part 1 gives a schema for schema documents, which says which
// attributes each element of the XML Schema namespace takes and of what
// type, and in which order its children come. checkS4S applies those
// rules, written here as tables of our own: for every kind of schema
// element (an element's kind depends on where it stands, so a top-level
// xs:element differs from a local one), the attributes it takes with a
// check of their values, and its children as a regular expression over
// their local names. Attributes in other namespaces are allowed anywhere,
// as are any elements inside xs:appinfo and xs:documentation.

// s4sKind describes one kind of element of a schema document.
type s4sKind struct {
	attrs map[string]func(string) bool // unqualified attributes allowed, with their value checks
	kids  *regexp.Regexp               // child local names, each followed by a space; nil: any content
	// child gives the kind of a child of this element by its local name.
	child func(local string) string
}

func (k s4sKind) allows(a string) bool { _, ok := k.attrs[a]; return ok }

// Attribute value checks, after XML Schema's white space collapsing.
var (
	s4sAny    = func(string) bool { return true }
	s4sBool   = func(v string) bool { return v == "true" || v == "false" || v == "1" || v == "0" }
	s4sNCName = isNCName
	s4sQName  = func(v string) bool {
		p, l, ok := strings.Cut(v, ":")
		if !ok {
			return isNCName(v)
		}
		return isNCName(p) && isNCName(l)
	}
	s4sURI    = func(v string) bool { return builtinCheck("anyURI", v, nil) == "" }
	s4sNonNeg = func(v string) bool { return reNonNeg.MatchString(v) }
	s4sAllNNI = func(v string) bool { return v == "unbounded" || reNonNeg.MatchString(v) }
	s4sForm   = s4sEnum("qualified", "unqualified")
	s4sUse    = s4sEnum("prohibited", "optional", "required")
	s4sProc   = s4sEnum("skip", "lax", "strict")
	s4sToken  = func(v string) bool { return true }
	// derivation sets: #all or a list of the given words
	s4sDerivSet       = s4sSet("extension", "restriction")
	s4sBlockSet       = s4sSet("extension", "restriction", "substitution")
	s4sFullDerivSet   = s4sSet("extension", "restriction", "list", "union")
	s4sSimpleDerivSet = s4sSet("list", "union", "restriction")
	s4sQNames         = func(v string) bool {
		for _, f := range strings.Fields(v) {
			if !s4sQName(f) {
				return false
			}
		}
		return true
	}
	s4sNamespaces = func(v string) bool {
		f := strings.Fields(v)
		if len(f) == 1 && (f[0] == "##any" || f[0] == "##other") {
			return true
		}
		for _, x := range f {
			if strings.HasPrefix(x, "##") {
				if x != "##targetNamespace" && x != "##local" {
					return false
				}
				continue
			}
			if !s4sURI(x) {
				return false
			}
		}
		return true
	}
	s4sMax01 = s4sAllOccurs
	s4sMax1  = func(v string) bool {
		return reNonNeg.MatchString(v) && strings.TrimLeft(strings.TrimPrefix(v, "+"), "0") == "1"
	}
)

var reNonNeg = regexp.MustCompile(`^\+?[0-9]+$`)

func s4sEnum(vals ...string) func(string) bool {
	return func(v string) bool {
		for _, x := range vals {
			if v == x {
				return true
			}
		}
		return false
	}
}

func s4sSet(words ...string) func(string) bool {
	ok := s4sEnum(words...)
	return func(v string) bool {
		if v == "#all" {
			return true
		}
		for _, f := range strings.Fields(v) {
			if !ok(f) {
				return false
			}
		}
		return true
	}
}

// s4sAllOccurs checks minOccurs on xs:all and elements in it: 0 or 1.
func s4sAllOccurs(v string) bool {
	if !reNonNeg.MatchString(v) {
		return false
	}
	x := strings.TrimLeft(strings.TrimPrefix(v, "+"), "0")
	return x == "" || x == "1"
}

func s4sAttrs(pairs ...any) map[string]func(string) bool {
	m := map[string]func(string) bool{"id": s4sNCName}
	for i := 0; i < len(pairs); i += 2 {
		m[pairs[i].(string)] = pairs[i+1].(func(string) bool)
	}
	return m
}

func s4sRe(s string) *regexp.Regexp { return regexp.MustCompile("^(?:" + s + ")$") }

const (
	s4sAnn      = `(annotation )?`
	s4sFacets   = `(minExclusive |minInclusive |maxExclusive |maxInclusive |totalDigits |fractionDigits |length |minLength |maxLength |enumeration |whiteSpace |pattern )*`
	s4sAttrDecl = `(attribute |attributeGroup )*(anyAttribute )?`
	s4sTypeDef  = `(simpleType |complexType )`
	s4sParticle = `(element |group |choice |sequence |any )`
)

// child-kind helpers
func s4sMap(m map[string]string) func(string) string {
	return func(l string) string { return m[l] }
}

var (
	attrDeclKids = map[string]string{"annotation": "annotation", "attribute": "localAttribute", "attributeGroup": "attrGroupRef", "anyAttribute": "anyAttribute"}
	facetNames   = []string{"minExclusive", "minInclusive", "maxExclusive", "maxInclusive", "totalDigits", "fractionDigits", "length", "minLength", "maxLength", "enumeration", "whiteSpace", "pattern"}
)

func withFacets(m map[string]string) map[string]string {
	for _, f := range facetNames {
		m[f] = "facet:" + f
	}
	return m
}

func merge(ms ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range ms {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

var s4sKinds map[string]s4sKind

func init() {
	ann := map[string]string{"annotation": "annotation"}
	particleKids := map[string]string{"annotation": "annotation", "element": "localElement", "group": "groupRef", "choice": "choice", "sequence": "sequence", "any": "any"}
	modelKids := map[string]string{"group": "groupRef", "all": "all", "choice": "choice", "sequence": "sequence"}
	occ := []any{"minOccurs", s4sNonNeg, "maxOccurs", s4sAllNNI}
	k := map[string]s4sKind{}
	k["schema"] = s4sKind{
		attrs: s4sAttrs("attributeFormDefault", s4sForm, "elementFormDefault", s4sForm, "blockDefault", s4sBlockSet,
			"finalDefault", s4sFullDerivSet, "targetNamespace", s4sURI, "version", s4sToken),
		kids: s4sRe(`(include |import |redefine |annotation )*((simpleType |complexType |group |attributeGroup |element |attribute |notation )(annotation )*)*`),
		child: s4sMap(map[string]string{"include": "include", "import": "import", "redefine": "redefine", "annotation": "annotation",
			"simpleType": "topSimpleType", "complexType": "topComplexType", "group": "namedGroup", "attributeGroup": "namedAttrGroup",
			"element": "topElement", "attribute": "topAttribute", "notation": "notation"}),
	}
	k["annotation"] = s4sKind{attrs: s4sAttrs(), kids: s4sRe(`(appinfo |documentation )*`),
		child: s4sMap(map[string]string{"appinfo": "appinfo", "documentation": "documentation"})}
	k["appinfo"] = s4sKind{attrs: map[string]func(string) bool{"source": s4sURI}}
	k["documentation"] = s4sKind{attrs: map[string]func(string) bool{"source": s4sURI}}
	k["include"] = s4sKind{attrs: s4sAttrs("schemaLocation", s4sURI), kids: s4sRe(s4sAnn), child: s4sMap(ann)}
	k["import"] = s4sKind{attrs: s4sAttrs("namespace", s4sURI, "schemaLocation", s4sURI), kids: s4sRe(s4sAnn), child: s4sMap(ann)}
	k["redefine"] = s4sKind{attrs: s4sAttrs("schemaLocation", s4sURI), kids: s4sRe(`(annotation |simpleType |complexType |group |attributeGroup )*`),
		child: s4sMap(map[string]string{"annotation": "annotation", "simpleType": "topSimpleType", "complexType": "topComplexType",
			"group": "namedGroup", "attributeGroup": "namedAttrGroup"})}
	k["notation"] = s4sKind{attrs: s4sAttrs("name", s4sNCName, "public", s4sToken, "system", s4sURI), kids: s4sRe(s4sAnn), child: s4sMap(ann)}

	elemKids := map[string]string{"annotation": "annotation", "simpleType": "localSimpleType", "complexType": "localComplexType",
		"unique": "unique", "key": "key", "keyref": "keyref"}
	elemRe := s4sRe(s4sAnn + s4sTypeDef + `?(unique |key |keyref )*`)
	k["topElement"] = s4sKind{attrs: s4sAttrs("name", s4sNCName, "type", s4sQName, "substitutionGroup", s4sQName,
		"default", s4sAny, "fixed", s4sAny, "nillable", s4sBool, "abstract", s4sBool, "final", s4sDerivSet, "block", s4sBlockSet),
		kids: elemRe, child: s4sMap(elemKids)}
	k["localElement"] = s4sKind{attrs: s4sAttrs(append([]any{"name", s4sNCName, "ref", s4sQName, "type", s4sQName,
		"default", s4sAny, "fixed", s4sAny, "nillable", s4sBool, "block", s4sBlockSet, "form", s4sForm}, occ...)...),
		kids: elemRe, child: s4sMap(elemKids)}
	k["allElement"] = s4sKind{attrs: s4sAttrs("name", s4sNCName, "ref", s4sQName, "type", s4sQName,
		"default", s4sAny, "fixed", s4sAny, "nillable", s4sBool, "block", s4sBlockSet, "form", s4sForm,
		"minOccurs", s4sAllOccurs, "maxOccurs", s4sMax01),
		kids: elemRe, child: s4sMap(elemKids)}

	k["topAttribute"] = s4sKind{attrs: s4sAttrs("name", s4sNCName, "type", s4sQName, "default", s4sAny, "fixed", s4sAny),
		kids: s4sRe(s4sAnn + `(simpleType )?`), child: s4sMap(map[string]string{"annotation": "annotation", "simpleType": "localSimpleType"})}
	k["localAttribute"] = s4sKind{attrs: s4sAttrs("name", s4sNCName, "ref", s4sQName, "type", s4sQName, "use", s4sUse,
		"default", s4sAny, "fixed", s4sAny, "form", s4sForm),
		kids: s4sRe(s4sAnn + `(simpleType )?`), child: s4sMap(map[string]string{"annotation": "annotation", "simpleType": "localSimpleType"})}

	ctKids := merge(ann, modelKids, attrDeclKids, map[string]string{"simpleContent": "simpleContent", "complexContent": "complexContent"})
	ctRe := s4sRe(s4sAnn + `((simpleContent |complexContent )|((group |all |choice |sequence )?` + s4sAttrDecl + `))`)
	k["topComplexType"] = s4sKind{attrs: s4sAttrs("name", s4sNCName, "mixed", s4sBool, "abstract", s4sBool, "final", s4sDerivSet, "block", s4sDerivSet),
		kids: ctRe, child: s4sMap(ctKids)}
	k["localComplexType"] = s4sKind{attrs: s4sAttrs("mixed", s4sBool), kids: ctRe, child: s4sMap(ctKids)}
	k["complexContent"] = s4sKind{attrs: s4sAttrs("mixed", s4sBool), kids: s4sRe(s4sAnn + `(restriction |extension )`),
		child: s4sMap(map[string]string{"annotation": "annotation", "restriction": "ccDerivation", "extension": "ccDerivation"})}
	k["ccDerivation"] = s4sKind{attrs: s4sAttrs("base", s4sQName),
		kids:  s4sRe(s4sAnn + `(group |all |choice |sequence )?` + s4sAttrDecl),
		child: s4sMap(merge(ann, modelKids, attrDeclKids))}
	k["simpleContent"] = s4sKind{attrs: s4sAttrs(), kids: s4sRe(s4sAnn + `(restriction |extension )`),
		child: s4sMap(map[string]string{"annotation": "annotation", "restriction": "scRestriction", "extension": "scExtension"})}
	k["scRestriction"] = s4sKind{attrs: s4sAttrs("base", s4sQName),
		kids:  s4sRe(s4sAnn + `(simpleType )?` + s4sFacets + s4sAttrDecl),
		child: s4sMap(withFacets(merge(ann, attrDeclKids, map[string]string{"simpleType": "localSimpleType"})))}
	k["scExtension"] = s4sKind{attrs: s4sAttrs("base", s4sQName), kids: s4sRe(s4sAnn + s4sAttrDecl), child: s4sMap(merge(ann, attrDeclKids))}

	k["namedGroup"] = s4sKind{attrs: s4sAttrs("name", s4sNCName), kids: s4sRe(s4sAnn + `(all |choice |sequence )`),
		child: s4sMap(map[string]string{"annotation": "annotation", "all": "groupAll", "choice": "groupChoice", "sequence": "groupSequence"})}
	k["groupRef"] = s4sKind{attrs: s4sAttrs(append([]any{"ref", s4sQName}, occ...)...), kids: s4sRe(s4sAnn), child: s4sMap(ann)}
	seqRe := s4sRe(s4sAnn + s4sParticle + `*`)
	k["sequence"] = s4sKind{attrs: s4sAttrs(occ...), kids: seqRe, child: s4sMap(particleKids)}
	k["choice"] = k["sequence"]
	k["groupSequence"] = s4sKind{attrs: s4sAttrs(), kids: seqRe, child: s4sMap(particleKids)}
	k["groupChoice"] = k["groupSequence"]
	allKids := map[string]string{"annotation": "annotation", "element": "allElement"}
	k["all"] = s4sKind{attrs: s4sAttrs("minOccurs", s4sAllOccurs, "maxOccurs", s4sMax1), kids: s4sRe(s4sAnn + `(element )*`), child: s4sMap(allKids)}
	k["groupAll"] = s4sKind{attrs: s4sAttrs(), kids: s4sRe(s4sAnn + `(element )*`), child: s4sMap(allKids)}
	k["any"] = s4sKind{attrs: s4sAttrs(append([]any{"namespace", s4sNamespaces, "processContents", s4sProc}, occ...)...), kids: s4sRe(s4sAnn), child: s4sMap(ann)}
	k["anyAttribute"] = s4sKind{attrs: s4sAttrs("namespace", s4sNamespaces, "processContents", s4sProc), kids: s4sRe(s4sAnn), child: s4sMap(ann)}

	k["namedAttrGroup"] = s4sKind{attrs: s4sAttrs("name", s4sNCName), kids: s4sRe(s4sAnn + s4sAttrDecl), child: s4sMap(merge(ann, attrDeclKids))}
	k["attrGroupRef"] = s4sKind{attrs: s4sAttrs("ref", s4sQName), kids: s4sRe(s4sAnn), child: s4sMap(ann)}

	stKids := map[string]string{"annotation": "annotation", "restriction": "stRestriction", "list": "list", "union": "union"}
	stRe := s4sRe(s4sAnn + `(restriction |list |union )`)
	k["topSimpleType"] = s4sKind{attrs: s4sAttrs("name", s4sNCName, "final", s4sSimpleDerivSet), kids: stRe, child: s4sMap(stKids)}
	k["localSimpleType"] = s4sKind{attrs: s4sAttrs(), kids: stRe, child: s4sMap(stKids)}
	k["stRestriction"] = s4sKind{attrs: s4sAttrs("base", s4sQName), kids: s4sRe(s4sAnn + `(simpleType )?` + s4sFacets),
		child: s4sMap(withFacets(map[string]string{"annotation": "annotation", "simpleType": "localSimpleType"}))}
	k["list"] = s4sKind{attrs: s4sAttrs("itemType", s4sQName), kids: s4sRe(s4sAnn + `(simpleType )?`),
		child: s4sMap(map[string]string{"annotation": "annotation", "simpleType": "localSimpleType"})}
	k["union"] = s4sKind{attrs: s4sAttrs("memberTypes", s4sQNames), kids: s4sRe(s4sAnn + `(simpleType )*`),
		child: s4sMap(map[string]string{"annotation": "annotation", "simpleType": "localSimpleType"})}
	for _, f := range facetNames {
		a := s4sAttrs("value", s4sAny, "fixed", s4sBool)
		if f == "enumeration" || f == "pattern" {
			delete(a, "fixed")
		}
		k["facet:"+f] = s4sKind{attrs: a, kids: s4sRe(s4sAnn), child: s4sMap(ann)}
	}

	idcKids := map[string]string{"annotation": "annotation", "selector": "selector", "field": "field"}
	idcRe := s4sRe(s4sAnn + `selector (field )+`)
	k["unique"] = s4sKind{attrs: s4sAttrs("name", s4sNCName), kids: idcRe, child: s4sMap(idcKids)}
	k["key"] = k["unique"]
	k["keyref"] = s4sKind{attrs: s4sAttrs("name", s4sNCName, "refer", s4sQName), kids: idcRe, child: s4sMap(idcKids)}
	k["selector"] = s4sKind{attrs: s4sAttrs("xpath", s4sAny), kids: s4sRe(s4sAnn), child: s4sMap(ann)}
	k["field"] = k["selector"]
	s4sKinds = k
}

// s4sRequired lists the attributes a kind must have.
var s4sRequired = map[string][]string{
	"include": {"schemaLocation"}, "redefine": {"schemaLocation"}, "notation": {"name"},
	"topElement": {"name"}, "topAttribute": {"name"}, "topComplexType": {"name"}, "topSimpleType": {"name"},
	"namedGroup": {"name"}, "namedAttrGroup": {"name"}, "groupRef": {"ref"}, "attrGroupRef": {"ref"},
	"ccDerivation": {"base"}, "scRestriction": {"base"}, "scExtension": {"base"},
	"unique": {"name"}, "key": {"name"}, "keyref": {"name", "refer"}, "selector": {"xpath"}, "field": {"xpath"},
}

// checkS4S checks a schema document's XML representation.
func checkS4S(d *schemaDoc) error {
	ids := map[string]bool{}
	return s4sNode(d, d.root, "schema", ids)
}

func s4sNode(d *schemaDoc, n *node, kind string, ids map[string]bool) error {
	k, ok := s4sKinds[kind]
	if !ok {
		return d.errf(n, "xs:%s is not allowed here", n.name.Local)
	}
	for _, a := range n.attrs {
		switch a.name.Space {
		case "":
		case nsXS:
			return d.errf(n, "attribute %s in the XML Schema namespace on xs:%s", a.name.Local, n.name.Local)
		case nsXML:
			if a.name.Local == "lang" && !reLanguage.MatchString(collapse(a.value)) {
				return d.errf(n, "xml:lang=%q is not a language tag", a.value)
			}
			continue
		default:
			continue // attributes of other namespaces are allowed
		}
		check, ok := k.attrs[a.name.Local]
		if !ok {
			return d.errf(n, "attribute %s is not allowed on xs:%s here", a.name.Local, n.name.Local)
		}
		v := collapse(a.value)
		if !check(v) {
			return d.errf(n, "attribute %s=%q of xs:%s is not a valid value", a.name.Local, a.value, n.name.Local)
		}
		if a.name.Local == "id" {
			if ids[v] {
				return d.errf(n, "id %q is used twice in the schema document", v)
			}
			ids[v] = true
		}
	}
	for _, r := range s4sRequired[kind] {
		if _, ok := n.attr(r); !ok {
			return d.errf(n, "xs:%s needs the attribute %s", n.name.Local, r)
		}
	}
	if err := s4sForms(d, n, kind); err != nil {
		return err
	}
	if k.kids == nil {
		return nil // appinfo, documentation: any content
	}
	if strings.TrimSpace(n.text.String()) != "" {
		return d.errf(n, "xs:%s may not contain character data", n.name.Local)
	}
	var b strings.Builder
	for _, c := range n.children {
		if c.name.Space != nsXS {
			return d.errf(c, "element %s is not allowed in xs:%s", c.name, n.name.Local)
		}
		b.WriteString(c.name.Local)
		b.WriteByte(' ')
	}
	if !k.kids.MatchString(b.String()) {
		return d.errf(n, "the children of xs:%s (%s) are not allowed in that order or number", n.name.Local, strings.TrimSpace(b.String()))
	}
	for _, c := range n.children {
		ck := k.child(c.name.Local)
		if ck == "" {
			return d.errf(c, "xs:%s is not allowed in xs:%s", c.name.Local, n.name.Local)
		}
		if err := s4sNode(d, c, ck, ids); err != nil {
			return err
		}
	}
	return nil
}

// s4sForms checks the representation rules that depend on which
// attributes are present together (src-element, src-attribute and the
// like).
func s4sForms(d *schemaDoc, n *node, kind string) error {
	has := func(a string) bool { _, ok := n.attr(a); return ok }
	switch kind {
	case "localElement", "allElement":
		switch {
		case has("ref") && has("name"):
			return d.errf(n, "xs:element has both name and ref")
		case !has("ref") && !has("name"):
			return d.errf(n, "local xs:element needs a name or ref")
		case has("ref"):
			for _, a := range n.attrs {
				if a.name.Space != "" {
					continue
				}
				switch a.name.Local {
				case "ref", "id", "minOccurs", "maxOccurs":
				default:
					return d.errf(n, "xs:element with ref may not have %s", a.name.Local)
				}
			}
			for _, c := range n.children {
				if c.name.Local != "annotation" {
					return d.errf(n, "xs:element with ref may not have xs:%s", c.name.Local)
				}
			}
		}
		fallthrough
	case "topElement":
		if has("default") && has("fixed") {
			return d.errf(n, "xs:element has both default and fixed")
		}
		if has("type") {
			for _, c := range n.children {
				if c.name.Local == "simpleType" || c.name.Local == "complexType" {
					return d.errf(n, "xs:element has both a type attribute and an inline type")
				}
			}
		}
	case "localAttribute":
		switch {
		case has("ref") && has("name"):
			return d.errf(n, "xs:attribute has both name and ref")
		case !has("ref") && !has("name"):
			return d.errf(n, "xs:attribute needs a name or ref")
		case has("ref"):
			for _, a := range []string{"type", "form"} {
				if has(a) {
					return d.errf(n, "xs:attribute with ref may not have %s", a)
				}
			}
			for _, c := range n.children {
				if c.name.Local != "annotation" {
					return d.errf(n, "xs:attribute with ref may not have xs:%s", c.name.Local)
				}
			}
		}
		if u, _ := n.attr("use"); has("default") && collapse(u) != "optional" && has("use") {
			return d.errf(n, "xs:attribute with a default must be optional")
		}
		fallthrough
	case "topAttribute":
		if has("default") && has("fixed") {
			return d.errf(n, "xs:attribute has both default and fixed")
		}
		if has("type") {
			for _, c := range n.children {
				if c.name.Local == "simpleType" {
					return d.errf(n, "xs:attribute has both a type attribute and an inline type")
				}
			}
		}
		if v, ok := n.attr("name"); ok && collapse(v) == "xmlns" {
			return d.errf(n, "an attribute may not be named xmlns")
		}
	case "notation":
		if !has("public") && !has("system") {
			return d.errf(n, "xs:notation needs public or system")
		}
	case "stRestriction":
		hasInline := len(n.children) > 0 && (n.children[0].name.Local == "simpleType" || len(n.children) > 1 && n.children[0].name.Local == "annotation" && n.children[1].name.Local == "simpleType")
		if has("base") == hasInline {
			return d.errf(n, "xs:restriction needs either a base or an inline simpleType")
		}
	case "list":
		inline := false
		for _, c := range n.children {
			inline = inline || c.name.Local == "simpleType"
		}
		if has("itemType") == inline {
			return d.errf(n, "xs:list needs either itemType or an inline simpleType")
		}
	case "union":
		inline := false
		for _, c := range n.children {
			inline = inline || c.name.Local == "simpleType"
		}
		if v, _ := n.attr("memberTypes"); strings.TrimSpace(v) == "" && !inline {
			return d.errf(n, "xs:union needs member types")
		}
	case "schema", "import":
		a := "targetNamespace"
		if kind == "import" {
			a = "namespace"
		}
		if v, ok := n.attr(a); ok && v == "" {
			return d.errf(n, "%s may not be empty", a)
		}
	}
	return nil
}
