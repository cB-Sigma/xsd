package xsd

import (
	"encoding/base64"
	"fmt"
	"math"
	"math/big"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Simple types: the built-in datatypes of XSD 1.0 Part 2 and user types
// derived from them by restriction, list and union.

const (
	varAtomic = iota
	varList
	varUnion
)

const (
	wsPreserve = iota
	wsReplace
	wsCollapse
)

// facetSet is the facets one restriction step declares.
type facetSet struct {
	enum                               []string
	patterns                           []*regexp.Regexp
	patternSrc                         []string
	length, minLength, maxLength       int // -1 when absent
	totalDigits, fractionDigits        int // -1 when absent
	minIncl, maxIncl, minExcl, maxExcl string
}

func newFacetSet() *facetSet {
	return &facetSet{length: -1, minLength: -1, maxLength: -1, totalDigits: -1, fractionDigits: -1}
}

// builtinBase is the derivation tree of the built-in types.
var builtinBase = map[string]string{
	"anySimpleType": "anyType",
	"string":        "anySimpleType", "normalizedString": "string", "token": "normalizedString",
	"language": "token", "Name": "token", "NCName": "Name", "ID": "NCName", "IDREF": "NCName",
	"ENTITY": "NCName", "NMTOKEN": "token",
	"IDREFS": "anySimpleType", "NMTOKENS": "anySimpleType", "ENTITIES": "anySimpleType",
	"boolean": "anySimpleType", "decimal": "anySimpleType", "float": "anySimpleType",
	"double": "anySimpleType", "duration": "anySimpleType", "dateTime": "anySimpleType",
	"time": "anySimpleType", "date": "anySimpleType", "gYearMonth": "anySimpleType",
	"gYear": "anySimpleType", "gMonthDay": "anySimpleType", "gDay": "anySimpleType",
	"gMonth": "anySimpleType", "hexBinary": "anySimpleType", "base64Binary": "anySimpleType",
	"anyURI": "anySimpleType", "QName": "anySimpleType", "NOTATION": "anySimpleType",
	"integer": "decimal", "nonPositiveInteger": "integer", "negativeInteger": "nonPositiveInteger",
	"long": "integer", "int": "long", "short": "int", "byte": "short",
	"nonNegativeInteger": "integer", "unsignedLong": "nonNegativeInteger",
	"unsignedInt": "unsignedLong", "unsignedShort": "unsignedInt", "unsignedByte": "unsignedShort",
	"positiveInteger": "nonNegativeInteger",
}

var builtinLists = map[string]string{"IDREFS": "IDREF", "NMTOKENS": "NMTOKEN", "ENTITIES": "ENTITY"}

var intBounds = map[string][2]string{
	"long": {"-9223372036854775808", "9223372036854775807"}, "int": {"-2147483648", "2147483647"},
	"short": {"-32768", "32767"}, "byte": {"-128", "127"},
	"unsignedLong": {"0", "18446744073709551615"}, "unsignedInt": {"0", "4294967295"},
	"unsignedShort": {"0", "65535"}, "unsignedByte": {"0", "255"},
	"nonNegativeInteger": {"0", ""}, "positiveInteger": {"1", ""},
	"nonPositiveInteger": {"", "0"}, "negativeInteger": {"", "-1"},
}

// primitive is the primitive ancestor of a built-in type.
func primitive(name string) string {
	for {
		b, ok := builtinBase[name]
		if !ok || b == "anySimpleType" || b == "anyType" {
			return name
		}
		name = b
	}
}

func builtinWS(name string) int {
	switch name {
	case "string", "anySimpleType":
		return wsPreserve
	case "normalizedString":
		return wsReplace
	}
	return wsCollapse
}

func applyWS(ws int, v string) string {
	switch ws {
	case wsReplace:
		return replaceWS(v)
	case wsCollapse:
		return collapse(v)
	}
	return v
}

func collapse(s string) string { return strings.Join(strings.FieldsFunc(s, isXMLSpace), " ") }

func isXMLSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }

