package xsd

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

func loadSet(t *testing.T) *Schema {
	t.Helper()
	s, err := Load(os.DirFS("testdata/set"), "main.xsd")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const validLibrary = `<?xml version="1.0"?>
<m:library xmlns:m="urn:sigma:test:main" xmlns:o="urn:sigma:test:other"
  xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
  owner="lib1" version="1.0" xml:lang="en" o:flag="true" extra="1">
  <m:book id="b1" lang="en">
    <m:title script="latin">Go</m:title>
    <m:isbn>978-1234567890</m:isbn>
    <m:pages>300</m:pages>
    <m:authors>alice bob</m:authors>
    <m:edition>2</m:edition><m:year>2020</m:year>
    <m:price currency="INR">499.00</m:price>
    <m:meta><m:size>3</m:size><m:created>2026-01-02T03:04:05Z</m:created></m:meta>
    <m:shape xsi:type="m:Circle" unit="cm"><m:radius>1.5</m:radius></m:shape>
    <m:lent to="e1" o:x="1"/>
  </m:book>
  <m:ebook id="e1"><m:title>E</m:title><m:isbn>123456789X</m:isbn></m:ebook>
  <m:disc id="d1"><m:title>D</m:title></m:disc>
  <m:note>text <m:em>x</m:em> more</m:note>
  <o:tag k="a"><anything xmlns="urn:x"><deep/></anything></o:tag>
  <x:ext xmlns:x="urn:ext"><m:money>1.5</m:money></x:ext>
</m:library>`

type mutation struct {
	name, from, to, want string
}

// mutate runs each mutation of a valid document and expects an error
// containing want ("" expects the document to stay valid).
func mutate(t *testing.T, s *Schema, base string, cases []mutation) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.from != "" && !strings.Contains(base, c.from) {
				t.Fatalf("mutation source %q not in the document", c.from)
			}
			doc := strings.Replace(base, c.from, c.to, 1)
			err := s.Validate([]byte(doc))
			if c.want == "" {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("accepted; want an error containing %q", c.want)
			}
			var errs *Errors
			if !errors.As(err, &errs) || len(*errs) == 0 {
				t.Fatalf("error is %T, not *Errors", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not mention %q", err, c.want)
			}
			if (*errs)[0].Line == 0 && c.want != "not well-formed" {
				t.Errorf("issue without a line: %+v", (*errs)[0])
			}
		})
	}
}

// Loading follows include and import (relative locations, several target
// namespaces, a Latin-1 declared part, the unprefixed XSD style); a valid
// document passes.
//
// Covers: SIG-IPE-2340
func TestLoadIncludeImport(t *testing.T) {
	s := loadSet(t)
	for _, q := range []qname{{"urn:sigma:test:main", "library"}, {"urn:sigma:test:main", "money"}, {"urn:sigma:test:other", "tag"}} {
		if s.elems[q] == nil {
			t.Errorf("global element %s missing", q)
		}
	}
	if err := s.Validate([]byte(validLibrary)); err != nil {
		t.Fatal(err)
	}
	mutate(t, s, validLibrary, []mutation{
		{"root not global", validLibrary, `<m:title xmlns:m="urn:sigma:test:main">x</m:title>`, "not a global element"},
		{"not well-formed", validLibrary, `<m:library xmlns:m="urn:sigma:test:main">`, "not well-formed"},
		// the XML declaration needs a version and must come first
		{"declaration without version", `<?xml version="1.0"?>`, `<?xml?>`, "not well-formed"},
		{"declaration with encoding only", `<?xml version="1.0"?>`, `<?xml encoding="UTF-8"?>`, "not well-formed"},
		// white space before the declaration is accepted by default (see
		// TestLeadingSpace); anything else before it is not
		{"space before declaration", `<?xml version="1.0"?>`, "\n<?xml version=\"1.0\"?>", ""},
		{"text before declaration", `<?xml version="1.0"?>`, "x<?xml version=\"1.0\"?>", "not well-formed"},
		{"declaration after a comment", `<?xml version="1.0"?>`, `<!-- c --><?xml version="1.0"?>`, "not well-formed"},
		{"declaration with single quotes", `<?xml version="1.0"?>`, `<?xml version='1.0' encoding="UTF-8" standalone="yes"?>`, ""},
		{"byte order mark", `<?xml version="1.0"?>`, "\uFEFF<?xml version=\"1.0\"?>", ""},
		{"no declaration", `<?xml version="1.0"?>`, "", ""},
	})
}

