// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package xsd

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Internal general entities (DocOptions.Entities). A document's internal
// DTD subset may declare entities, <!ENTITY name "text">, that the
// document then refers to as &name;. With the option they are expanded;
// without it a reference to one is not well-formed, as before.
//
// What is read: internal general entities whose replacement text is
// character data (references to other such entities, character references
// and the predefined entities inside it are expanded). What is not:
// external entities (SYSTEM or PUBLIC: nothing is read from outside the
// document), parameter entities, replacement text with markup (an
// element, a comment, a processing instruction), attribute-list defaults
// and the rest of the DTD. A document that refers to an entity of those
// kinds is refused.
//
// The expansion is bounded so that a small document cannot grow into a
// large one (the "billion laughs" pattern): each reference in the
// document, counted before parsing, adds its entity's full expansion to a
// total that may not pass Limits.MaxEntityBytes (DefaultMaxEntityBytes);
// entity nesting deeper than maxEntityDepth is refused.

// DefaultMaxEntityBytes bounds the text entity references add to one
// document unless Limits say otherwise.
const DefaultMaxEntityBytes = 1 << 20

const maxEntityDepth = 16

type entityDecl struct {
	value    string // literal value, character references expanded
	external bool
}

// readInternalSubset reads the general entity declarations of a DOCTYPE
// directive's internal subset ("DOCTYPE root [ ... ]").
func readInternalSubset(dir []byte) (map[string]entityDecl, error) {
	s := string(dir)
	open := strings.IndexByte(s, '[')
	if open < 0 {
		return nil, nil
	}
	sub := s[open+1:]
	if c := strings.LastIndexByte(sub, ']'); c >= 0 {
		sub = sub[:c]
	}
	out := map[string]entityDecl{}
	for len(sub) > 0 {
		i := strings.Index(sub, "<!")
		if i < 0 {
			break
		}
		sub = sub[i:]
		if strings.HasPrefix(sub, "<!--") {
			end := strings.Index(sub, "-->")
			if end < 0 {
				return nil, fmt.Errorf("unterminated comment in the DTD")
			}
			sub = sub[end+3:]
			continue
		}
		if !strings.HasPrefix(sub, "<!ENTITY") {
			end := declEnd(sub)
			if end < 0 {
				return nil, fmt.Errorf("unterminated declaration in the DTD")
			}
			sub = sub[end+1:]
			continue
		}
		end := declEnd(sub)
		if end < 0 {
			return nil, fmt.Errorf("unterminated entity declaration in the DTD")
		}
		decl := strings.TrimSpace(sub[len("<!ENTITY"):end])
		sub = sub[end+1:]
		if strings.HasPrefix(decl, "%") {
			continue // a parameter entity
		}
		name, rest := cutSpace(decl)
		if !isNCNameOrColon(name) {
			return nil, fmt.Errorf("entity name %q is not a name", name)
		}
		rest = strings.TrimSpace(rest)
		if _, dup := out[name]; dup {
			continue // the first declaration binds
		}
		if strings.HasPrefix(rest, "SYSTEM") || strings.HasPrefix(rest, "PUBLIC") {
			out[name] = entityDecl{external: true}
			continue
		}
		if rest == "" || rest[0] != '"' && rest[0] != '\'' {
			return nil, fmt.Errorf("entity %s: no quoted value", name)
		}
		q := rest[0]
		j := strings.IndexByte(rest[1:], q)
		if j < 0 {
			return nil, fmt.Errorf("entity %s: unterminated value", name)
		}
		lit := rest[1 : 1+j]
		if strings.ContainsRune(lit, '%') {
			return nil, fmt.Errorf("entity %s: parameter entity references in a value are not read", name)
		}
		v, err := expandCharRefs(lit)
		if err != nil {
			return nil, fmt.Errorf("entity %s: %w", name, err)
		}
		out[name] = entityDecl{value: v}
	}
	return out, nil
}

// declEnd finds the ">" closing a declaration, outside quoted literals.
func declEnd(s string) int {
	var q byte
	for i := 2; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '>':
			return i
		}
	}
	return -1
}

func cutSpace(s string) (string, string) {
	i := strings.IndexAny(s, " \t\r\n")
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i:]
}

func isNCNameOrColon(s string) bool {
	for _, p := range strings.Split(s, ":") {
		if !isNCName(p) {
			return false
		}
	}
	return s != ""
}

// expandCharRefs replaces character references (&#..; and &#x..;) in an
// entity's literal value, as a declaration does.
func expandCharRefs(s string) (string, error) {
	var b strings.Builder
	for {
		i := strings.Index(s, "&#")
		if i < 0 {
			b.WriteString(s)
			return b.String(), nil
		}
		b.WriteString(s[:i])
		j := strings.IndexByte(s[i:], ';')
		if j < 0 {
			return "", fmt.Errorf("unterminated character reference")
		}
		r, ok := charRef(s[i+2 : i+j])
		if !ok {
			return "", fmt.Errorf("bad character reference %s", s[i:i+j+1])
		}
		if r == '&' || r == '<' { // kept as references, read again when expanded
			b.WriteString(map[rune]string{'&': "&amp;", '<': "&lt;"}[r])
		} else {
			b.WriteRune(r)
		}
		s = s[i+j+1:]
	}
}