func replaceWS(s string) string {
	return strings.Map(func(r rune) rune {
		if isXMLSpace(r) {
			return ' '
		}
		return r
	}, s)
}

var (
	reDecimal  = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)$`)
	reInteger  = regexp.MustCompile(`^[+-]?\d+$`)
	reFloat    = regexp.MustCompile(`^([+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?|-?INF|NaN)$`)
	reName     = regexp.MustCompile(`^[` + nameStart + `][` + nameChar + `]*$`)
	reNCName   = regexp.MustCompile(`^[\p{L}\p{Nl}_][\p{L}\p{Nl}\p{Mn}\p{Mc}\p{Nd}\p{Lm}._\-\x{B7}]*$`)
	reNMTOKEN  = regexp.MustCompile(`^[` + nameChar + `]+$`)
	reLanguage = regexp.MustCompile(`^[a-zA-Z]{1,8}(-[a-zA-Z0-9]{1,8})*$`)
	reDuration = regexp.MustCompile(`^-?P(\d+Y)?(\d+M)?(\d+D)?(T(\d+H)?(\d+M)?(\d+(\.\d+)?S)?)?$`)
	reHex      = regexp.MustCompile(`^([0-9a-fA-F]{2})*$`)
	tz         = `(Z|[+-]\d{2}:\d{2})?`
	reDateTime = regexp.MustCompile(`^(-?\d{4,})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(\.\d+)?` + tz + `$`)
	reDate     = regexp.MustCompile(`^(-?\d{4,})-(\d{2})-(\d{2})` + tz + `$`)
	reTime     = regexp.MustCompile(`^(\d{2}):(\d{2}):(\d{2})(\.\d+)?` + tz + `$`)
	reGYM      = regexp.MustCompile(`^(-?\d{4,})-(\d{2})` + tz + `$`)
	reGYear    = regexp.MustCompile(`^(-?\d{4,})` + tz + `$`)
	reGMD      = regexp.MustCompile(`^--(\d{2})-(\d{2})` + tz + `$`)
	reGDay     = regexp.MustCompile(`^---(\d{2})` + tz + `$`)
	reGMonth   = regexp.MustCompile(`^--(\d{2})(--)?` + tz + `$`)
)

func isNCName(s string) bool { return reNCName.MatchString(s) }

// builtinCheck checks the lexical form (and range) of a built-in atomic
// type; ctx resolves QName prefixes. It returns why the value is invalid.
func builtinCheck(name, v string, ctx *node) string {
	switch name {
	case "anySimpleType", "string", "normalizedString", "token":
		return ""
	case "language":
		if !reLanguage.MatchString(v) {
			return "is not a language tag"
		}
	case "Name":
		if !reName.MatchString(v) {
			return "is not an XML name"
		}
	case "NCName", "ID", "IDREF", "ENTITY":
		if !reNCName.MatchString(v) {
			return "is not a name without colon (NCName)"
		}
	case "NMTOKEN":
		if !reNMTOKEN.MatchString(v) {
			return "is not a name token (NMTOKEN)"
		}
	case "boolean":
		if v != "true" && v != "false" && v != "1" && v != "0" {
			return "is not a boolean (true, false, 1, 0)"
		}
	case "decimal":
		if !reDecimal.MatchString(v) {
			return "is not a decimal number"
		}
	case "float", "double":
		if !reFloat.MatchString(v) {
			return "is not a " + name
		}
		if strings.Contains(v, "INF") || v == "NaN" {
			return ""
		}
		bits := 64
		if name == "float" {
			bits = 32
		}
		if _, err := strconv.ParseFloat(v, bits); err != nil {
			return "is out of the range of " + name
		}
	case "duration":
		if !reDuration.MatchString(v) || strings.HasSuffix(v, "P") || strings.HasSuffix(v, "T") {
			return "is not a duration (PnYnMnDTnHnMnS)"
		}
	case "dateTime", "date", "time", "gYearMonth", "gYear", "gMonthDay", "gDay", "gMonth":
		return dateCheck(name, v)
	case "hexBinary":
		if !reHex.MatchString(v) {
			return "is not hexadecimal binary"
		}
	case "base64Binary":
		if _, err := base64.StdEncoding.Strict().DecodeString(strings.ReplaceAll(v, " ", "")); err != nil {
			return "is not base64"
		}
	case "anyURI":
		if _, err := url.Parse(strings.ReplaceAll(v, " ", "%20")); err != nil {
			return "is not a URI"
		}
	case "QName", "NOTATION":
		if ctx == nil {
			return ""
		}
		if _, err := ctx.resolveQName(v); err != nil {
			return "is not a QName in scope (" + err.Error() + ")"
		}
	default:
		if name == "integer" || intBounds[name] != [2]string{} {
			if !reInteger.MatchString(v) {
				return "is not an integer"
			}
			b := intBounds[name]
			x, _ := new(big.Int).SetString(strings.TrimPrefix(v, "+"), 10)
			if b[0] != "" {
				lo, _ := new(big.Int).SetString(b[0], 10)
				if x.Cmp(lo) < 0 {
					return fmt.Sprintf("is out of range, below the minimum %s of %s", b[0], name)
				}
			}
			if b[1] != "" {
				hi, _ := new(big.Int).SetString(b[1], 10)
				if x.Cmp(hi) > 0 {
					return fmt.Sprintf("is out of range, above the maximum %s of %s", b[1], name)
				}
			}
			return ""
		}
		return "has built-in type " + name + ", which is not supported"
	}
	return ""
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func daysIn(year, month int) int {
	switch month {
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

func checkYear(y string) string {
	d := strings.TrimPrefix(y, "-")
	if len(d) > 4 && d[0] == '0' {
		return "has a year with a leading zero"
	}
	if strings.Trim(d, "0") == "" {
		return "has year 0000"
	}
	return ""
}

func checkTZ(z string) string {
	if z == "" || z == "Z" {
		return ""
	}
	h, m := atoi(z[1:3]), atoi(z[4:6])
	if m > 59 || h > 14 || h == 14 && m != 0 {
		return "has a time zone out of range"
	}
	return ""
}

func checkClock(h, m, s, frac string) string {
	hh, mm, ss := atoi(h), atoi(m), atoi(s)
	if mm > 59 || ss > 59 || hh > 24 {
		return "has a time out of range"
	}
	if hh == 24 && (mm != 0 || ss != 0 || strings.Trim(frac, ".0") != "") {
		return "has hour 24 with a non-zero time"
	}
	return ""
}

func dateCheck(name, v string) string {
	bad := "is not a valid " + name
	var why string
	switch name {
	case "dateTime":
		m := reDateTime.FindStringSubmatch(v)
		if m == nil {
			return bad
		}
		why = firstOf(checkYear(m[1]), checkMonthDay(m[1], m[2], m[3]), checkClock(m[4], m[5], m[6], m[7]), checkTZ(m[8]))
	case "date":
		m := reDate.FindStringSubmatch(v)
		if m == nil {
			return bad
		}
		why = firstOf(checkYear(m[1]), checkMonthDay(m[1], m[2], m[3]), checkTZ(m[4]))
	case "time":
		m := reTime.FindStringSubmatch(v)
		if m == nil {
			return bad
		}
		why = firstOf(checkClock(m[1], m[2], m[3], m[4]), checkTZ(m[5]))
	case "gYearMonth":
		m := reGYM.FindStringSubmatch(v)
		if m == nil {
			return bad
		}
		why = firstOf(checkYear(m[1]), checkMonthDay("2000", m[2], "01"), checkTZ(m[3]))
	case "gYear":
		m := reGYear.FindStringSubmatch(v)
		if m == nil {
			return bad
		}
		why = firstOf(checkYear(m[1]), checkTZ(m[2]))
	case "gMonthDay":
		m := reGMD.FindStringSubmatch(v)
		if m == nil {
			return bad
		}
		why = firstOf(checkMonthDay("2000", m[1], m[2]), checkTZ(m[3]))
	case "gDay":
		m := reGDay.FindStringSubmatch(v)
		if m == nil {
			return bad
		}
		why = firstOf(checkMonthDay("2000", "01", m[1]), checkTZ(m[2]))
	case "gMonth":
		m := reGMonth.FindStringSubmatch(v)
		if m == nil {
			return bad
		}
		why = firstOf(checkMonthDay("2000", m[1], "01"), checkTZ(m[3]))
	}
	return why
}

func checkMonthDay(y, m, d string) string {
	mm, dd := atoi(m), atoi(d)
	if mm < 1 || mm > 12 {
		return "has a month out of range"
	}
	yy, _ := strconv.Atoi(y)
	if dd < 1 || dd > daysIn(yy, mm) {
		return "has a day out of range"
	}
	return ""
}

func firstOf(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// ---- value comparison for enumeration and range facets ---------------------

func isNumeric(prim string) bool { return prim == "decimal" || prim == "float" || prim == "double" }

// isOrderedTime reports the date, time and duration primitives, which
// range facets order.
func isOrderedTime(prim string) bool {
	switch prim {
	case "dateTime", "date", "time", "gYearMonth", "gYear", "gMonthDay", "gDay", "gMonth", "duration":
		return true
	}
	return false
}

// toTime places a date or time value on the time line, in UTC. A value
// without a time zone is taken as UTC (XSD 1.0 orders such values only
// partially; this is the usual simplification). The partial types take
// the reference year 1972 (a leap year) and month December where they
// have none, as XSD 1.0 appendix D does. Years before 1 are XSD's (no
// year 0: -0001 is the year before 0001).
func toTime(prim, v string) (time.Time, bool) {
	var y, mo, d, h, mi, s, frac, tz string
	m := (map[string]*regexp.Regexp{
		"dateTime": reDateTime, "date": reDate, "time": reTime, "gYearMonth": reGYM, "gYear": reGYear,
		"gMonthDay": reGMD, "gDay": reGDay, "gMonth": reGMonth,
	})[prim]
	if m == nil {
		return time.Time{}, false
	}
	g := m.FindStringSubmatch(v)
	if g == nil {
		return time.Time{}, false
	}
	y, mo, d = "1972", "12", "01"
	switch prim {
	case "dateTime":
		y, mo, d, h, mi, s, frac, tz = g[1], g[2], g[3], g[4], g[5], g[6], g[7], g[8]
	case "date":
		y, mo, d, tz = g[1], g[2], g[3], g[4]
	case "time":
		h, mi, s, frac, tz = g[1], g[2], g[3], g[4], g[5]
	case "gYearMonth":
		y, mo, tz = g[1], g[2], g[3]
	case "gYear":
		y, mo, tz = g[1], "01", g[2]
	case "gMonthDay":
		mo, d, tz = g[1], g[2], g[3]
	case "gDay":
		d, tz = g[1], g[2]
	case "gMonth":
		mo, tz = g[1], g[3]
	}
	year, err := strconv.Atoi(y)
	if err != nil {
		return time.Time{}, false
	}
	if year < 0 {
		year++ // XSD 1.0 has no year 0
	}
	ns := 0
	if frac != "" {
		f := (frac[1:] + "000000000")[:9]
		ns = atoi(f)
	}
	t := time.Date(year, time.Month(atoi(mo)), atoi(d), atoi(h), atoi(mi), atoi(s), ns, time.UTC)
	if tz != "" && tz != "Z" {
		off := time.Duration(atoi(tz[1:3]))*time.Hour + time.Duration(atoi(tz[4:6]))*time.Minute
		if tz[0] == '+' {
			off = -off
		}
		t = t.Add(off)
	}
	return t, true
}

// durationRefs are the four dateTimes XSD 1.0 (3.2.6.2) adds durations to
// in order to compare them.
var durationRefs = []time.Time{
	time.Date(1696, 9, 1, 0, 0, 0, 0, time.UTC),
	time.Date(1697, 2, 1, 0, 0, 0, 0, time.UTC),
	time.Date(1903, 3, 1, 0, 0, 0, 0, time.UTC),
	time.Date(1903, 7, 1, 0, 0, 0, 0, time.UTC),
}

// compareDuration orders two durations by adding each to the reference
// dateTimes; ok is false when the four results disagree (the order is
// partial: P1M and P30D are incomparable) or a value is out of range.
func compareDuration(a, b string) (int, bool) {
	res := 0
	for i, ref := range durationRefs {
		x, ok1 := addDuration(ref, a)
		y, ok2 := addDuration(ref, b)
		if !ok1 || !ok2 {
			return 0, false
		}
		c := x.Compare(y)
		if i > 0 && c != res {
			return 0, false
		}
		res = c
	}
	return res, true
}

func addDuration(t time.Time, v string) (time.Time, bool) {
	g := reDurFields.FindStringSubmatch(v)
	if g == nil {
		return time.Time{}, false
	}
	sign := 1
	if g[1] == "-" {
		sign = -1
	}
	n := func(s string) (int, bool) {
		if s == "" {
			return 0, true
		}
		x, err := strconv.Atoi(s)
		return x, err == nil && x < 1<<40
	}
	yy, ok1 := n(g[2])
	mm, ok2 := n(g[3])
	dd, ok3 := n(g[4])
	hh, ok4 := n(g[5])
	mi, ok5 := n(g[6])
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
		return time.Time{}, false
	}
	sec := 0.0
	if g[7] != "" {
		f, err := strconv.ParseFloat(g[7], 64)
		if err != nil || f > 1<<40 {
			return time.Time{}, false
		}
		sec = f
	}
	// Months first, then days and the clock, each from the day the months
	// lead to (the reference days are all the first of a month).
	t = t.AddDate(sign*yy, sign*mm, 0)
	total := float64(hh)*3600 + float64(mi)*60 + sec
	days := math.Floor(total / 86400)
	rest := total - days*86400
	t = t.AddDate(0, 0, sign*(dd+int(days)))
	return t.Add(time.Duration(float64(sign) * rest * float64(time.Second))), true
}

// compare orders two values of a primitive; ok is false when either does
// not parse or the pair is incomparable (NaN).
func compare(prim, a, b string) (int, bool) {
	switch {
	case prim == "decimal":
		x, ok1 := new(big.Rat).SetString(strings.TrimPrefix(a, "+"))
		y, ok2 := new(big.Rat).SetString(strings.TrimPrefix(b, "+"))
		if !ok1 || !ok2 {
			return 0, false
		}
		return x.Cmp(y), true
	case prim == "float" || prim == "double":
		x, ok1 := parseXSDFloat(a)
		y, ok2 := parseXSDFloat(b)
		if !ok1 || !ok2 || math.IsNaN(x) || math.IsNaN(y) {
			return 0, false
		}
		switch {
		case x < y:
			return -1, true
		case x > y:
			return 1, true
		}
		return 0, true
	case prim == "duration":
		return compareDuration(a, b)
	case isOrderedTime(prim):
		x, ok1 := toTime(prim, a)
		y, ok2 := toTime(prim, b)
		if !ok1 || !ok2 {
			return 0, false
		}
		return x.Compare(y), true
	}
	return 0, false
}

func parseXSDFloat(s string) (float64, bool) {
	switch s {
	case "INF":
		return math.Inf(1), true
	case "-INF":
		return math.Inf(-1), true
	case "NaN":
		return math.NaN(), true
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

// sameValue is value equality for enumeration facets.
func sameValue(prim, a, b string) bool {
	if isNumeric(prim) || isOrderedTime(prim) {
		if c, ok := compare(prim, a, b); ok {
			return c == 0
		}
	}
	return a == b
}

// digits counts the total and fraction digits of a decimal lexical form.
func digits(v string) (total, frac int) {
	v = strings.TrimLeft(v, "+-")
	ip, fp, _ := strings.Cut(v, ".")
	ip = strings.TrimLeft(ip, "0")
	fp = strings.TrimRight(fp, "0")
	total = len(ip) + len(fp)
	if total == 0 {
		total = 1
	}
	return total, len(fp)
}

// ---- checking a value against a simple type --------------------------------

// checkSimple validates a lexical value against a simple type. It returns
// the whitespace-normalised value and why the value is invalid ("" if it
// is valid).
func (s *Schema) checkSimple(t *xtype, raw string, ctx *node) (string, string) {
	var v string
	units := -1
	switch t.variety {
	case varUnion:
		v = collapse(raw)
		ok := false
		for _, m := range t.members {
			if _, why := s.checkSimple(m, raw, ctx); why == "" {
				ok = true
				break
			}
		}
		if !ok {
			return v, "is not a valid value of " + t.label() + " (no member type accepts it)"
		}
	case varList:
		v = collapse(raw)
		items := strings.Fields(v)
		for _, it := range items {
			if _, why := s.checkSimple(t.item, it, ctx); why != "" {
				return v, fmt.Sprintf("has list item %q that %s", it, why)
			}
		}
		units = len(items)
	default:
		v = applyWS(t.ws, raw)
		if why := builtinCheck(t.builtin, v, ctx); why != "" {
			return v, why
		}
	}
	prim := ""
	if t.variety == varAtomic {
		prim = primitive(t.builtin)
		if units < 0 {
			switch prim {
			case "hexBinary":
				units = len(v) / 2
			case "base64Binary":
				b, _ := base64.StdEncoding.DecodeString(strings.ReplaceAll(v, " ", ""))
				units = len(b)
			case "QName", "NOTATION":
			default:
				units = utf8.RuneCountInString(v)
			}
		}
	}
	for cur := t; cur != nil; cur = cur.base {
		if cur.facets == nil {
			continue
		}
		if why := checkFacets(cur.facets, prim, v, units, t.label()); why != "" {
			return v, why
		}
	}
	return v, ""
}

func checkFacets(f *facetSet, prim, v string, units int, label string) string {
	if len(f.enum) > 0 {
		ok := false
		for _, e := range f.enum {
			if sameValue(prim, v, e) {
				ok = true
				break
			}
		}
		if !ok {
			if len(f.enum) <= 10 {
				q := make([]string, len(f.enum))
				for i, e := range f.enum {
					q[i] = strconv.Quote(e)
				}
				return "is not one of " + strings.Join(q, ", ")
			}
			return fmt.Sprintf("is not a value of %s (none of its %d values)", label, len(f.enum))
		}
	}
	if len(f.patterns) > 0 {
		ok := false
		for _, re := range f.patterns {
			if re.MatchString(v) {
				ok = true
				break
			}
		}
		if !ok {
			return "does not match the pattern " + strings.Join(f.patternSrc, " | ")
		}
	}
	if units >= 0 {
		if f.length >= 0 && units != f.length {
			return fmt.Sprintf("has length %d, not %d", units, f.length)
		}
		if f.minLength >= 0 && units < f.minLength {
			return fmt.Sprintf("has length %d, below the minimum %d", units, f.minLength)
		}
		if f.maxLength >= 0 && units > f.maxLength {
			return fmt.Sprintf("has length %d, above the maximum %d", units, f.maxLength)
		}
	}
	for _, b := range []struct {
		lim  string
		ok   func(int) bool
		what string
	}{
		{f.minIncl, func(c int) bool { return c >= 0 }, "at least"},
		{f.maxIncl, func(c int) bool { return c <= 0 }, "at most"},
		{f.minExcl, func(c int) bool { return c > 0 }, "more than"},
		{f.maxExcl, func(c int) bool { return c < 0 }, "less than"},
	} {
		if b.lim == "" {
			continue
		}
		if c, ok := compare(prim, v, b.lim); !ok || !b.ok(c) {
			return "must be " + b.what + " " + b.lim
		}
	}
	if (f.totalDigits >= 0 || f.fractionDigits >= 0) && prim == "decimal" {
		td, fd := digits(v)
		if f.totalDigits >= 0 && td > f.totalDigits {
			return fmt.Sprintf("has %d digits, more than %d", td, f.totalDigits)
		}
		if f.fractionDigits >= 0 && fd > f.fractionDigits {
			return fmt.Sprintf("has %d fraction digits, more than %d", fd, f.fractionDigits)
		}
	}
	return ""
}

// ---- identity of values (identity constraints) ------------------------------

var (
	reTZ        = regexp.MustCompile(`(Z|[+-]\d{2}:\d{2})$`)
	reDurFields = regexp.MustCompile(`^(-?)P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)
)