// A schema set loads from a zip archive, the root found by base name,
// macOS resource entries ignored.
//
// Covers: SIG-IPE-2340
func TestLoadZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, b []byte) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(b)
	}
	for _, f := range []string{"main.xsd", "parts.xsd", "sub/other.xsd"} {
		b, err := os.ReadFile("testdata/set/" + f)
		if err != nil {
			t.Fatal(err)
		}
		add("pkg/schema/"+f, b)
	}
	add("__MACOSX/pkg/schema/._main.xsd", []byte("junk"))
	add("pkg/schema/._main.xsd", []byte("junk"))
	zw.Close()
	s, err := LoadZip(buf.Bytes(), "main.xsd")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate([]byte(validLibrary)); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadZip(buf.Bytes(), "absent.xsd"); err == nil {
		t.Fatal("missing root accepted")
	}
	if _, err := LoadZip([]byte("not a zip"), "main.xsd"); err == nil {
		t.Fatal("non-zip accepted")
	}
}

// Sequences, choices, all, named groups and occurrence bounds.
//
// Covers: SIG-IPE-2341
func TestContentModels(t *testing.T) {
	s := loadSet(t)
	mutate(t, s, validLibrary, []mutation{
		{"optional absent", `<m:pages>300</m:pages>`, ``, ""},
		{"order", `<m:title script="latin">Go</m:title>`, `<m:pages>1</m:pages><m:title>Go</m:title>`, "m:pages"},
		{"choice missing", `<m:isbn>978-1234567890</m:isbn>`, ``, "pages is out of order in m:book (expected isbn or issn)"},
		{"choice other branch", `<m:isbn>978-1234567890</m:isbn>`, `<m:issn>1234-5678</m:issn>`, ""},
		{"both branches", `<m:isbn>978-1234567890</m:isbn>`, `<m:isbn>978-1234567890</m:isbn><m:issn>1</m:issn>`, "issn is out of order"},
		{"maxOccurs", `<m:note>text <m:em>x</m:em> more</m:note>`, `<m:note/><m:note/><m:note/>`, "at most 2"},
		{"group optional", `<m:edition>2</m:edition><m:year>2020</m:year>`, ``, ""},
		{"group incomplete", `<m:edition>2</m:edition>`, ``, "is out of order"},
		{"all any order", `<m:size>3</m:size><m:created>2026-01-02T03:04:05Z</m:created>`, `<m:created>2026-01-02T03:04:05Z</m:created>`, ""},
		{"all missing", `<m:created>2026-01-02T03:04:05Z</m:created>`, ``, "meta needs at least 1 created"},
		{"all repeated", `<m:size>3</m:size>`, `<m:size>3</m:size><m:size>4</m:size>`, "m:size"},
		{"unknown element", `<m:pages>300</m:pages>`, `<m:bogus/>`, "m:bogus is not expected"},
		{"character data", `<m:pages>300</m:pages>`, `stray <m:pages>300</m:pages>`, "may not contain character data"},
		{"mixed allows text", `<m:note>text`, `<m:note>more text`, ""},
		{"empty content", `<m:lent to="e1" o:x="1"/>`, `<m:lent to="e1"><m:em/></m:lent>`, "lent may not contain m:em"},
		{"simple content child", `<m:title>E</m:title>`, `<m:title><m:em/></m:title>`, "may not contain elements"},
	})
}

// Substitution groups (transitive, abstract heads, a member taking the
// head's type) and element references across namespaces.
//
// Covers: SIG-IPE-2342
func TestSubstitutionGroups(t *testing.T) {
	s := loadSet(t)
	mutate(t, s, validLibrary, []mutation{
		{"abstract head", `<m:disc id="d1"><m:title>D</m:title></m:disc>`, `<m:item id="d1"><m:title>D</m:title></m:item>`, "abstract"},
		{"member of member", `<m:disc id="d1"><m:title>D</m:title></m:disc>`, `<m:ebook id="d1"><m:title>D</m:title><m:isbn>123456789X</m:isbn></m:ebook>`, ""},
		{"member uses head type", `<m:disc id="d1"><m:title>D</m:title></m:disc>`, `<m:disc id="d1"/>`, "disc needs at least 1 title"},
		{"foreign ref", `<o:tag k="a">`, `<o:tag>`, "required attribute k"},
	})
}

