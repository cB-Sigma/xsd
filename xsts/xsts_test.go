// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

// TestXSTS runs the W3C XML Schema Test Suite (XSTS,
// github.com/w3c/xsdtests) against github.com/cB-Sigma/xsd. The suite is
// not copied here (it is large and W3C-licensed); point XSTS_DIR at a
// checkout of its master branch to run it:
//
//	XSTS_DIR=/path/to/xsdtests go test -v ./...
//
// Only tests that apply to an XSD 1.0 processor are run. The test fails
// when a test outside testdata/known-failures.txt fails, or when a listed
// test now passes, so the list always matches the code. XSTS_UPDATE=1
// rewrites the list instead.

package xsts

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cB-Sigma/xsd"
)

// supported are the XSTS version tokens this processor supports: XSD 1.0
// (second edition) over XML 1.0.
var supported = map[string]bool{"1.0": true, "1.0-2e": true, "XML-1.0": true, "XML-1.0-1e-4e": true}

type suiteFile struct {
	Refs []struct {
		Href string `xml:"http://www.w3.org/1999/xlink href,attr"`
	} `xml:"testSetRef"`
}

type testSet struct {
	Name    string      `xml:"name,attr"`
	Version string      `xml:"version,attr"`
	Groups  []testGroup `xml:"testGroup"`
}

type testGroup struct {
	Name      string     `xml:"name,attr"`
	Version   string     `xml:"version,attr"`
	Schema    *testCase  `xml:"schemaTest"`
	Instances []testCase `xml:"instanceTest"`
}

type testCase struct {
	Name    string `xml:"name,attr"`
	Version string `xml:"version,attr"`
	Docs    []struct {
		Href string `xml:"http://www.w3.org/1999/xlink href,attr"`
	} `xml:",any"`
	Expected []struct {
		Validity string `xml:"validity,attr"`
		Version  string `xml:"version,attr"`
	} `xml:"expected"`
	Current *struct {
		Status string `xml:"status,attr"`
	} `xml:"current"`
}

// applies reports whether a version attribute (tokens joined by "or")
// admits this processor.
func applies(v string) bool {
	f := strings.Fields(v)
	if len(f) == 0 {
		return true
	}
	for _, t := range f {
		if supported[t] {
			return true
		}
	}
	return false
}

// expectation picks the expected result that applies (its tokens joined
// by "and").
func (c *testCase) expectation() string {
	for _, e := range c.Expected {
		ok := true
		for _, t := range strings.Fields(e.Version) {
			if !supported[t] {
				ok = false
			}
		}
		if ok {
			return e.Validity
		}
	}
	return ""
}

// docs lists the schemaDocument or instanceDocument references.
func (c *testCase) docs() []string {
	var out []string
	for _, d := range c.Docs {
		if d.Href != "" {
			out = append(out, d.Href)
		}
	}
	return out
}

type tally struct {
	pass, fail int
}

// row is one line of the summary: a test set, by kind of test.
type row struct {
	schemaValid, schemaInvalid, instValid, instInvalid tally
	skipped                                            int
}

func (r *row) add(kind string, expected string, ok bool) {
	var t *tally
	switch {
	case kind == "schema" && expected == "valid":
		t = &r.schemaValid
	case kind == "schema":
		t = &r.schemaInvalid
	case expected == "valid":
		t = &r.instValid
	default:
		t = &r.instInvalid
	}
	if ok {
		t.pass++
	} else {
		t.fail++
	}
}

func (r *row) total() tally {
	var t tally
	for _, x := range []tally{r.schemaValid, r.schemaInvalid, r.instValid, r.instInvalid} {
		t.pass += x.pass
		t.fail += x.fail
	}
	return t
}

// outcome is what happened to one test.
type outcome struct {
	id     string
	ok     bool
	reason string
}

