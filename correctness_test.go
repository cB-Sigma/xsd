// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package xsd

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

const corrHead = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:t="urn:t" targetNamespace="urn:t" elementFormDefault="qualified">`

func loadOne(body string, extra ...string) (*Schema, error) {
	fsys := fstest.MapFS{"root.xsd": {Data: []byte(corrHead + body + `</xs:schema>`)}}
	for i := 0; i+1 < len(extra); i += 2 {
		fsys[extra[i]] = &fstest.MapFile{Data: []byte(extra[i+1])}
	}
	return Load(fsys, "root.xsd")
}

// Schemas that break the constraints XSD 1.0 puts on schemas themselves
// are refused at load, each with a message naming the problem: the
// representation rules of the schema for schema documents, facets that
// do not apply or loosen their base, value constraints outside their
// type, substitution groups and derivations their bases forbid, complex
// type restrictions that admit more than the base, all groups out of
// place, and XSD regular expressions outside the grammar.
//
// Covers: SIG-IPE-2349
func TestSchemaCorrectnessRefused(t *testing.T) {
	cases := []struct{ name, body, want string }{
		// representation (schema for schema documents)
		{"unknown attribute", `<xs:element name="e" colour="red"/>`, "not allowed on xs:element"},
		{"attribute value", `<xs:element name="e" nillable="maybe"/>`, "not a valid value"},
		{"minOccurs on global", `<xs:element name="e" minOccurs="0"/>`, "not allowed on xs:element"},
		{"children order", `<xs:complexType name="c"><xs:attribute name="a"/><xs:sequence/></xs:complexType>`, "not allowed in that order"},
		{"annotation text", `<xs:annotation>words</xs:annotation>`, "character data"},
		{"ref with type", `<xs:complexType name="c"><xs:sequence><xs:element ref="t:e" type="xs:string"/></xs:sequence></xs:complexType><xs:element name="e"/>`, "ref may not have"},
		{"default and fixed", `<xs:element name="e" default="a" fixed="a"/>`, "both default and fixed"},
		{"required with default", `<xs:complexType name="c"><xs:attribute name="a" use="required" default="x"/></xs:complexType>`, "must be optional"},
		{"duplicate id", `<xs:element name="e" id="x"/><xs:element name="f" id="x"/>`, "used twice"},
		{"notation without ids", `<xs:notation name="n"/>`, "public or system"},
		{"xmlns attribute", `<xs:attribute name="xmlns"/>`, "xmlns"},
		{"bad xml:lang", `<xs:annotation><xs:documentation xml:lang=""/></xs:annotation>`, "language tag"},
		// facets
		{"facet not applicable", `<xs:simpleType name="s"><xs:restriction base="xs:decimal"><xs:length value="2"/></xs:restriction></xs:simpleType>`, "does not apply"},
		{"length and minLength", `<xs:simpleType name="s"><xs:restriction base="xs:string"><xs:length value="5"/><xs:minLength value="1"/></xs:restriction></xs:simpleType>`, "length and minLength"},
		{"min above max", `<xs:simpleType name="s"><xs:restriction base="xs:int"><xs:minInclusive value="5"/><xs:maxInclusive value="4"/></xs:restriction></xs:simpleType>`, "above maxInclusive"},
		{"loosened maxLength", `<xs:simpleType name="b"><xs:restriction base="xs:string"><xs:maxLength value="3"/></xs:restriction></xs:simpleType><xs:simpleType name="s"><xs:restriction base="t:b"><xs:maxLength value="4"/></xs:restriction></xs:simpleType>`, "above the base type's"},
		{"fixed facet", `<xs:simpleType name="b"><xs:restriction base="xs:string"><xs:maxLength value="3" fixed="true"/></xs:restriction></xs:simpleType><xs:simpleType name="s"><xs:restriction base="t:b"><xs:maxLength value="2"/></xs:restriction></xs:simpleType>`, "fixed in the base"},
		{"white space loosened", `<xs:simpleType name="s"><xs:restriction base="xs:token"><xs:whiteSpace value="preserve"/></xs:restriction></xs:simpleType>`, "less collapsed"},
		{"enumeration outside base", `<xs:simpleType name="s"><xs:restriction base="xs:int"><xs:enumeration value="x"/></xs:restriction></xs:simpleType>`, "enumeration value"},
		{"integer fraction digits", `<xs:simpleType name="s"><xs:restriction base="xs:integer"><xs:fractionDigits value="2"/></xs:restriction></xs:simpleType>`, "fractionDigits"},
		{"facet twice", `<xs:simpleType name="s"><xs:restriction base="xs:string"><xs:maxLength value="3"/><xs:maxLength value="3"/></xs:restriction></xs:simpleType>`, "twice"},
		// components
		{"default outside type", `<xs:element name="e" type="xs:int" default="x"/>`, "default or fixed value"},
		{"ID with default", `<xs:attribute name="a" type="xs:ID" default="x"/>`, "type ID"},
		{"default on element content", `<xs:element name="e" default="x"><xs:complexType><xs:sequence><xs:element name="c"/></xs:sequence></xs:complexType></xs:element>`, "element content"},
		{"substitution type", `<xs:element name="h" type="xs:int"/><xs:element name="m" type="xs:string" substitutionGroup="t:h"/>`, "substitution group"},
		{"final restriction", `<xs:simpleType name="b" final="restriction"><xs:restriction base="xs:string"/></xs:simpleType><xs:simpleType name="s"><xs:restriction base="t:b"/></xs:simpleType>`, "final"},
		{"list of lists", `<xs:simpleType name="l"><xs:list itemType="xs:int"/></xs:simpleType><xs:simpleType name="s"><xs:list itemType="t:l"/></xs:simpleType>`, "atomic"},
		{"two ID attributes", `<xs:complexType name="c"><xs:attribute name="a" type="xs:ID"/><xs:attribute name="b" type="xs:ID"/></xs:complexType>`, "more than one attribute of type ID"},
		{"inconsistent elements", `<xs:complexType name="c"><xs:sequence><xs:element name="a" type="xs:int"/><xs:element name="a" type="xs:string"/></xs:sequence></xs:complexType>`, "different types"},
		{"all group nested", `<xs:group name="g"><xs:all><xs:element name="a"/></xs:all></xs:group><xs:complexType name="c"><xs:sequence><xs:group ref="t:g"/></xs:sequence></xs:complexType>`, "all group"},
		{"extension changes mixed", `<xs:complexType name="b" mixed="true"><xs:sequence><xs:element name="a"/></xs:sequence></xs:complexType><xs:complexType name="c"><xs:complexContent><xs:extension base="t:b"><xs:sequence><xs:element name="z"/></xs:sequence></xs:extension></xs:complexContent></xs:complexType>`, "mixed"},
		{"notation twice", `<xs:notation name="n" public="p"/><xs:notation name="n" system="s"/>`, "declared twice"},
		{"notation not declared", `<xs:simpleType name="s"><xs:restriction base="xs:NOTATION"><xs:enumeration value="t:none"/></xs:restriction></xs:simpleType>`, "declared notation"},
		{"NOTATION directly", `<xs:attribute name="a" type="xs:NOTATION"/>`, "xs:NOTATION itself"},
		{"simple content type", `<xs:complexType name="b"><xs:simpleContent><xs:extension base="xs:decimal"/></xs:simpleContent></xs:complexType><xs:complexType name="c"><xs:simpleContent><xs:restriction base="t:b"><xs:simpleType><xs:list itemType="xs:int"/></xs:simpleType></xs:restriction></xs:simpleContent></xs:complexType>`, "must derive"},
		{"import not declared", `<xs:element name="e" type="o:x" xmlns:o="urn:other"/>`, "does not import"},
		// complex type restriction
		{"restriction adds element", `<xs:complexType name="b"><xs:sequence><xs:element name="a"/></xs:sequence></xs:complexType><xs:complexType name="c"><xs:complexContent><xs:restriction base="t:b"><xs:sequence><xs:element name="a"/><xs:element name="z"/></xs:sequence></xs:restriction></xs:complexContent></xs:complexType>`, "not a valid restriction"},
		{"restriction loosens bounds", `<xs:complexType name="b"><xs:sequence><xs:element name="a" maxOccurs="2"/></xs:sequence></xs:complexType><xs:complexType name="c"><xs:complexContent><xs:restriction base="t:b"><xs:sequence><xs:element name="a" maxOccurs="3"/></xs:sequence></xs:restriction></xs:complexContent></xs:complexType>`, "bounds"},
		{"restriction drops required", `<xs:complexType name="b"><xs:sequence><xs:element name="a"/><xs:element name="b"/></xs:sequence></xs:complexType><xs:complexType name="c"><xs:complexContent><xs:restriction base="t:b"><xs:sequence><xs:element name="a"/></xs:sequence></xs:restriction></xs:complexContent></xs:complexType>`, "left out"},
		{"restriction adds attribute", `<xs:complexType name="b"/><xs:complexType name="c"><xs:complexContent><xs:restriction base="t:b"><xs:attribute name="x"/></xs:restriction></xs:complexContent></xs:complexType>`, "adds attribute"},
		{"restriction relaxes required attribute", `<xs:complexType name="b"><xs:attribute name="x" use="required"/></xs:complexType><xs:complexType name="c"><xs:complexContent><xs:restriction base="t:b"><xs:attribute name="x" use="optional"/></xs:restriction></xs:complexContent></xs:complexType>`, "optional"},
		{"restriction widens wildcard", `<xs:complexType name="b"><xs:sequence><xs:any namespace="##targetNamespace"/></xs:sequence></xs:complexType><xs:complexType name="c"><xs:complexContent><xs:restriction base="t:b"><xs:sequence><xs:any namespace="##any"/></xs:sequence></xs:restriction></xs:complexContent></xs:complexType>`, "namespaces"},
		// regular expressions
		{"reluctant quantifier", `<xs:simpleType name="s"><xs:restriction base="xs:string"><xs:pattern value="a+?"/></xs:restriction></xs:simpleType>`, "nothing to repeat"},
		{"bad quantifier", `<xs:simpleType name="s"><xs:restriction base="xs:string"><xs:pattern value="a{,2}"/></xs:restriction></xs:simpleType>`, "quantifier"},
		{"hyphen in class", `<xs:simpleType name="s"><xs:restriction base="xs:string"><xs:pattern value="[a-c-x]"/></xs:restriction></xs:simpleType>`, "unescaped -"},
		{"unbalanced", `<xs:simpleType name="s"><xs:restriction base="xs:string"><xs:pattern value=")("/></xs:restriction></xs:simpleType>`, "unbalanced"},
		{"bracket outside class", `<xs:simpleType name="s"><xs:restriction base="xs:string"><xs:pattern value="a]"/></xs:restriction></xs:simpleType>`, "unescaped"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := loadOne(c.body)
			if err == nil {
				t.Fatal("loaded")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

// Their valid neighbours load: restrictions that narrow, extensions that
// add, facets that tighten, substitution through derived types, groups
// reached again through an element inside them.
//
// Covers: SIG-IPE-2341, SIG-IPE-2348
func TestSchemaCorrectnessAccepted(t *testing.T) {
	cases := map[string]string{
		"narrowing restriction": `<xs:complexType name="b"><xs:sequence><xs:element name="a" minOccurs="0" maxOccurs="5"/><xs:element name="o" minOccurs="0"/></xs:sequence><xs:attribute name="x"/><xs:anyAttribute namespace="##any"/></xs:complexType>
			<xs:complexType name="c"><xs:complexContent><xs:restriction base="t:b"><xs:sequence><xs:element name="a" maxOccurs="2"/></xs:sequence><xs:attribute name="x" use="required"/><xs:attribute name="y"/></xs:restriction></xs:complexContent></xs:complexType>`,
		"restriction of a choice":   `<xs:complexType name="b"><xs:choice maxOccurs="unbounded"><xs:element name="a"/><xs:element name="b"/></xs:choice></xs:complexType><xs:complexType name="c"><xs:complexContent><xs:restriction base="t:b"><xs:sequence><xs:element name="a" maxOccurs="3"/></xs:sequence></xs:restriction></xs:complexContent></xs:complexType>`,
		"restriction of a wildcard": `<xs:complexType name="b"><xs:sequence><xs:any processContents="lax" maxOccurs="unbounded"/></xs:sequence></xs:complexType><xs:complexType name="c"><xs:complexContent><xs:restriction base="t:b"><xs:sequence><xs:element name="a"/><xs:element name="b" minOccurs="0"/></xs:sequence></xs:restriction></xs:complexContent></xs:complexType>`,
		"tightened facets":          `<xs:simpleType name="b"><xs:restriction base="xs:decimal"><xs:totalDigits value="6"/><xs:minInclusive value="0"/></xs:restriction></xs:simpleType><xs:simpleType name="s"><xs:restriction base="t:b"><xs:totalDigits value="4"/><xs:fractionDigits value="2"/><xs:minExclusive value="0"/><xs:maxExclusive value="100"/></xs:restriction></xs:simpleType>`,
		"length restating base":     `<xs:simpleType name="s"><xs:restriction base="xs:IDREFS"><xs:length value="5"/><xs:minLength value="1"/></xs:restriction></xs:simpleType>`,
		"substitution by extension": `<xs:complexType name="b"><xs:sequence><xs:element name="a"/></xs:sequence></xs:complexType><xs:complexType name="d"><xs:complexContent><xs:extension base="t:b"><xs:sequence><xs:element name="z"/></xs:sequence></xs:extension></xs:complexContent></xs:complexType><xs:element name="h" type="t:b"/><xs:element name="m" type="t:d" substitutionGroup="t:h"/>`,
		"group through an element":  `<xs:group name="g"><xs:sequence><xs:element name="a"/><xs:element name="x"><xs:complexType><xs:sequence><xs:group ref="t:g" minOccurs="0"/></xs:sequence></xs:complexType></xs:element></xs:sequence></xs:group><xs:element name="e"><xs:complexType><xs:group ref="t:g"/></xs:complexType></xs:element>`,
		"mixed default":             `<xs:element name="e" default="x"><xs:complexType mixed="true"><xs:sequence><xs:element name="c" minOccurs="0"/></xs:sequence></xs:complexType></xs:element>`,
		"attribute wildcards meet":  `<xs:attributeGroup name="g"><xs:anyAttribute namespace="##other"/></xs:attributeGroup><xs:complexType name="c"><xs:attributeGroup ref="t:g"/><xs:anyAttribute namespace="urn:a urn:t"/></xs:complexType>`,
		"class subtraction":         `<xs:simpleType name="s"><xs:restriction base="xs:string"><xs:pattern value="[a-z-[aeiou]]+|[-a]|\{x\}"/></xs:restriction></xs:simpleType>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := loadOne(body); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Element and type blocks are applied in documents: a member whose type
