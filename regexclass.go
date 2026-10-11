// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package xsd

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Character classes that RE2 cannot write directly (subtraction such as
// [a-z-[aeiou]], \P{IsBlock}, \I or \C inside a class) are computed as
// sets of code point ranges and written out range by range.

type runeRange struct{ lo, hi rune }

// runeSet is a sorted list of disjoint, non-adjacent ranges.
type runeSet []runeRange

func normalize(rs []runeRange) runeSet {
	sort.Slice(rs, func(i, j int) bool { return rs[i].lo < rs[j].lo })
	var out runeSet
	for _, r := range rs {
		if r.lo > r.hi {
			continue
		}
		if n := len(out); n > 0 && r.lo <= out[n-1].hi+1 {
			if r.hi > out[n-1].hi {
				out[n-1].hi = r.hi
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

func union(a, b runeSet) runeSet {
	return normalize(append(append([]runeRange(nil), a...), b...))
}

func negate(a runeSet) runeSet {
	var out runeSet
	next := rune(0)
	for _, r := range a {
		if r.lo > next {
			out = append(out, runeRange{next, r.lo - 1})
		}
		next = r.hi + 1
	}
	if next <= unicode.MaxRune {
		out = append(out, runeRange{next, unicode.MaxRune})
	}
	return out
}

func subtract(a, b runeSet) runeSet {
	// a minus b is a and not b
	nb := negate(b)
	var out runeSet
	i, j := 0, 0
	for i < len(a) && j < len(nb) {
		lo, hi := max(a[i].lo, nb[j].lo), min(a[i].hi, nb[j].hi)
		if lo <= hi {
			out = append(out, runeRange{lo, hi})
		}
		if a[i].hi < nb[j].hi {
			i++
		} else {
			j++
		}
	}
	return out
}

func tableSet(t *unicode.RangeTable) runeSet {
	var rs []runeRange
	for _, r := range t.R16 {
		for c := rune(r.Lo); c <= rune(r.Hi); c += rune(r.Stride) {
			if r.Stride == 1 {
				rs = append(rs, runeRange{rune(r.Lo), rune(r.Hi)})
				break
			}
			rs = append(rs, runeRange{c, c})
		}
	}
	for _, r := range t.R32 {
		for c := rune(r.Lo); c <= rune(r.Hi); c += rune(r.Stride) {
			if r.Stride == 1 {
				rs = append(rs, runeRange{rune(r.Lo), rune(r.Hi)})
				break
			}
			rs = append(rs, runeRange{c, c})
		}
	}
	return normalize(rs)
}

func chars(cs ...rune) runeSet {
	rs := make([]runeRange, len(cs))
	for i, c := range cs {
		rs[i] = runeRange{c, c}
	}
	return normalize(rs)
}

func categories(names ...string) runeSet {
	var s runeSet
	for _, n := range names {
		s = union(s, tableSet(unicode.Categories[n]))
	}
	return s
}

// String writes the set as the inside of an RE2 class.
func (s runeSet) String() string {
	var b strings.Builder
	for _, r := range s {
		if r.lo == r.hi {
			fmt.Fprintf(&b, `\x{%X}`, r.lo)
		} else {
			fmt.Fprintf(&b, `\x{%X}-\x{%X}`, r.lo, r.hi)
		}
	}
	return b.String()
}

// class writes the set as an RE2 class.
func (s runeSet) class() string {
	if len(s) == 0 {
		return `[^\x{0}-\x{10FFFF}]` // matches nothing
	}
	return "[" + s.String() + "]"
}

// escapeSet is the set of a multi-character escape (\d, \s, \w, \i, \c,
// their capitals, \p{..}, \P{..}); name is the property name for p and P.
func escapeSet(e rune, name string) (runeSet, error) {
	switch e {
	case 'd':
		return categories("Nd"), nil
	case 'D':
		return negate(categories("Nd")), nil
	case 's':
		return chars(' ', '\t', '\n', '\r'), nil
	case 'S':
		return negate(chars(' ', '\t', '\n', '\r')), nil
	case 'w':
		return categories("L", "M", "N", "S"), nil
	case 'W':
		return negate(categories("L", "M", "N", "S")), nil
	case 'i', 'I':
		s := union(categories("L", "Nl"), chars('_', ':'))
		if e == 'I' {
			s = negate(s)
		}
		return s, nil
	case 'c', 'C':
		s := union(categories("L", "Nl", "Mn", "Mc", "Nd", "Lm"), chars('.', '_', ':', '-', 0xB7))
		if e == 'C' {
			s = negate(s)
		}
		return s, nil
	case 'p', 'P':
		var s runeSet
		if strings.HasPrefix(name, "Is") {
			blk, ok := unicodeBlocks[name[2:]]
			if !ok {
				return nil, fmt.Errorf("unknown Unicode block %s", name)
			}
			s = normalize(append([]runeRange(nil), blk...))
		} else {
			t, ok := unicode.Categories[name]
			if !ok {
				return nil, fmt.Errorf("unknown Unicode category %s", name)
			}
			s = tableSet(t)
		}
		if e == 'P' {
			s = negate(s)
		}
		return s, nil
	}
	return nil, fmt.Errorf("\\%c is not a multi-character escape", e)
}

// singleEscape is the character a single-character escape stands for.
func singleEscape(e rune) (rune, bool) {
	switch e {
	case 'n':
		return '\n', true
	case 'r':
		return '\r', true
	case 't':
		return '\t', true
	case '\\', '|', '.', '-', '^', '?', '*', '+', '{', '}', '(', ')', '[', ']', '$':
		return e, true
	}
	return 0, false
}

// needsSets reports whether the class starting at rs[i] ('[') uses
// subtraction, \P{IsBlock}, \I or \C, which only the set form can write.
func needsSets(rs []rune, i int) bool {
	depth := 0
	for ; i < len(rs); i++ {
		switch rs[i] {
		case '\\':
			if i+1 < len(rs) {
				switch rs[i+1] {
				case 'I', 'C':
					return true
				case 'P':
					if i+4 < len(rs) && rs[i+2] == '{' && rs[i+3] == 'I' && rs[i+4] == 's' {
						return true
					}
				}
			}
			i++
		case '[':
			if depth > 0 {
				return true // a nested class: subtraction
			}
			depth++
		case ']':
			depth--
			if depth == 0 {
				return false
			}
		}
	}
	return false
}

// parseClass reads the class starting at rs[i] ('[') as a set, with
// subtraction. It returns the set and the index of the closing ']'.
func parseClass(rs []rune, i int) (runeSet, int, error) {
	i++ // past '['
	neg := false
	if i < len(rs) && rs[i] == '^' {
		neg = true
		i++
	}
	var set runeSet
	var sub runeSet
	hasSub := false
	first := true
	// atom reads one character or escape at rs[i]: a single character
	// (ok) or a set (multi-character escape).
	atom := func() (rune, runeSet, bool, error) {
		c := rs[i]
		if c != '\\' {
			i++
			return c, nil, true, nil
		}
		if i+1 >= len(rs) {
			return 0, nil, false, fmt.Errorf("class ends with a backslash")
		}
		e := rs[i+1]
		if ch, ok := singleEscape(e); ok {
			i += 2
			return ch, nil, true, nil
		}
		name := ""
		if e == 'p' || e == 'P' {
			j := i + 2
			if j >= len(rs) || rs[j] != '{' {
				return 0, nil, false, fmt.Errorf("\\%c needs {name}", e)
			}
			k := j
			for k < len(rs) && rs[k] != '}' {
				k++
			}
			if k >= len(rs) {
				return 0, nil, false, fmt.Errorf("unclosed \\%c{", e)
			}
			name = string(rs[j+1 : k])
			i = k + 1
		} else {
			i += 2
		}
		s, err := escapeSet(e, name)
		return 0, s, false, err
	}
	for {
		if i >= len(rs) {
			return nil, 0, fmt.Errorf("unclosed character class")
		}
		c := rs[i]
		if c == ']' && !first {
			break
		}
		if c == '-' && i+1 < len(rs) && rs[i+1] == '[' {
			s, end, err := parseClass(rs, i+1)
			if err != nil {
				return nil, 0, err
			}
			sub, hasSub = s, true
			i = end + 1
			if i >= len(rs) || rs[i] != ']' {
				return nil, 0, fmt.Errorf("a subtracted class must end the class")
			}
			break
		}
		if c == '[' {
			return nil, 0, fmt.Errorf("nested character class")
		}
		first = false
		lo, s, single, err := atom()
		if err != nil {
			return nil, 0, err
		}
		if !single {
			set = union(set, s)
			continue
		}
		// a range lo-hi, unless the hyphen ends the class or starts a
		// subtraction
		if i+1 < len(rs) && rs[i] == '-' && rs[i+1] != ']' && rs[i+1] != '[' {
			i++
			hi, s2, single2, err := atom()
			if err != nil {
				return nil, 0, err
			}
			if !single2 || s2 != nil {
				return nil, 0, fmt.Errorf("a range must end in a single character")
			}
			if hi < lo {
				return nil, 0, fmt.Errorf("range %c-%c is reversed", lo, hi)
			}
			set = union(set, runeSet{{lo, hi}})
			continue
		}
		set = union(set, runeSet{{lo, lo}})
	}
	if neg {
		set = negate(set)
	}
	if hasSub {
		set = subtract(set, sub)
	}
	return set, i, nil
}