func TestXSTS(t *testing.T) {
	dir := os.Getenv("XSTS_DIR")
	if dir == "" {
		t.Skip("XSTS_DIR is not set (a checkout of github.com/w3c/xsdtests)")
	}
	fsys := os.DirFS(dir)
	raw, err := fs.ReadFile(fsys, "suite.xml")
	if err != nil {
		t.Fatal(err)
	}
	var suite suiteFile
	if err := xml.Unmarshal(raw, &suite); err != nil {
		t.Fatal(err)
	}

	rows := map[string]*row{}
	var order []string
	var results []outcome
	start := time.Now()
	for _, ref := range suite.Refs {
		file := path.Clean(ref.Href)
		raw, err := fs.ReadFile(fsys, file)
		if err != nil {
			t.Fatal(err)
		}
		var set testSet
		if err := xml.Unmarshal(raw, &set); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		r := &row{}
		rows[file] = r
		order = append(order, file)
		if !applies(set.Version) {
			continue
		}
		base := path.Dir(file)
		setStart := time.Now()
		for gi := range set.Groups {
			g := &set.Groups[gi]
			if !applies(g.Version) {
				continue
			}
			results = append(results, runGroup(fsys, base, set.Name, g, r)...)
		}
		if testing.Verbose() {
			t.Logf("%s: %d run, %d skipped, %s", file, r.total().pass+r.total().fail, r.skipped, time.Since(setStart).Round(time.Millisecond))
		}
	}
	elapsed := time.Since(start)

	// Summary by test set and by contributor directory.
	var b strings.Builder
	byDir := map[string]*row{}
	var dirs []string
	fmt.Fprintf(&b, "\n%-45s %13s %13s %13s %13s %13s %7s\n", "test set", "schema valid", "schema inval", "inst valid", "inst invalid", "total", "skipped")
	cell := func(x tally) string {
		if x.pass+x.fail == 0 {
			return "-"
		}
		return fmt.Sprintf("%d/%d", x.pass, x.pass+x.fail)
	}
	addTo := func(dst *row, src *row) {
		for _, p := range [][2]*tally{{&dst.schemaValid, &src.schemaValid}, {&dst.schemaInvalid, &src.schemaInvalid}, {&dst.instValid, &src.instValid}, {&dst.instInvalid, &src.instInvalid}} {
			p[0].pass += p[1].pass
			p[0].fail += p[1].fail
		}
		dst.skipped += src.skipped
	}
	all := &row{}
	for _, f := range order {
		r := rows[f]
		d := path.Dir(f)
		if byDir[d] == nil {
			byDir[d] = &row{}
			dirs = append(dirs, d)
		}
		addTo(byDir[d], r)
		addTo(all, r)
		if r.total().pass+r.total().fail+r.skipped == 0 {
			continue
		}
		fmt.Fprintf(&b, "%-45s %13s %13s %13s %13s %13s %7d\n", f, cell(r.schemaValid), cell(r.schemaInvalid), cell(r.instValid), cell(r.instInvalid), cell(r.total()), r.skipped)
	}
	fmt.Fprintf(&b, "\n%-45s %13s %13s %13s %13s %13s %7s\n", "contributor", "schema valid", "schema inval", "inst valid", "inst invalid", "total", "skipped")
	for _, d := range dirs {
		r := byDir[d]
		if r.total().pass+r.total().fail+r.skipped == 0 {
			continue
		}
		fmt.Fprintf(&b, "%-45s %13s %13s %13s %13s %13s %7d\n", d, cell(r.schemaValid), cell(r.schemaInvalid), cell(r.instValid), cell(r.instInvalid), cell(r.total()), r.skipped)
	}
	fmt.Fprintf(&b, "%-45s %13s %13s %13s %13s %13s %7d\n", "all", cell(all.schemaValid), cell(all.schemaInvalid), cell(all.instValid), cell(all.instInvalid), cell(all.total()), all.skipped)
	fmt.Fprintf(&b, "run in %s\n", elapsed.Round(time.Second))
	t.Log(b.String())

	// Reasons, grouped, for the report.
	reasons := map[string]int{}
	for _, o := range results {
		if !o.ok {
			reasons[o.reason]++
		}
	}
	type rc struct {
		r string
		n int
	}
	var rcs []rc
	for r, n := range reasons {
		rcs = append(rcs, rc{r, n})
	}
	sort.Slice(rcs, func(i, j int) bool { return rcs[i].n > rcs[j].n || rcs[i].n == rcs[j].n && rcs[i].r < rcs[j].r })
	var rb strings.Builder
	for i, x := range rcs {
		if i == 60 {
			break
		}
		fmt.Fprintf(&rb, "%6d  %s\n", x.n, x.r)
	}
	t.Log("most common failure reasons:\n" + rb.String())

	checkKnown(t, results)
}

