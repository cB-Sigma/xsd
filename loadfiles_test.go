// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package xsd

import (
	"strings"
	"testing"
	"testing/fstest"
)

// LoadFiles reads one schema from several roots in different namespaces;
// a root that another root imports is read once.
func TestLoadFilesSeveralRoots(t *testing.T) {
	fsys := fstest.MapFS{
		"a.xsd": {Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:a" xmlns:b="urn:b" elementFormDefault="qualified">
  <xs:import namespace="urn:b" schemaLocation="b.xsd"/>
  <xs:element name="a"><xs:complexType><xs:sequence><xs:element ref="b:b"/></xs:sequence></xs:complexType></xs:element>
</xs:schema>`)},
		"b.xsd": {Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:b">
  <xs:element name="b" type="xs:int"/>
</xs:schema>`)},
		"c.xsd": {Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="c" type="xs:boolean"/>
</xs:schema>`)},
	}
	s, err := LoadFiles(fsys, "a.xsd", "c.xsd", "b.xsd")
	if err != nil {
		t.Fatal(err)
	}
	for doc, want := range map[string]string{
		`<a xmlns="urn:a"><b xmlns="urn:b">7</b></a>`: "",
		`<b xmlns="urn:b">7</b>`:                      "",
		`<c>true</c>`:                                 "",
		`<c>maybe</c>`:                                "boolean",
		`<d/>`:                                        "not a global element",
	} {
		err := s.Validate([]byte(doc))
		switch {
		case want == "" && err != nil:
			t.Errorf("%s: %v", doc, err)
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("%s: %v, want %q", doc, err, want)
		}
	}
	if _, err := LoadFiles(fsys); err == nil {
		t.Error("LoadFiles without a root succeeded")
	}
}
