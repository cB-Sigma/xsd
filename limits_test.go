package xsd

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func deepNotes(depth int) string {
	var b strings.Builder
	for range depth {
		b.WriteString(`<m:note xmlns:m="urn:sigma:test:main">`)
	}
	for range depth {
		b.WriteString(`</m:note>`)
	}
	return b.String()
}

func limitIssue(t *testing.T, err error, want string) {
	t.Helper()
	errs, ok := err.(*Errors)
	if !ok || len(*errs) != 1 {
		t.Fatalf("got %v, want one limit issue", err)
	}
	if is := (*errs)[0]; is.Kind != IssueLimit || !strings.Contains(is.Msg, want) {
		t.Fatalf("got %+v, want an IssueLimit mentioning %q", is, want)
	}
}

// Documents nested deeper than the limit (256 by default, adjustable) are
// refused while parsing, before any validation, in Validate and in
// ValidateTree alike; element and attribute counts and the size can be
// bounded too.
// Covers: SIG-IPE-2397
func TestLimits(t *testing.T) {
	s := loadSet(t)
	// within the default: a note may not contain notes, so this is a
	// content issue, not a limit
	if err := s.Validate([]byte(deepNotes(DefaultMaxDepth))); err == nil || strings.Contains(err.Error(), "limit") {
		t.Fatalf("depth %d: %v", DefaultMaxDepth, err)
	}
	start := time.Now()
	limitIssue(t, s.Validate([]byte(deepNotes(200000))), "deeper than 256")
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("refusing a 200,000-deep document took %v", d)
	}
	limitIssue(t, s.ValidateLimits([]byte(deepNotes(10)), Limits{MaxDepth: 5}), "deeper than 5")
	if err := s.ValidateLimits([]byte(deepNotes(300)), Limits{MaxDepth: -1}); err == nil || strings.Contains(err.Error(), "limit") {
		t.Fatalf("no depth limit: %v", err)
	}

	many := `<m:values xmlns:m="urn:sigma:test:main"><m:v>1</m:v><m:v>2</m:v><m:v>3</m:v></m:values>`
	if err := s.ValidateLimits([]byte(many), Limits{MaxElements: 4}); err != nil {
		t.Fatal(err)
	}
	limitIssue(t, s.ValidateLimits([]byte(many), Limits{MaxElements: 3}), "more than 3 elements")
	limitIssue(t, s.ValidateLimits([]byte(many), Limits{MaxBytes: 20}), "larger than 20 bytes")
	attrs := `<m:values xmlns:m="urn:sigma:test:main" xmlns:a="urn:a" a:x="1"/>`
	limitIssue(t, s.ValidateLimits([]byte(attrs), Limits{MaxAttrs: 2}), "more than 2 attributes")

	// ValidateTree: the caller's tree is held to the same depth limit
	var root, cur *Element
	for range 300 {
		e := &Element{Space: "urn:sigma:test:main", Local: "note", Line: 1}
		if root == nil {
			root = e
		} else {
			cur.Children = append(cur.Children, e)
		}
		cur = e
	}
	is := s.ValidateTree(root, Options{})
	if len(is) != 1 || is[0].Kind != IssueLimit {
		t.Fatalf("tree: %v", is)
	}
	if is := s.ValidateTree(root, Options{Limits: Limits{MaxDepth: 1000}}); len(is) == 0 || is[0].Kind == IssueLimit {
		t.Fatalf("tree with a raised limit: %v", is)
	}

	// schema documents are parsed under the default depth limit too
	var x strings.Builder
	x.WriteString(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">`)
	for range 300 {
		x.WriteString(`<xs:annotation>`)
	}
	for range 300 {
		x.WriteString(`</xs:annotation>`)
	}
	x.WriteString(`</xs:schema>`)
	if _, err := Load(fstest.MapFS{"deep.xsd": {Data: []byte(x.String())}}, "deep.xsd"); err == nil || !strings.Contains(err.Error(), "deeper than 256") {
		t.Fatalf("deep schema document: %v", err)
	}
}

// Costs stay linear in the document: namespace declarations on many
// elements under a root with many of its own, and long runs of children
// whose content does not match at the end.
// Covers: SIG-IPE-2397
func TestLinearCost(t *testing.T) {
	s := loadSet(t)
	var b strings.Builder
	b.WriteString(`<m:values xmlns:m="urn:sigma:test:main"`)
	for i := range 2000 {
		fmt.Fprintf(&b, ` xmlns:p%d="urn:%d"`, i, i)
	}
	b.WriteString(`>`)
	for i := range 10000 {
		fmt.Fprintf(&b, `<m:v xmlns:q="urn:q%d">x</m:v>`, i)
	}
	b.WriteString(`</m:values>`)
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	if err := s.Validate([]byte(b.String())); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&m1)
	if mb := (m1.TotalAlloc - m0.TotalAlloc) >> 20; mb > 200 {
		t.Errorf("%d KB of namespace declarations allocated %d MB", b.Len()>>10, mb)
	}

	b.Reset()
	b.WriteString(`<m:values xmlns:m="urn:sigma:test:main">`)
	for range 100000 {
		b.WriteString(`<m:v>x</m:v>`)
	}
	b.WriteString(`<m:bad/></m:values>`)
	start := time.Now()
	if err := s.Validate([]byte(b.String())); err == nil || !strings.Contains(err.Error(), "m:bad is not expected") {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("100,000 children with a bad last one took %v", d)
	}
}