// runGroup runs a group's schema test and instance tests.
func runGroup(fsys fs.FS, base, setName string, g *testGroup, r *row) []outcome {
	var out []outcome
	var schema *xsd.Schema
	var loadErr error
	if g.Schema != nil {
		docs := g.Schema.docs()
		for i := range docs {
			docs[i] = path.Join(base, docs[i])
		}
		schema, loadErr = load(fsys, docs)
		c := g.Schema
		id := setName + "/" + g.Name + "/" + c.Name
		if !applies(c.Version) || !runnable(c) {
			r.skipped++
		} else {
			exp := c.expectation()
			ok := (loadErr == nil) == (exp == "valid")
			reason := ""
			if !ok {
				if loadErr != nil {
					reason = "schema refused: " + short(loadErr.Error())
				} else {
					reason = "invalid schema accepted"
				}
			}
			r.add("schema", exp, ok)
			out = append(out, outcome{id, ok, reason})
		}
	}
	for i := range g.Instances {
		c := &g.Instances[i]
		id := setName + "/" + g.Name + "/" + c.Name
		if !applies(c.Version) || !runnable(c) || g.Schema == nil {
			// Without a schema test the instance names its schema
			// through xsi:schemaLocation, which this module does not
			// follow.
			r.skipped++
			continue
		}
		exp := c.expectation()
		docs := c.docs()
		if len(docs) != 1 {
			r.skipped++
			continue
		}
		var ok bool
		var reason string
		if loadErr != nil {
			ok = false
			reason = "schema not loaded"
		} else {
			doc, err := fs.ReadFile(fsys, path.Join(base, docs[0]))
			if err != nil {
				ok, reason = false, "instance not readable"
			} else {
				verr := validate(schema, doc)
				ok = (verr == nil) == (exp == "valid")
				if !ok {
					if verr != nil {
						reason = "valid instance refused: " + short(verr.Error())
					} else {
						reason = "invalid instance accepted"
					}
				}
			}
		}
		r.add("instance", exp, ok)
		out = append(out, outcome{id, ok, reason})
	}
	return out
}

// runnable reports whether a test has an accepted expectation for XSD 1.0.
func runnable(c *testCase) bool {
	if c.Current != nil {
		switch c.Current.Status {
		case "", "accepted", "stable":
		default:
			return false // queried or disputed
		}
	}
	switch c.expectation() {
	case "valid", "invalid":
		return true
	}
	return false
}

func load(fsys fs.FS, docs []string) (s *xsd.Schema, err error) {
	defer func() {
		if p := recover(); p != nil {
			s, err = nil, fmt.Errorf("PANIC: %v", p)
		}
	}()
	if len(docs) == 1 {
		return xsd.Load(fsys, docs[0])
	}
	return xsd.LoadFiles(fsys, docs...)
}

func validate(s *xsd.Schema, doc []byte) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("PANIC: %v", p)
		}
	}()
	return s.Validate(doc)
}

// short reduces an error to its kind, without file names, line numbers
// and values, so similar failures group together.
func short(msg string) string {
	if i := strings.Index(msg, "; "); i > 0 {
		msg = msg[:i]
	}
	msg = strings.TrimPrefix(msg, "schema validation: ")
	f := strings.Fields(msg)
	var keep []string
	for _, w := range f {
		if strings.HasSuffix(w, ".xsd") || strings.HasSuffix(w, ".xsd:") || strings.HasPrefix(w, "\"") || strings.HasPrefix(w, "/") || w == "line" || isNum(strings.TrimSuffix(w, ":")) {
			continue
		}
		keep = append(keep, w)
		if len(keep) == 9 {
			break
		}
	}
	return strings.Join(keep, " ")
}

func isNum(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// checkKnown compares the failures with testdata/known-failures.txt.
func checkKnown(t *testing.T, results []outcome) {
	t.Helper()
	file := filepath.Join("testdata", "known-failures.txt")
	if os.Getenv("XSTS_UPDATE") != "" {
		var b strings.Builder
		b.WriteString("# XSTS tests github.com/cB-Sigma/xsd fails, one per line: test set/group/test<TAB>reason.\n")
		b.WriteString("# Rewritten by XSTS_UPDATE=1 go test; see the README for the analysis.\n")
		for _, o := range results {
			if !o.ok {
				b.WriteString(o.id + "\t" + o.reason + "\n")
			}
		}
		if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	known := map[string]bool{}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, _, _ := strings.Cut(line, "\t")
		known[id] = true
	}
	var newFail, nowPass []string
	for _, o := range results {
		switch {
		case !o.ok && !known[o.id]:
			newFail = append(newFail, o.id+": "+o.reason)
		case o.ok && known[o.id]:
			nowPass = append(nowPass, o.id)
		}
	}
	for i, s := range newFail {
		if i == 50 {
			t.Errorf("and %d more new failures", len(newFail)-50)
			break
		}
		t.Errorf("new failure: %s", s)
	}
	for i, s := range nowPass {
		if i == 50 {
			t.Errorf("and %d more", len(nowPass)-50)
			break
		}
		t.Errorf("listed as failing but passes (remove it from the list): %s", s)
	}
}
