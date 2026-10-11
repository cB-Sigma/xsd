// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package xsd_test

import (
	"fmt"
	"testing/fstest"

	"github.com/cB-Sigma/xsd"
)

// Load a schema from a file system and validate two documents.
func Example() {
	fsys := fstest.MapFS{"meter.xsd": {Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="meter">
    <xs:complexType>
      <xs:sequence><xs:element name="reading" type="xs:decimal" maxOccurs="unbounded"/></xs:sequence>
      <xs:attribute name="id" type="xs:string" use="required"/>
    </xs:complexType>
  </xs:element>
</xs:schema>`)}}
	s, err := xsd.Load(fsys, "meter.xsd")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(s.Validate([]byte(`<meter id="m1"><reading>1.5</reading></meter>`)) == nil)
	fmt.Println(s.Validate([]byte(`<meter><reading>high</reading></meter>`)) == nil)
	// Output:
	// true
	// false
}