// valueKey names a value of a simple type by its place in the value
// space, so that two lexical forms of one value get the same key: the
// primitive type, then a canonical form. Types derived from one primitive
// share its value space (an xs:int key matches an xs:integer keyref).
// ok is false when the value is not valid for the type.
func (s *Schema) valueKey(t *xtype, raw string, ctx *node) (string, bool) {
	v, why := s.checkSimple(t, raw, ctx)
	if why != "" {
		return "", false
	}
	switch t.variety {
	case varUnion:
		// the first member type that accepts the value gives its type
		for _, m := range t.members {
			if k, ok := s.valueKey(m, raw, ctx); ok {
				return k, true
			}
		}
		return "", false
	case varList:
		items := strings.Fields(v)
		keys := make([]string, len(items))
		for i, it := range items {
			k, ok := s.valueKey(t.item, it, ctx)
			if !ok {
				return "", false
			}
			keys[i] = k
		}
		return "list(" + strings.Join(keys, "\x02") + ")", true
	}
	prim := primitive(t.builtin)
	return prim + ":" + canonical(prim, v, ctx), true
}

// canonical is one spelling per value of a primitive type (v is valid and
// whitespace-normalised).
func canonical(prim, v string, ctx *node) string {
	switch prim {
	case "decimal":
		if x, ok := new(big.Rat).SetString(strings.TrimPrefix(v, "+")); ok {
			return x.RatString()
		}
	case "float", "double":
		bits := 64
		if prim == "float" {
			bits = 32
		}
		switch v {
		case "INF", "-INF", "NaN":
			return v
		}
		if f, err := strconv.ParseFloat(v, bits); err == nil {
			if f == 0 {
				f = 0 // -0 equals 0
			}
			return strconv.FormatFloat(f, 'g', -1, bits)
		}
	case "boolean":
		if v == "1" || v == "true" {
			return "true"
		}
		return "false"
	case "dateTime", "date", "time":
		// with a time zone: the instant; without: the local value (the
		// two are never equal)
		if t, ok := toTime(prim, v); ok {
			if reTZ.MatchString(v) {
				return "z" + t.UTC().Format(time.RFC3339Nano)
			}
			return "n" + t.Format(time.RFC3339Nano)
		}
	case "duration":
		if m := reDurFields.FindStringSubmatch(v); m != nil {
			months := atoi(m[2])*12 + atoi(m[3])
			secs := new(big.Rat)
			for i, mul := range []int64{86400, 3600, 60} {
				secs.Add(secs, new(big.Rat).SetInt64(int64(atoi(m[4+i]))*mul))
			}
			if m[7] != "" {
				x, _ := new(big.Rat).SetString(m[7])
				secs.Add(secs, x)
			}
			sign := ""
			if m[1] == "-" && (months != 0 || secs.Sign() != 0) {
				sign = "-"
			}
			return fmt.Sprintf("%s%d/%s", sign, months, secs.RatString())
		}
	case "hexBinary":
		return strings.ToUpper(v)
	case "base64Binary":
		if b, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(v, " ", "")); err == nil {
			return fmt.Sprintf("%X", b)
		}
	case "QName", "NOTATION":
		if ctx != nil {
			if q, err := ctx.resolveQName(v); err == nil {
				return q.String()
			}
		}
	}
	return v
}