// the head blocks is not a substitute, and xsi:type may not name a type
// derived in a way the declaration blocks. xsi:nil may not appear on an
// element that is not nillable, two ID attributes may not meet on one
// element and an element with a fixed value has no element children.
//
// Covers: SIG-IPE-2342, SIG-IPE-2343
func TestBlockingInDocuments(t *testing.T) {
	s, err := loadOne(`
		<xs:complexType name="b"><xs:sequence><xs:element name="a" minOccurs="0"/></xs:sequence></xs:complexType>
		<xs:complexType name="d"><xs:complexContent><xs:extension base="t:b"><xs:sequence><xs:element name="z" minOccurs="0"/></xs:sequence></xs:extension></xs:complexContent></xs:complexType>
		<xs:element name="h" type="t:b" block="extension"/>
		<xs:element name="m" type="t:d" substitutionGroup="t:h"/>
		<xs:element name="free" type="t:b"/>
		<xs:element name="root"><xs:complexType><xs:sequence>
			<xs:element ref="t:h" minOccurs="0"/><xs:element ref="t:free" minOccurs="0"/>
		</xs:sequence><xs:anyAttribute namespace="##local" processContents="lax"/></xs:complexType></xs:element>
		<xs:element name="fx" fixed="v"><xs:complexType mixed="true"><xs:sequence><xs:element name="c" minOccurs="0"/></xs:sequence></xs:complexType></xs:element>`)
	if err != nil {
		t.Fatal(err)
	}
	const ns = `xmlns="urn:t" xmlns:t="urn:t" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"`
	good := []string{
		`<root ` + ns + `><h/></root>`,
		`<root ` + ns + `><free xsi:type="t:d"><a/><z/></free></root>`,
		`<fx ` + ns + `>v</fx>`,
	}
	for _, doc := range good {
		if err := s.Validate([]byte(doc)); err != nil {
			t.Errorf("%s: %v", doc, err)
		}
	}
	bad := map[string]string{
		`<root ` + ns + `><m/></root>`:                       "not expected",
		`<root ` + ns + `><h xsi:type="t:d"/></root>`:        "blocks",
		`<root ` + ns + `><h xsi:nil="false"/></root>`:       "not nillable",
		`<fx ` + ns + `>v<c/></fx>`:                          "may not contain elements",
		`<root ` + ns + `><free><a/><a/></free></root>`:      "",
		`<root ` + ns + ` ><h><a/></h><free/><free/></root>`: "",
	}
	for doc, want := range bad {
		err := s.Validate([]byte(doc))
		if err == nil {
			t.Errorf("%s: accepted", doc)
			continue
		}
		if want != "" && !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v (want %q)", doc, err, want)
		}
	}
}