// Complex type derivation (extension, restriction with a prohibited
// attribute), simple content by extension and restriction, abstract types
// and xsi:type.
//
// Covers: SIG-IPE-2343
func TestDerivationAndXsiType(t *testing.T) {
	s := loadSet(t)
	mutate(t, s, validLibrary, []mutation{
		{"prohibited attribute", `<m:ebook id="e1">`, `<m:ebook id="e1" lang="en">`, "attribute lang is not allowed"},
		{"restricted content", `<m:isbn>123456789X</m:isbn></m:ebook>`, `<m:isbn>123456789X</m:isbn><m:pages>2</m:pages></m:ebook>`, "pages is not expected in m:ebook"},
		{"abstract type", ` xsi:type="m:Circle"`, ``, "abstract"},
		{"xsi:type not derived", `m:Circle`, `m:Unrelated`, "not derived"},
		{"xsi:type unknown", `m:Circle`, `m:Nothing`, "does not define"},
		{"xsi:type bad prefix", `m:Circle`, `zz:Circle`, "not declared"},
		{"xsi:type content", `<m:radius>1.5</m:radius>`, ``, "shape needs at least 1 radius"},
		{"extension attribute", `<m:title script="latin">`, `<m:title script="latin" bad="1">`, "attribute bad is not allowed"},
	})
	small := `<m:small xmlns:m="urn:sigma:test:main">abc</m:small>`
	mutate(t, s, small, []mutation{
		{"simple content restriction ok", "", "", ""},
		{"simple content restriction facet", "abc", "toolong", "above the maximum 5"},
		{"inherited facet", "abc", "", "below the minimum 1"},
	})
}

// Element and attribute wildcards: namespace constraints and strict, lax
// and skip processing.
//
// Covers: SIG-IPE-2344
func TestWildcards(t *testing.T) {
	s := loadSet(t)
	mutate(t, s, validLibrary, []mutation{
		{"lax validates known", `<m:money>1.5</m:money>`, `<m:money>0</m:money>`, "more than 0"},
		{"lax skips unknown", `<m:money>1.5</m:money>`, `<m:whatever/>`, ""},
		{"##other excludes target", `<x:ext xmlns:x="urn:ext">`, `<x:ext xmlns:x="urn:sigma:test:main">`, "not expected"},
		{"skip", `<deep/>`, `<deep bad="x"><m:money>0</m:money></deep>`, ""},
		{"attr wildcard list", ` extra="1"`, ` xmlns:z="urn:zzz" z:q="1"`, "attribute z:q is not allowed"},
		{"attr wildcard lax global", `o:flag="true"`, `o:flag="yes"`, "not a boolean"},
		{"attr wildcard skip", `o:x="1"`, `o:x="anything"`, ""},
	})
	box := `<o:strictBox xmlns:o="urn:sigma:test:other" xmlns:m="urn:sigma:test:main"><m:money>2</m:money></o:strictBox>`
	mutate(t, s, box, []mutation{
		{"strict known", "", "", ""},
		{"strict unknown", "<m:money>2</m:money>", "<m:nothing/>", "strict wildcard"},
		{"strict namespace", "<m:money>2</m:money>", `<q xmlns="urn:q"/>`, "not expected"},
	})
}

// Attributes: required, fixed, default, attribute groups, xml:lang.
//
// Covers: SIG-IPE-2345
func TestAttributes(t *testing.T) {
	s := loadSet(t)
	mutate(t, s, validLibrary, []mutation{
		{"required missing", ` owner="lib1"`, ``, "required attribute owner is missing"},
		{"fixed differs", `version="1.0" xml:lang`, `version="2.0" xml:lang`, "fixed value"},
		{"fixed absent", ` version="1.0" xml:lang`, ` xml:lang`, ""},
		{"group pattern", `version="1.0" xml:lang`, `version="1.x" xml:lang`, "pattern"},
		{"xml:lang", `xml:lang="en"`, `xml:lang="toolonglanguage"`, "no member type"},
		{"xml:lang empty", `xml:lang="en"`, `xml:lang=""`, ""},
		{"default not required", ` script="latin"`, ``, ""},
		{"simple content fixed attr", `currency="INR"`, `currency="INR" scheme="trade"`, "fixed value"},
		{"unknown xsi attribute", ` extra="1"`, ` xsi:bogus="1"`, "unknown attribute xsi:bogus"},
	})
}

