// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package xsd

import (
	"strings"
	"testing"
	"testing/fstest"
)

func entitySchema(t *testing.T) *Schema {
	t.Helper()
	s, err := Load(fstest.MapFS{"e.xsd": {Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="r"><xs:complexType><xs:sequence><xs:element name="v" type="xs:string" maxOccurs="unbounded"/></xs:sequence>
    <xs:attribute name="a" type="xs:string"/></xs:complexType></xs:element></xs:schema>`)}}, "e.xsd")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// With DocOptions.Entities the internal subset's general entities are
// expanded in content and attributes, nested ones and character
// references included; without it, as before, a reference is not
// well-formed.
//
// Covers: SIG-IPE-2397
func TestEntities(t *testing.T) {
	s := entitySchema(t)
	doc := []byte(`<?xml version="1.0"?>
<!DOCTYPE r [
  <!-- a comment -->
  <!ENTITY co "Codebucket &amp; co">
  <!ENTITY full 'Made by &co; &#x263A;'>
  <!ELEMENT r ANY>
]>
<r a="&co;"><v>&full;</v></r>`)
	if _, err := s.ValidateDoc(doc, DocOptions{}); err == nil {
		t.Fatal("entities expanded without the option")
	}
	if _, err := s.ValidateDoc(doc, DocOptions{Entities: true}); err != nil {
		t.Fatal(err)
	}
	root, _, err := parseDoc(doc, Limits{}, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := root.children[0].text.String(); got != "Made by Codebucket & co ☺" {
		t.Fatalf("expanded to %q", got)
	}
	if a, _ := root.attr("a"); a != "Codebucket & co" {
		t.Fatalf("attribute %q", a)
	}
}

// Expansion is bounded: the billion-laughs pattern, self reference, deep
// nesting, external entities and markup in entities are refused.
//
// Covers: SIG-IPE-2397
func TestEntityLimits(t *testing.T) {
	s := entitySchema(t)
	var lol strings.Builder
	lol.WriteString(`<!DOCTYPE r [<!ENTITY l0 "lol">`)
	for i := 1; i <= 9; i++ {
		lol.WriteString(`<!ENTITY l` + string(rune('0'+i)) + ` "`)
		for range 10 {
			lol.WriteString(`&l` + string(rune('0'+i-1)) + `;`)
		}
		lol.WriteString(`">`)
	}
	lol.WriteString(`]><r><v>&l9;</v></r>`)
	_, err := s.ValidateDoc([]byte(lol.String()), DocOptions{Entities: true})
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("billion laughs: %v", err)
	}
	// many references to a modest entity
	many := `<!DOCTYPE r [<!ENTITY k "` + strings.Repeat("k", 1000) + `">]><r>` + strings.Repeat("<v>&k;</v>", 2000) + `</r>`
	if _, err := s.ValidateDoc([]byte(many), DocOptions{Entities: true}); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("references past the budget: %v", err)
	}
	if _, err := s.ValidateDoc([]byte(many), DocOptions{Entities: true, Limits: Limits{MaxEntityBytes: 4 << 20}}); err != nil {
		t.Fatalf("a raised budget: %v", err)
	}
	for name, doc := range map[string]string{
		"self":     `<!DOCTYPE r [<!ENTITY a "x&b;"><!ENTITY b "&a;">]><r><v>&a;</v></r>`,
		"external": `<!DOCTYPE r [<!ENTITY x SYSTEM "file:///etc/passwd">]><r><v>&x;</v></r>`,
		"markup":   `<!DOCTYPE r [<!ENTITY m "<v>x</v>">]><r>&m;</r>`,
		"param":    `<!DOCTYPE r [<!ENTITY % p "x"><!ENTITY q "%p;">]><r><v>&q;</v></r>`,
	} {
		if _, err := s.ValidateDoc([]byte(doc), DocOptions{Entities: true}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// declared but unused external entities do no harm
	ok := `<!DOCTYPE r [<!ENTITY x SYSTEM "file:///etc/passwd">]><r><v>a</v></r>`
	if _, err := s.ValidateDoc([]byte(ok), DocOptions{Entities: true}); err != nil {
		t.Fatal(err)
	}
}

// Any document either parses or is refused, without a panic or unbounded
// growth, with entities on.
func FuzzEntities(f *testing.F) {
	f.Add(`<!DOCTYPE r [<!ENTITY a "x&#38;amp;y"><!ENTITY b "&a;&a;">]><r>&b;</r>`)
	f.Add(`<!DOCTYPE r [<!ENTITY a '&#60;'>]><r a="&a;"/>`)
	f.Fuzz(func(t *testing.T, doc string) {
		root, _, err := parseDoc([]byte(doc), Limits{MaxEntityBytes: 4096}, false, true)
		if err != nil || root == nil {
			return
		}
		var size func(n *node) int
		size = func(n *node) int {
			s := n.text.Len()
			for _, a := range n.attrs {
				s += len(a.value)
			}
			for _, c := range n.children {
				s += size(c)
			}
			return s
		}
		if got := size(root); got > len(doc)+4096 {
			t.Fatalf("%d bytes of text from a %d-byte document", got, len(doc))
		}
	})
}