// xs:redefine replaces components of the redefined document everywhere:
// a type redefined by extension of itself, a group taking the old group
// once, an attribute group restricting the old one. The old definitions
// stay reachable only through the new ones.
//
// Covers: SIG-IPE-2349
func TestRedefine(t *testing.T) {
	base := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:t="urn:t" targetNamespace="urn:t" elementFormDefault="qualified">
		<xs:complexType name="person"><xs:sequence><xs:element name="name"/></xs:sequence></xs:complexType>
		<xs:group name="extra"><xs:sequence><xs:element name="note" minOccurs="0"/></xs:sequence></xs:group>
		<xs:attributeGroup name="ids"><xs:attribute name="id"/><xs:attribute name="ref"/></xs:attributeGroup>
		<xs:element name="who" type="t:person"/>
	</xs:schema>`
	s, err := loadOne(`<xs:redefine schemaLocation="base.xsd">
		<xs:complexType name="person"><xs:complexContent><xs:extension base="t:person"><xs:sequence><xs:element name="age"/></xs:sequence><xs:attributeGroup ref="t:ids"/></xs:extension></xs:complexContent></xs:complexType>
		<xs:group name="extra"><xs:sequence><xs:group ref="t:extra"/><xs:element name="tag" minOccurs="0"/></xs:sequence></xs:group>
		<xs:attributeGroup name="ids"><xs:attribute name="id" use="required"/></xs:attributeGroup>
	</xs:redefine>
	<xs:element name="card"><xs:complexType><xs:sequence><xs:element ref="t:who"/><xs:group ref="t:extra"/></xs:sequence></xs:complexType></xs:element>`, "base.xsd", base)
	if err != nil {
		t.Fatal(err)
	}
	const ns = `xmlns="urn:t"`
	if err := s.Validate([]byte(`<card ` + ns + `><who id="1"><name/><age/></who><note/><tag/></card>`)); err != nil {
		t.Errorf("redefined components: %v", err)
	}
	for _, doc := range []string{
		`<card ` + ns + `><who id="1"><name/></who></card>`,               // the redefined type needs age
		`<card ` + ns + `><who><name/><age/></who></card>`,                // the redefined group makes id required
		`<card ` + ns + `><who id="1" ref="x"><name/><age/></who></card>`, // and drops ref
	} {
		if err := s.Validate([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", doc)
		}
	}
	// a redefinition must derive from what it redefines
	if _, err := loadOne(`<xs:redefine schemaLocation="base.xsd"><xs:complexType name="person"><xs:sequence><xs:element name="x"/></xs:sequence></xs:complexType></xs:redefine>`, "base.xsd", base); err == nil {
		t.Error("a redefinition not derived from the old type loaded")
	}
	// a group redefined without itself must restrict the old one
	if _, err := loadOne(`<xs:redefine schemaLocation="base.xsd"><xs:group name="extra"><xs:sequence><xs:element name="other"/></xs:sequence></xs:group></xs:redefine>`, "base.xsd", base); err == nil {
		t.Error("a group redefinition that does not restrict loaded")
	}
}

// An include or import whose location does not resolve is not an error;
// LoadWarnings names it.
//
// Covers: SIG-IPE-2340
func TestUnresolvedLocationWarns(t *testing.T) {
	s, err := loadOne(`<xs:include schemaLocation="absent.xsd"/><xs:element name="e"/>`)
	if err != nil {
		t.Fatal(err)
	}
	w := s.LoadWarnings()
	if len(w) != 1 || !strings.Contains(w[0].Msg, "absent.xsd") {
		t.Errorf("warnings %v", w)
	}
	// a reference to what the missing document would have declared fails
	if _, err := loadOne(`<xs:include schemaLocation="absent.xsd"/><xs:element name="e" type="t:missing"/>`); err == nil {
		t.Error("a reference into a missing document loaded")
	}
}

// Smaller points the W3C suite found: an XML 1.1 document is read, a
// root the schema does not declare may give its type with xsi:type,
// identity constraint paths may hold white space between tokens, huge
// occurrence bounds load, a defaulted IDREFS attribute must resolve,
// fixed values compare in the value space and two ID attributes may not
// meet on one element through a wildcard.
//
// Covers: SIG-IPE-2345, SIG-IPE-2395
func TestSuiteFindings(t *testing.T) {
	s, err := loadOne(`
		<xs:complexType name="ct"><xs:sequence><xs:element name="v" type="xs:int"/></xs:sequence></xs:complexType>
		<xs:element name="u"><xs:simpleType><xs:union memberTypes="xs:boolean xs:string"/></xs:simpleType></xs:element>
		<xs:element name="f" fixed="1"><xs:simpleType><xs:union memberTypes="xs:boolean xs:int"/></xs:simpleType></xs:element>
		<xs:element name="r"><xs:complexType><xs:sequence>
			<xs:element name="k" minOccurs="0" maxOccurs="79228162514264337593543950335"><xs:complexType><xs:attribute name="id"/></xs:complexType></xs:element>
		</xs:sequence><xs:attribute name="refs" type="xs:IDREFS" default="nowhere"/></xs:complexType>
		<xs:key name="kk"><xs:selector xpath=". // t:k"/><xs:field xpath="attribute :: id"/></xs:key></xs:element>
		<xs:attribute name="a" type="xs:ID"/><xs:attribute name="b" type="xs:ID"/>
		<xs:element name="w"><xs:complexType><xs:anyAttribute namespace="urn:t" processContents="lax"/></xs:complexType></xs:element>`)
	if err != nil {
		t.Fatal(err)
	}
	ok := []string{
		`<?xml version="1.1"?><u xmlns="urn:t">x</u>`,
		`<other xmlns="urn:t" xmlns:t="urn:t" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="t:ct"><v>1</v></other>`,
		`<f xmlns="urn:t">true</f>`,
	}
	for _, doc := range ok {
		if err := s.Validate([]byte(doc)); err != nil {
			t.Errorf("%s: %v", doc, err)
		}
	}
	bad := []string{
		`<r xmlns="urn:t"><k id="1"/><k id="1"/></r>`, // key through the spaced paths, and the defaulted IDREFS
		`<r xmlns="urn:t"/>`,                          // the default IDREFS names no ID
		`<f xmlns="urn:t">false</f>`,
		`<w xmlns="urn:t" xmlns:t="urn:t" t:a="x" t:b="y"/>`,
	}
	for _, doc := range bad {
		if err := s.Validate([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", doc)
		}
	}
}

// Documents outside the file system are read only through
// LoadOptions.Fetch, relative locations inside them resolved against
// their URL, within MaxFetches; without Fetch they are not read.
//
// Covers: SIG-IPE-2340
func TestLoadWithFetch(t *testing.T) {
	remote := map[string]string{
		"http://example.com/s/far.xsd":  `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:far"><xs:include schemaLocation="part.xsd"/></xs:schema>`,
		"http://example.com/s/part.xsd": `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:far"><xs:simpleType name="code"><xs:restriction base="xs:string"><xs:length value="2"/></xs:restriction></xs:simpleType></xs:schema>`,
	}
	var asked []string
	fetch := func(u string) ([]byte, error) {
		asked = append(asked, u)
		if b, ok := remote[u]; ok {
			return []byte(b), nil
		}
		return nil, fs.ErrNotExist
	}
	root := corrHead + `<xs:import namespace="urn:far" schemaLocation="http://example.com/s/far.xsd"/><xs:element name="e" xmlns:f="urn:far" type="f:code"/></xs:schema>`
	fsys := fstest.MapFS{"root.xsd": {Data: []byte(root)}}
	s, err := LoadWith(fsys, LoadOptions{Fetch: fetch}, "root.xsd")
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 2 || asked[1] != "http://example.com/s/part.xsd" {
		t.Errorf("fetched %v", asked)
	}
	if err := s.Validate([]byte(`<e xmlns="urn:t">ab</e>`)); err != nil {
		t.Error(err)
	}
	if err := s.Validate([]byte(`<e xmlns="urn:t">abc</e>`)); err == nil {
		t.Error("the fetched type was not applied")
	}
	if _, err := LoadWith(fsys, LoadOptions{Fetch: fetch, MaxFetches: 1}, "root.xsd"); err == nil {
		t.Error("loaded past MaxFetches")
	}
	if _, err := Load(fsys, "root.xsd"); err == nil || !strings.Contains(err.Error(), "not read") {
		t.Errorf("without Fetch: %v", err)
	}
}