// Simple types: facets, union, list, derivation chains.
//
// Covers: SIG-IPE-2348
func TestSimpleTypes(t *testing.T) {
	s := loadSet(t)
	mutate(t, s, validLibrary, []mutation{
		{"pattern alternatives", `978-1234567890`, `12345`, "does not match the pattern"},
		{"minInclusive", `<m:pages>300</m:pages>`, `<m:pages>0</m:pages>`, "at least 1"},
		{"maxExclusive", `<m:pages>300</m:pages>`, `<m:pages>10000</m:pages>`, "less than 10000"},
		{"integer lexical", `<m:pages>300</m:pages>`, `<m:pages>3.5</m:pages>`, "not an integer"},
		{"collapse", `<m:pages>300</m:pages>`, "<m:pages>\n 300 \n</m:pages>", ""},
		{"union member", `currency="INR"`, `currency="x-GBP"`, ""},
		{"union none", `currency="INR"`, `currency="GBP"`, "no member type"},
		{"list length", `alice bob`, `a b c d`, "length 4"},
		{"list item", `alice bob`, `alice 1bob`, "list item"},
		{"positiveInteger", `<m:edition>2</m:edition>`, `<m:edition>0</m:edition>`, "below the minimum 1"},
		{"unsignedShort", `<m:size>3</m:size>`, `<m:size>70000</m:size>`, "above the maximum 65535"},
		{"dateTime", `2026-01-02T03:04:05Z`, `2026-02-30T03:04:05Z`, "day out of range"},
		{"gYear", `<m:year>2020</m:year>`, `<m:year>20</m:year>`, "not a valid gYear"},
		{"double", `<m:radius>1.5</m:radius>`, `<m:radius>1.5.1</m:radius>`, "not a double"},
		{"decimal", `499.00`, `4,99`, "not a decimal"},
	})
	mutate(t, s, `<m:money xmlns:m="urn:sigma:test:main">12.5</m:money>`, []mutation{
		{"fractionDigits", "12.5", "12.555", "fraction digits"},
		{"totalDigits", "12.5", "1234567", "digits, more than 6"},
		{"trailing zeros", "12.5", "0012.500", ""},
	})
	mutate(t, s, `<m:fixedText xmlns:m="urn:sigma:test:main"> same </m:fixedText>`, []mutation{
		{"fixed element", "", "", ""},
		{"fixed element empty", " same ", "", ""},
		{"fixed element differs", " same ", "other", "fixed value"},
	})
}

// xsi:nil and nillable; ID uniqueness and IDREF resolution.
//
// Covers: SIG-IPE-2343, SIG-IPE-2345
func TestNilAndIDs(t *testing.T) {
	s := loadSet(t)
	mutate(t, s, validLibrary, []mutation{
		{"nil allowed", `<m:price currency="INR">499.00</m:price>`, `<m:price currency="INR" xsi:nil="true"/>`, ""},
		{"nil with content", `<m:price currency="INR">499.00</m:price>`, `<m:price currency="INR" xsi:nil="true">1</m:price>`, "must be empty"},
		{"nil not nillable", `<m:title>E</m:title>`, `<m:title xsi:nil="true"/>`, "not nillable"},
		{"nil not boolean", `<m:price currency="INR">499.00</m:price>`, `<m:price currency="INR" xsi:nil="maybe"/>`, "not a boolean"},
		{"dangling IDREF", `to="e1"`, `to="zz"`, `IDREF "zz"`},
		{"duplicate ID", `<m:disc id="d1">`, `<m:disc id="b1">`, "already used"},
		{"ID lexical", `<m:disc id="d1">`, `<m:disc id="1d">`, "NCName"},
	})
}