func charRef(s string) (rune, bool) {
	var n uint64
	var err error
	if strings.HasPrefix(s, "x") {
		n, err = strconv.ParseUint(s[1:], 16, 32)
	} else {
		n, err = strconv.ParseUint(s, 10, 32)
	}
	if err != nil || n > utf8.MaxRune || !isXMLChar(rune(n)) {
		return 0, false
	}
	return rune(n), true
}

func isXMLChar(r rune) bool {
	return r == 0x9 || r == 0xA || r == 0xD || r >= 0x20 && r <= 0xD7FF || r >= 0xE000 && r <= 0xFFFD || r >= 0x10000 && r <= 0x10FFFF
}

var predefined = map[string]string{"lt": "<", "gt": ">", "amp": "&", "apos": "'", "quot": `"`}

// expandEntities gives each declared internal entity its replacement as
// character data, with nesting and size bounds; an entity that cannot be
// expanded (external, markup, a cycle, too deep, unknown references) is
// left out with the reason, so a document that refers to it is refused.
func expandEntities(decls map[string]entityDecl, maxBytes int) (map[string]string, map[string]string) {
	out := map[string]string{}
	bad := map[string]string{}
	var expand func(name string, depth int, stack map[string]bool) (string, string)
	expand = func(name string, depth int, stack map[string]bool) (string, string) {
		if v, ok := out[name]; ok {
			return v, ""
		}
		if why, ok := bad[name]; ok {
			return "", why
		}
		d, ok := decls[name]
		switch {
		case !ok:
			return "", "entity " + name + " is not declared"
		case d.external:
			return "", "entity " + name + " is external, which is not read"
		case stack[name]:
			return "", "entity " + name + " refers to itself"
		case depth > maxEntityDepth:
			return "", fmt.Sprintf("entities nest deeper than %d", maxEntityDepth)
		}
		stack[name] = true
		defer delete(stack, name)
		var b strings.Builder
		s := d.value
		for {
			i := strings.IndexAny(s, "&<")
			if i < 0 {
				b.WriteString(s)
				break
			}
			b.WriteString(s[:i])
			if s[i] == '<' {
				return "", "entity " + name + " holds markup, which is not supported"
			}
			j := strings.IndexByte(s[i:], ';')
			if j < 0 {
				return "", "entity " + name + " has an unterminated reference"
			}
			ref := s[i+1 : i+j]
			switch {
			case strings.HasPrefix(ref, "#"):
				r, ok := charRef(ref[1:])
				if !ok {
					return "", "entity " + name + " has a bad character reference"
				}
				b.WriteRune(r)
			case predefined[ref] != "":
				b.WriteString(predefined[ref])
			default:
				v, why := expand(ref, depth+1, stack)
				if why != "" {
					return "", why
				}
				b.WriteString(v)
			}
			if maxBytes > 0 && b.Len() > maxBytes {
				return "", fmt.Sprintf("entity %s expands to more than %d bytes (the limit)", name, maxBytes)
			}
			s = s[i+j+1:]
		}
		v := b.String()
		if maxBytes > 0 && len(v) > maxBytes {
			return "", fmt.Sprintf("entity %s expands to more than %d bytes (the limit)", name, maxBytes)
		}
		out[name] = v
		return v, ""
	}
	for name := range decls {
		if _, why := expand(name, 0, map[string]bool{}); why != "" {
			bad[name] = why
		}
	}
	return out, bad
}

// useEntities reads a DOCTYPE directive's entities and hands their
// expansions to the decoder, after checking the document's references
// against the budget.
func useEntities(d *xml.Decoder, dir, rest []byte, maxBytes int) error {
	decls, err := readInternalSubset(dir)
	if err != nil || len(decls) == 0 {
		return err
	}
	ents, bad := expandEntities(decls, maxBytes)
	if err := entityBudget(rest, ents, bad, maxBytes); err != nil {
		return err
	}
	d.Entity = ents
	return nil
}

// entityBudget counts the references to declared entities in the document
// after the DTD and checks their total expansion against the limit. It
// counts every "&name;" in the text, comments and CDATA sections included,
// so it errs towards refusing.
func entityBudget(body []byte, ents map[string]string, bad map[string]string, maxBytes int) error {
	total := 0
	for {
		i := bytes.IndexByte(body, '&')
		if i < 0 {
			return nil
		}
		body = body[i+1:]
		j := bytes.IndexByte(body, ';')
		if j < 0 || j > 256 {
			continue
		}
		name := body[:j]
		if why, ok := bad[string(name)]; ok {
			return fmt.Errorf("%s", why)
		}
		if v, ok := ents[string(name)]; ok {
			total += len(v)
			if maxBytes > 0 && total > maxBytes {
				return &limitError{msg: fmt.Sprintf("entity references expand to more than %d bytes (the limit)", maxBytes)}
			}
		}
	}
}
