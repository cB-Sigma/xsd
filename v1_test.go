package xsd

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

const v1Schema = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:element name="a" type="xs:int"/></xs:schema>`

func TestMalformedDocumentHasItsOwnKind(t *testing.T) {
	s, err := Load(fstest.MapFS{"a.xsd": {Data: []byte(v1Schema)}}, "a.xsd")
	if err != nil {
		t.Fatal(err)
	}
	err = s.Validate([]byte(`<a>1</b>`))
	var es *Errors
	if !errors.As(err, &es) || len(*es) != 1 || (*es)[0].Kind != IssueMalformed {
		t.Fatalf("got %v, want one IssueMalformed", err)
	}
	err = s.Validate([]byte(`<a>x</a>`))
	if !errors.As(err, &es) || (*es)[0].Kind != IssueValue {
		t.Fatalf("got %v, want IssueValue", err)
	}
}

func TestIssueKindString(t *testing.T) {
	for k := IssueOther; k <= IssueMalformed; k++ {
		if s := k.String(); s == "" || strings.HasPrefix(s, "IssueKind(") {
			t.Errorf("%d has no name", int(k))
		}
	}
	if got := IssueKind(99).String(); got != "IssueKind(99)" {
		t.Errorf("unknown kind: %q", got)
	}
}

func TestErrorsCarryThePackagePrefix(t *testing.T) {
	_, err := Load(fstest.MapFS{}, "missing.xsd")
	if err == nil || !strings.HasPrefix(err.Error(), "xsd: ") {
		t.Errorf("Load: %v", err)
	}
	if _, err := Hints([]byte(`<a`)); err == nil || !strings.HasPrefix(err.Error(), "xsd: ") {
		t.Errorf("Hints: %v", err)
	}
	if _, err := LoadZip([]byte("not a zip"), "a.xsd"); err == nil || !strings.HasPrefix(err.Error(), "xsd: ") {
		t.Errorf("LoadZip: %v", err)
	}
	s, _ := Load(fstest.MapFS{"a.xsd": {Data: []byte(v1Schema)}}, "a.xsd")
	if err := s.Validate([]byte(`<b/>`)); err == nil || !strings.HasPrefix(err.Error(), "xsd: ") {
		t.Errorf("Validate: %v", err)
	}
}
