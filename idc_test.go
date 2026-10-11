package xsd

import (
	"os"
	"strings"
	"testing"
)

func loadShop(t *testing.T) *Schema {
	t.Helper()
	s, err := Load(os.DirFS("testdata/idc"), "shop.xsd")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const validShop = `<aisles xmlns="urn:sigma:test:shop"><shop version="1">
<shelf aisle="A"><label>Tea</label><item sku="TE001" size="S"/><item sku="TE001" size="L" uuid="u1"/></shelf>
<shelf aisle="B"><label>Coffee</label><item sku="CO001" size="M"/></shelf>
<order aisle="B"><line sku="CO001" size="M"/><line sku="TE001" size="L"/></order>
</shop></aisles>`

// unique, key and keyref with the XPath subset: multi-field keys, ".//"
// selectors, alternatives, element fields, and a keyref resolved against
// the key table a descendant element supplies.
// Covers: SIG-IPE-2395
func TestIdentityConstraints(t *testing.T) {
	s := loadShop(t)
	mutate(t, s, validShop, []mutation{
		{"valid", "", "", ""},
		{"key duplicate", `size="S"`, `size="L"`, "item duplicates TE001/L (line 2) in shop: itemKey must be unique"},
		{"key field missing", ` sku="TE001" size="S"`, ` size="S"`, "item lacks @sku and @size, required by key itemKey"},
		{"keyref dangling", `<line sku="TE001" size="L"/>`, `<line sku="TE001" size="XL"/>`, "line refers to TE001/XL which is not defined (lineRef)"},
		{"unique on element field", `<label>Coffee</label>`, `<label>Tea</label>`, "shelf duplicates Tea"},
		// xs:string keeps spaces, so " Tea " is another value than "Tea"
		{"unique compares string values", `<label>Coffee</label>`, `<label> Tea </label>`, ""},
		// the value space of xs:decimal: 1.0 and 1 are one value
		{"unique compares decimal values", `<shelf aisle="B">`, `<shelf aisle="C" code="1.0"><item sku="CO002"/></shelf><shelf aisle="B" code="1">`, "shelf duplicates 1 (line 3)"},
		{"decimal zeros", `<shelf aisle="B">`, `<shelf aisle="C" code="1.5"><item sku="CO002"/></shelf><shelf aisle="B" code="01.50">`, "shelf duplicates 01.50"},
		{"distinct decimals", `<shelf aisle="B">`, `<shelf aisle="C" code="1.5"><item sku="CO002"/></shelf><shelf aisle="B" code="1.25">`, ""},
		// defaults count: an absent size is M, an empty label is Misc
		{"key field from attribute default", `<item sku="CO001" size="M"/>`, `<item sku="CO001"/>`, ""},
		{"duplicate through attribute default", `<item sku="CO001" size="M"/>`, `<item sku="CO001"/><item sku="CO001" size="M"/>`, "item duplicates CO001/M"},
		{"element default", `<label>Coffee</label>`, `<label/>`, ""},
		{"duplicate through element default", `<shelf aisle="B"><label>Coffee</label>`, `<shelf aisle="C"><label/><item sku="CO002"/></shelf><shelf aisle="B"><label>Misc</label>`, "shelf duplicates Misc"},
		{"unique absent field is fine", `<label>Coffee</label>`, ``, ""},
		{"keyref from ancestor", `<order aisle="B">`, `<order aisle="C">`, "order refers to C which is not defined (aisleRef)"},
		{"descendant table conflict", `<shelf aisle="B">`, `<shelf aisle="A">`, "shelf duplicates A"},
	})
}

// Options: children left out by the Child hook are neither matched nor
// validated, undeclared attributes go to Undeclared, Check adds value
// checks, Opaque skips content and NoFixed lifts a fixed value; issues say
// their kind and element.
// Covers: SIG-IPE-2396
func TestValidateTreeOptions(t *testing.T) {
	s := loadShop(t)
	const ns = "urn:sigma:test:shop"
	el := func(local string, attrs []Attr, kids ...*Element) *Element {
		return &Element{Space: ns, Local: local, Attrs: attrs, Children: kids, Line: 1}
	}
	a := func(kv ...string) []Attr {
		var out []Attr
		for i := 0; i < len(kv); i += 2 {
			out = append(out, Attr{Local: kv[i], Value: kv[i+1]})
		}
		return out
	}
	unknown := el("gadget", nil)
	foreign := &Element{Space: "urn:other", Prefix: "o", Local: "x"}
	note := el("note", nil, el("anything", nil))
	shop := el("shop", a("version", "2", "colour", "red"),
		foreign,
		el("shelf", a("aisle", "A"), el("item", a("sku", "TE001", "size", "S"))),
		unknown,
		el("order", nil, el("line", a("sku", "TE001", "size", "S"))),
		note)
	root := el("aisles", nil, shop)

	// without hooks: the unknown child, the undeclared attribute and the
	// fixed version are schema errors
	is := s.ValidateTree(root, Options{})
	kinds := map[IssueKind]int{}
	for _, i := range is {
		kinds[i.Kind]++
	}
	if kinds[IssueContent] != 1 || kinds[IssueAttribute] != 2 || len(is) != 3 {
		t.Fatalf("no hooks: %v", is)
	}

	var left, undeclared []string
	opt := Options{
		Child: func(c Child) bool {
			if c.Elem.Space != ns || !c.Declared {
				left = append(left, c.Elem.Local)
				return false
			}
			return true
		},
		Undeclared: func(e *Element, at Attr, allowed bool) bool {
			undeclared = append(undeclared, e.Local+"@"+at.Local)
			return true
		},
		Check: func(typ, v string) string {
			if typ == "Sku" && strings.HasPrefix(v, "TE") {
				return "is a tea"
			}
			return ""
		},
		Opaque:  func(e *Element) bool { return e.Local == "note" },
		NoFixed: func(e *Element, local string) bool { return e == shop && local == "version" },
	}
	is = s.ValidateTree(root, opt)
	if strings.Join(left, ",") != "x,gadget" || strings.Join(undeclared, ",") != "shop@colour" {
		t.Errorf("hooks saw %v and %v", left, undeclared)
	}
	if len(is) != 2 {
		t.Fatalf("with hooks: %v", is)
	}
	for _, i := range is {
		if i.Kind != IssueValue || i.Attr != "sku" || i.Value != "TE001" || i.Why != "is a tea" || i.Elem == nil || i.Elem.Local == "" {
			t.Errorf("issue %+v", i)
		}
	}
}