// Constructs the validator does not implement fail at load time, as do
// broken references; nothing is accepted loosely.
//
// Covers: SIG-IPE-2349
func TestLoadRefusesUnsupported(t *testing.T) {
	head := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:t="urn:t" targetNamespace="urn:t" elementFormDefault="qualified">`
	cases := map[string]string{
		"redefine":        `<xs:redefine schemaLocation="x.xsd"/>`,
		"identity path":   `<xs:element name="e"><xs:complexType/><xs:key name="k"><xs:selector xpath="../x"/><xs:field xpath="@a"/></xs:key></xs:element>`,
		"keyref refer":    `<xs:element name="e"><xs:complexType/><xs:keyref name="k" refer="t:none"><xs:selector xpath="."/><xs:field xpath="@a"/></xs:keyref></xs:element>`,
		"undefined type":  `<xs:element name="e" type="t:Nope"/>`,
		"undefined ref":   `<xs:complexType name="c"><xs:sequence><xs:element ref="t:nope"/></xs:sequence></xs:complexType>`,
		"block escape":    `<xs:simpleType name="s"><xs:restriction base="xs:string"><xs:pattern value="\p{IsKlingon}+"/></xs:restriction></xs:simpleType>`,
		"nested class":    `<xs:simpleType name="s"><xs:restriction base="xs:string"><xs:pattern value="[a-[b]c]"/></xs:restriction></xs:simpleType>`,
		"string range":    `<xs:simpleType name="s"><xs:restriction base="xs:string"><xs:minInclusive value="a"/></xs:restriction></xs:simpleType>`,
		"unknown facet":   `<xs:simpleType name="s"><xs:restriction base="xs:string"><xs:assertion test="true()"/></xs:restriction></xs:simpleType>`,
		"xsd 1.1":         `<xs:defaultOpenContent><xs:any/></xs:defaultOpenContent>`,
		"missing include": `<xs:include schemaLocation="absent.xsd"/>`,
		"remote import":   `<xs:import namespace="urn:far" schemaLocation="http://example.com/far.xsd"/>`,
		"escape outside":  `<xs:include schemaLocation="../up.xsd"/>`,
		"circular type":   `<xs:complexType name="a"><xs:complexContent><xs:extension base="t:b"/></xs:complexContent></xs:complexType><xs:complexType name="b"><xs:complexContent><xs:extension base="t:a"/></xs:complexContent></xs:complexType>`,
		"duplicate":       `<xs:element name="e"/><xs:element name="e"/>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"root.xsd": {Data: []byte(head + body + `</xs:schema>`)},
				"cham.xsd": {Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`)},
			}
			if _, err := Load(fsys, "root.xsd"); err == nil {
				t.Fatal("loaded")
			} else {
				t.Log(err)
			}
		})
	}
	if _, err := Load(fstest.MapFS{"x.xsd": {Data: []byte(`<schema/>`)}}, "x.xsd"); err == nil {
		t.Fatal("non-XSD root accepted")
	}
}

// The built-in datatypes' lexical spaces and ranges.
//
// Covers: SIG-IPE-2348
func TestBuiltins(t *testing.T) {
	type c struct{ typ, v string }
	good := []c{
		{"boolean", "true"}, {"boolean", "0"}, {"decimal", "-1.50"}, {"decimal", ".5"}, {"integer", "+12"},
		{"long", "-9223372036854775808"}, {"int", "2147483647"}, {"short", "-32768"}, {"byte", "127"},
		{"unsignedLong", "18446744073709551615"}, {"unsignedInt", "0"}, {"unsignedByte", "255"},
		{"nonNegativeInteger", "0"}, {"negativeInteger", "-1"}, {"nonPositiveInteger", "0"},
		{"float", "1e10"}, {"float", "INF"}, {"double", "-INF"}, {"double", "NaN"}, {"double", "1.5E-3"},
		{"dateTime", "2024-02-29T23:59:59.123+05:30"}, {"dateTime", "2026-01-01T24:00:00"},
		{"date", "2026-09-25Z"}, {"time", "10:00:00"}, {"gYearMonth", "2026-09"}, {"gMonthDay", "--02-29"},
		{"gDay", "---31"}, {"gMonth", "--12"}, {"duration", "P1Y2M3DT4H5M6.5S"}, {"duration", "-PT1H"}, {"duration", "P1D"},
		{"hexBinary", "0aFF"}, {"base64Binary", "aGVsbG8="}, {"base64Binary", "aGVs bG8="}, {"anyURI", "https://a.example/x?y#z"},
		{"anyURI", "relative/path"}, {"QName", "xs:string"}, {"NCName", "a-b.c"}, {"Name", "a:b"}, {"ID", "_x1"},
		{"NMTOKEN", "12-ab"}, {"language", "en-IN"}, {"token", "a b"},
	}
	bad := []c{
		{"boolean", "yes"}, {"decimal", "1e3"}, {"integer", "1.0"}, {"int", "2147483648"}, {"byte", "-129"},
		{"unsignedLong", "18446744073709551616"}, {"unsignedInt", "-1"}, {"positiveInteger", "0"},
		{"negativeInteger", "0"}, {"float", "1e39"}, {"double", "+INF"}, {"double", "abc"},
		{"dateTime", "2023-02-29T00:00:00"}, {"dateTime", "2026-01-01T24:00:01"}, {"dateTime", "2026-01-01"},
		{"dateTime", "0000-01-01T00:00:00"}, {"dateTime", "2026-01-01T10:00:00+15:00"}, {"date", "2026-13-01"},
		{"time", "10:60:00"}, {"gMonth", "--13"}, {"duration", "P"}, {"duration", "PT"}, {"duration", "P1H"},
		{"hexBinary", "abc"}, {"hexBinary", "zz"}, {"base64Binary", "a==="}, {"anyURI", "%zz"},
		{"QName", "zz:x"}, {"QName", "a:b:c"}, {"NCName", "a:b"}, {"NCName", "1a"}, {"Name", "1a"},
		{"NMTOKEN", "a b"}, {"language", "toolonglang"},
	}
	ctx := &node{ns: newScope(map[string]string{"xs": nsXS, "xml": nsXML})}
	for _, x := range good {
		if why := builtinCheck(x.typ, x.v, ctx); why != "" {
			t.Errorf("%s %q rejected: %s", x.typ, x.v, why)
		}
	}
	for _, x := range bad {
		if why := builtinCheck(x.typ, x.v, ctx); why == "" {
			t.Errorf("%s %q accepted", x.typ, x.v)
		}
	}
	// the list built-ins
	s := loadSet(t)
	for _, x := range []struct {
		typ, v string
		ok     bool
	}{{"NMTOKENS", "a b", true}, {"NMTOKENS", "", false}, {"IDREFS", "a b", true}, {"IDREFS", "a 1", false}} {
		_, why := s.checkSimple(s.types[qname{nsXS, x.typ}], x.v, ctx)
		if (why == "") != x.ok {
			t.Errorf("%s %q: %q", x.typ, x.v, why)
		}
	}
}

// XSD regular expressions are translated to anchored RE2 with the XSD
// meanings of ., ^, $ and the class escapes.
//
// Covers: SIG-IPE-2348
func TestRegexTranslation(t *testing.T) {
	cases := []struct {
		re, v string
		ok    bool
	}{
		{`\d+`, "123", true}, {`\d+`, "12a", false}, {`\d+`, "١٢", true},
		{`abc`, "xabcx", false}, {`a^b$`, "a^b$", true}, {`.`, "\n", false}, {`.`, "x", true},
		{`\s\S`, " x", true}, {`\s`, " ", false}, {`[\s,]+`, " , ", true},
		{`\w+`, "abc9", true}, {`\w`, "_", false}, {`\w`, "-", false}, {`\w`, "€", true}, {`[\w-]+`, "a-b", true},
		{`\i\c*`, "a1:b", true}, {`\i\c*`, "1a", false}, {`\I`, "1", true}, {`\C`, " ", true},
		{`[^a-c]`, "d", true}, {`[a^]`, "^", true}, {`\p{Lu}+`, "ABC", true}, {`\P{Lu}`, "a", true},
		{`x-\S.*`, "x-GBP", true}, {`(ab)|c`, "c", true}, {`\\\|\.`, `\|.`, true}, {`a{2,3}`, "aaa", true},
		{`\-?\d`, "-1", true},
		{`[\p{IsBasicLatin}\p{IsLatin-1Supplement}]*`, "Zürich", true}, {`[\p{IsBasicLatin}]*`, "Zürich", false},
		{`\p{IsGreek}`, "λ", true}, {`\P{IsGreek}`, "λ", false},
		// computed classes: subtraction, \P{IsBlock}, \I and \C inside a class
		{`[a-z-[aeiou]]+`, "bcd", true}, {`[a-z-[aeiou]]`, "e", false}, {`[^a-z-[0-9]]`, "5", false},
		{`[^a-z-[0-9]]`, "A", true}, {`[\c-[:]]+`, "a1.b", true}, {`[\c-[:]]`, ":", false},
		{`[\P{IsGreek}]`, "λ", false}, {`[\P{IsGreek}]`, "a", true}, {`[\I\d]`, "5", true}, {`[\I]`, "a", false},
		{`[a-c-[b-[b]]]`, "b", true}, {`\p{IsPrivateUse}`, "\U000F0001", true}, {`[\p{IsSpecials}]`, "�", true},
	}
	for _, c := range cases {
		re, err := translateRegex(c.re)
		if err != nil {
			t.Errorf("%s: %v", c.re, err)
			continue
		}
		if re.MatchString(c.v) != c.ok {
			t.Errorf("%s on %q: want %v", c.re, c.v, c.ok)
		}
	}
	for _, bad := range []string{`\p{IsKlingon}`, `[\P{IsKlingon}]`, `[a-z-[aeiou`, `[a-[b]c]`, `(?i)a`, `a\`, `[ab`, `\q`} {
		if _, err := translateRegex(bad); err == nil {
			t.Errorf("%s translated", bad)
		}
	}
}

// Errors caps what it prints.
func TestErrorsCapped(t *testing.T) {
	s := loadSet(t)
	var b strings.Builder
	b.WriteString(`<m:values xmlns:m="urn:sigma:test:main">`)
	for range 30 {
		b.WriteString(`<m:v><m:x/></m:v>`)
	}
	b.WriteString(`</m:values>`)
	err := s.Validate([]byte(b.String()))
	if err == nil || !strings.Contains(err.Error(), "more") {
		t.Fatalf("%v", err)
	}
}

// Content models in which an element could match two different particles
// (Unique Particle Attribution) are refused when the schema loads, counting
// occurrence bounds exactly; unambiguous ones load.
// Covers: SIG-IPE-2398
func TestUniqueParticleAttribution(t *testing.T) {
	head := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:t="urn:t" xmlns:o="urn:o" targetNamespace="urn:t" elementFormDefault="qualified">
<xs:import namespace="urn:o" schemaLocation="o.xsd"/>
<xs:element name="head" abstract="true"/><xs:element name="member" substitutionGroup="t:head"/>`
	other := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:o"><xs:element name="tag"/></xs:schema>`
	seq := func(body string) string {
		return `<xs:complexType name="c"><xs:sequence>` + body + `</xs:sequence></xs:complexType>`
	}
	ambiguous := map[string]string{
		"optional then same":        seq(`<xs:element name="a" minOccurs="0"/><xs:element name="a"/>`),
		"choice of same":            `<xs:complexType name="c"><xs:choice><xs:element name="a"/><xs:sequence><xs:element name="a"/><xs:element name="b"/></xs:sequence></xs:choice></xs:complexType>`,
		"optional then wildcard":    seq(`<xs:element ref="o:tag" minOccurs="0"/><xs:any namespace="##other" processContents="lax" minOccurs="0"/>`),
		"repeat then wildcard":      seq(`<xs:element name="a" maxOccurs="unbounded"/><xs:any namespace="##targetNamespace" minOccurs="0"/>`),
		"two wildcards":             seq(`<xs:any namespace="##other" minOccurs="0"/><xs:any namespace="urn:o"/>`),
		"bounded beyond minimum":    seq(`<xs:element name="a" minOccurs="2" maxOccurs="3"/><xs:element name="a" minOccurs="0"/>`),
		"repeated group":            seq(`<xs:sequence maxOccurs="unbounded"><xs:element name="a"/><xs:element name="b" minOccurs="0"/></xs:sequence><xs:element name="b"/>`),
		"substitution group":        seq(`<xs:element ref="t:head" minOccurs="0"/><xs:element ref="t:member"/>`),
		"extension adds same":       `<xs:complexType name="b"><xs:sequence><xs:element name="a" minOccurs="0"/></xs:sequence></xs:complexType><xs:complexType name="c"><xs:complexContent><xs:extension base="t:b"><xs:sequence><xs:element name="a"/></xs:sequence></xs:extension></xs:complexContent></xs:complexType>`,
		"anonymous type":            `<xs:element name="e"><xs:complexType><xs:sequence><xs:element name="a" minOccurs="0"/><xs:element name="a"/></xs:sequence></xs:complexType></xs:element>`,
		"all with the same element": `<xs:complexType name="c"><xs:all><xs:element name="a"/><xs:element name="a" minOccurs="0"/></xs:all></xs:complexType>`,
	}
	unambiguous := map[string]string{
		"exact count then optional": seq(`<xs:element name="a" minOccurs="2" maxOccurs="2"/><xs:element name="a" minOccurs="0"/>`),
		"required between":          seq(`<xs:element name="a" minOccurs="0"/><xs:element name="b"/><xs:element name="a"/>`),
		"wildcard of other names":   seq(`<xs:element name="a" maxOccurs="unbounded"/><xs:any namespace="##other" minOccurs="0"/>`),
		"same particle repeated":    seq(`<xs:element name="a" maxOccurs="unbounded"/><xs:element name="b"/>`),
		"choice of distinct":        `<xs:complexType name="c"><xs:choice maxOccurs="unbounded"><xs:element name="a"/><xs:element name="b"/><xs:any namespace="urn:o"/></xs:choice></xs:complexType>`,
		"large bounds":              seq(`<xs:element name="a" minOccurs="500" maxOccurs="500"/><xs:element name="a" minOccurs="0"/>`),
	}
	load := func(body string) error {
		_, err := Load(fstest.MapFS{"root.xsd": {Data: []byte(head + body + `</xs:schema>`)}, "o.xsd": {Data: []byte(other)}}, "root.xsd")
		return err
	}
	for name, body := range ambiguous {
		t.Run(name, func(t *testing.T) {
			if err := load(body); err == nil || !strings.Contains(err.Error(), "Unique Particle Attribution") {
				t.Fatalf("loaded or other error: %v", err)
			} else {
				t.Log(err)
			}
		})
	}
	for name, body := range unambiguous {
		t.Run(name, func(t *testing.T) {
			if err := load(body); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A schema document without a target namespace included into one takes
// that namespace (chameleon include): its components and the unprefixed
// references among them move into it.
// Covers: SIG-IPE-2399
func TestChameleonInclude(t *testing.T) {
	fsys := fstest.MapFS{
		"root.xsd": {Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:t="urn:t" targetNamespace="urn:t" elementFormDefault="qualified">
<xs:include schemaLocation="common.xsd"/>
<xs:element name="doc"><xs:complexType><xs:sequence><xs:element ref="t:copyright"/><xs:element name="body" type="t:Text"/></xs:sequence></xs:complexType></xs:element>
</xs:schema>`)},
		"other.xsd": {Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:u="urn:u" targetNamespace="urn:u">
<xs:include schemaLocation="common.xsd"/>
<xs:element name="note" type="u:Text"/>
</xs:schema>`)},
		"common.xsd": {Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
<xs:element name="copyright" type="Holder"/>
<xs:complexType name="Holder"><xs:attribute name="year" type="Year" use="required"/></xs:complexType>
<xs:simpleType name="Year"><xs:restriction base="xs:gYear"/></xs:simpleType>
<xs:simpleType name="Text"><xs:restriction base="xs:string"><xs:maxLength value="5"/></xs:restriction></xs:simpleType>
</xs:schema>`)},
		"both.xsd": {Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:t="urn:t" targetNamespace="urn:t">
<xs:include schemaLocation="root.xsd"/>
<xs:import namespace="urn:u" schemaLocation="other.xsd"/>
</xs:schema>`)},
	}
	s, err := Load(fsys, "both.xsd")
	if err != nil {
		t.Fatal(err)
	}
	mutate(t, s, `<t:doc xmlns:t="urn:t"><t:copyright year="2026"/><t:body>hello</t:body></t:doc>`, []mutation{
		{"valid", "", "", ""},
		{"included type applies", "hello", "too long", "above the maximum 5"},
		{"included attribute type applies", "2026", "26", "not a valid gYear"},
		{"components are in the includer's namespace", `<t:copyright year="2026"/>`, `<copyright year="2026"/>`, "t:doc needs at least 1 copyright"},
	})
	mutate(t, s, `<u:note xmlns:u="urn:u">hi</u:note>`, []mutation{
		{"second namespace", "", "", ""},
		{"second namespace type", "hi", "far too long", "above the maximum 5"},
	})
}

// XML white space before the XML declaration, which some senders write,
// is accepted by default with a warning (IssueLeadingSpace) and refused
// with StrictProlog; schema documents with it load, with a load warning.
// Covers: SIG-IPE-2340
func TestLeadingSpace(t *testing.T) {
	s := loadSet(t)
	doc := []byte(" \t\r\n" + validLibrary)
	if err := s.Validate(doc); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	warn, err := s.ValidateDoc(doc, DocOptions{})
	if err != nil || len(warn) != 1 || warn[0].Kind != IssueLeadingSpace || warn[0].Line != 2 {
		t.Fatalf("default: %v, %v", warn, err)
	}
	warn, err = s.ValidateDoc(doc, DocOptions{StrictProlog: true})
	if err == nil || !strings.Contains(err.Error(), "must come first") || len(warn) != 0 {
		t.Fatalf("strict: %v, %v", warn, err)
	}
	if warn, err := s.ValidateDoc([]byte(validLibrary), DocOptions{StrictProlog: true}); err != nil || len(warn) != 0 {
		t.Fatalf("strict, declaration first: %v, %v", warn, err)
	}
	if _, err := s.ValidateDoc([]byte("<!-- c -->\n"+validLibrary), DocOptions{}); err == nil {
		t.Fatal("a comment before the declaration accepted")
	}
	head := "\n" + `<?xml version="1.0"?><xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:element name="e"/></xs:schema>`
	ls, err := Load(fstest.MapFS{"a.xsd": {Data: []byte(head)}}, "a.xsd")
	if err != nil {
		t.Fatal(err)
	}
	if w := ls.LoadWarnings(); len(w) != 1 || w[0].Kind != IssueLeadingSpace || w[0].Path != "a.xsd" {
		t.Fatalf("load warnings %v", w)
	}
	if w := s.LoadWarnings(); len(w) != 0 {
		t.Fatalf("clean set warned: %v", w)
	}
}
