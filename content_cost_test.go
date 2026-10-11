// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package xsd

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// Nested unbounded groups over thousands of children are checked in
// bounded time, valid or not (found by the W3C XML Schema Test Suite: a
// document of about 2,000 children took minutes).
func TestNestedRepetitionCost(t *testing.T) {
	fsys := fstest.MapFS{"s.xsd": {Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="r"><xs:complexType>
    <xs:sequence maxOccurs="unbounded">
      <xs:choice maxOccurs="unbounded">
        <xs:sequence minOccurs="0" maxOccurs="unbounded"><xs:element name="a" minOccurs="0" maxOccurs="unbounded"/></xs:sequence>
        <xs:element name="b" minOccurs="0" maxOccurs="unbounded"/>
      </xs:choice>
    </xs:sequence>
  </xs:complexType></xs:element>
</xs:schema>`)}}
	s, err := Load(fsys, "s.xsd")
	if err != nil {
		t.Fatal(err)
	}
	kids := strings.Repeat("<a/><b/>", 1500)
	for _, c := range []struct{ doc, want string }{
		{"<r>" + kids + "</r>", ""},
		{"<r>" + kids + "<c/></r>", "c is not expected in r"},
	} {
		start := time.Now()
		err := s.Validate([]byte(c.doc))
		d := time.Since(start)
		if d > 20*time.Second {
			t.Errorf("validation took %s", d)
		}
		t.Logf("%s: %v", d, err)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("valid content refused: %v", err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("got %v, want an issue naming %q", err, c.want)
		}
	}
}
