// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package xsd

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

var hintFS = fstest.MapFS{
	"schemas/meter.xsd": {Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:meter" xmlns="urn:meter" elementFormDefault="qualified">
  <xs:element name="meter"><xs:complexType><xs:sequence>
    <xs:element name="kWh" type="xs:decimal"/>
    <xs:any namespace="##other" processContents="strict" minOccurs="0"/>
  </xs:sequence></xs:complexType></xs:element>
</xs:schema>`)},
	"schemas/tag.xsd": {Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:tag">
  <xs:element name="tag" type="xs:NCName"/>
</xs:schema>`)},
	"schemas/other-meter.xsd": {Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:meter">
  <xs:element name="meter" type="xs:string"/>
</xs:schema>`)},
	"schemas/nons.xsd": {Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:element name="n" type="xs:int"/></xs:schema>`)},
}

// Hints lists xsi:schemaLocation and xsi:noNamespaceSchemaLocation on any
// element; LoadHinted assembles the schema from them (relative to the
// document), with root documents winning over hints for their namespace
// unless Override; Validate never follows hints.
//
// Covers: SIG-IPE-2397
func TestLoadHinted(t *testing.T) {
	doc := []byte(`<meter xmlns="urn:meter" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
	  xsi:schemaLocation="urn:meter ../schemas/meter.xsd"><kWh>1.5</kWh>
	  <t:tag xmlns:t="urn:tag" xsi:schemaLocation="urn:tag ../schemas/tag.xsd">a1</t:tag></meter>`)
	hs, err := Hints(doc)
	if err != nil || len(hs) != 2 || hs[1] != (Hint{"urn:tag", "../schemas/tag.xsd"}) {
		t.Fatalf("Hints = %v, %v", hs, err)
	}
	s, err := LoadHinted(hintFS, HintOptions{DocPath: "docs/m.xml"}, doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(doc); err != nil {
		t.Fatalf("hinted schema: %v", err)
	}
	bad := []byte(strings.Replace(string(doc), ">a1<", ">1 a<", 1))
	if err := s.Validate(bad); err == nil {
		t.Fatal("the hinted tag schema was not applied")
	}

	// the caller's root document wins for its namespace; the strict
	// wildcard then needs the hinted tag schema
	s, err = LoadHinted(hintFS, HintOptions{DocPath: "docs/m.xml"}, doc, "schemas/meter.xsd")
	if err != nil || s.Validate(doc) != nil {
		t.Fatalf("roots and hints: %v", err)
	}
	clash := []byte(strings.Replace(string(doc), "../schemas/meter.xsd", "../schemas/other-meter.xsd", 1))
	if s, err = LoadHinted(hintFS, HintOptions{DocPath: "docs/m.xml"}, clash, "schemas/meter.xsd"); err != nil || s.Validate(clash) != nil {
		t.Fatalf("a hint for the root's namespace must be ignored: %v", err)
	}
	if _, err = LoadHinted(hintFS, HintOptions{DocPath: "docs/m.xml", Override: true}, clash, "schemas/meter.xsd"); err == nil {
		t.Fatal("Override loaded two declarations of meter")
	}

	// a plain schema ignores the hints
	plain, err := Load(hintFS, "schemas/meter.xsd")
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.Validate(doc); err == nil {
		t.Fatal("Validate followed a hint")
	}

	// noNamespaceSchemaLocation, and locations outside the files or by URL
	nn := []byte(`<n xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:noNamespaceSchemaLocation="nons.xsd">7</n>`)
	if s, err := LoadHinted(hintFS, HintOptions{DocPath: "schemas/n.xml"}, nn); err != nil || s.Validate(nn) != nil {
		t.Fatalf("noNamespaceSchemaLocation: %v", err)
	}
	up := []byte(`<n xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:noNamespaceSchemaLocation="../../etc/x.xsd">7</n>`)
	if _, err := LoadHinted(hintFS, HintOptions{DocPath: "schemas/n.xml"}, up); err == nil {
		t.Fatal("a location outside the files was read")
	}
	web := []byte(`<n xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:noNamespaceSchemaLocation="https://example.com/nons.xsd">7</n>`)
	if _, err := LoadHinted(hintFS, HintOptions{DocPath: "n.xml"}, web); err == nil || !strings.Contains(err.Error(), "Fetch") {
		t.Fatalf("a URL without Fetch: %v", err)
	}
	fetched := ""
	opt := HintOptions{DocPath: "n.xml", LoadOptions: LoadOptions{Fetch: func(u string) ([]byte, error) {
		fetched = u
		return hintFS["schemas/nons.xsd"].Data, nil
	}}}
	if s, err := LoadHinted(hintFS, opt, web); err != nil || s.Validate(web) != nil || fetched != "https://example.com/nons.xsd" {
		t.Fatalf("a URL through Fetch: %v (%q)", err, fetched)
	}
	opt.Fetch = func(string) ([]byte, error) { return nil, errors.New("offline") }
	if _, err := LoadHinted(hintFS, opt, web); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("a failed fetch: %v", err)
	}
}

// XSD 1.0: a new location for a namespace after an element of it has been
// met is an error; repeating the same hint is not.
//
// Covers: SIG-IPE-2397
func TestHintsLate(t *testing.T) {
	late := []byte(`<m:a xmlns:m="urn:m" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="urn:m a.xsd">
	  <m:b xsi:schemaLocation="urn:m b.xsd"/></m:a>`)
	if _, err := Hints(late); err == nil {
		t.Fatal("late hint accepted")
	}
	same := []byte(`<m:a xmlns:m="urn:m" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="urn:m a.xsd">
	  <m:b xsi:schemaLocation="urn:m a.xsd"/><o:c xmlns:o="urn:o" xsi:schemaLocation="urn:o o.xsd"/></m:a>`)
	if hs, err := Hints(same); err != nil || len(hs) != 2 {
		t.Fatalf("Hints = %v, %v", hs, err)
	}
	if _, err := Hints([]byte(`<a xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="urn:m"/>`)); err == nil {
		t.Fatal("odd schemaLocation accepted")
	}
}
