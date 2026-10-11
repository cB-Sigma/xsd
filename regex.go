package xsd

import (
	"fmt"
	"regexp"
	"strings"
)

// XSD 1.0 regular expressions (Part 2, Appendix F) differ from RE2 in a
// few places; translateRegex rewrites one into an anchored Go expression:
//   - the whole value must match, so the result is wrapped in ^(?:...)$;
//   - ^ and $ are ordinary characters outside a class;
//   - . excludes only newline and carriage return;
//   - \d, \w, \s, \i, \c and their capitals use the XSD definitions
//     (\d is any Unicode decimal digit, \w anything but punctuation,
//     separators and "other" characters, \s the four XML space characters,
//     \i and \c the XML name start and name characters);
//   - \p{IsBlock} block escapes become the code point ranges of the
//     Unicode 3.1 block (unicodeBlocks), the version XSD 1.0 names;
//   - a class with subtraction ([a-z-[aeiou]]), \P{IsBlock}, \I or \C has
//     no RE2 form, so it is computed as a set of ranges and written out
//     (regexclass.go).

const (
	// Letters and the other name-start characters (approximated by
	// Unicode categories, as most validators do).
	nameStart = `\p{L}\p{Nl}_:`
	nameChar  = `\p{L}\p{Nl}\p{Mn}\p{Mc}\p{Nd}\p{Lm}._:\-\x{B7}`
	spaceSet  = ` \t\n\r`
	// \w is the complement of P, Z and C: every character is in exactly
	// one general category, so it is the union of the rest.
	wordSet = `\p{L}\p{M}\p{N}\p{S}`
	nonWord = `\p{P}\p{Z}\p{C}`
	nonSpc  = `\x{0}-\x{8}\x{B}\x{C}\x{E}-\x{1F}\x{21}-\x{10FFFF}`
)

func translateRegex(p string) (*regexp.Regexp, error) {
	var b strings.Builder
	rs := []rune(p)
	inClass := false
	classStart := false
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		atStart := classStart
		classStart = false
		switch {
		case c == '\\':
			if i+1 >= len(rs) {
				return nil, fmt.Errorf("pattern %q ends with a backslash", p)
			}
			i++
			e := rs[i]
			var in, out string // inside a class, outside a class
			switch e {
			case 'd':
				in, out = `\p{Nd}`, `\p{Nd}`
			case 'D':
				in, out = `\P{Nd}`, `\P{Nd}`
			case 's':
				in, out = spaceSet, `[`+spaceSet+`]`
			case 'S':
				in, out = nonSpc, `[^`+spaceSet+`]`
			case 'w':
				in, out = wordSet, `[`+wordSet+`]`
			case 'W':
				in, out = nonWord, `[`+nonWord+`]`
			case 'i':
				in, out = nameStart, `[`+nameStart+`]`
			case 'c':
				in, out = nameChar, `[`+nameChar+`]`
			case 'I', 'C':
				if inClass {
					return nil, fmt.Errorf("pattern %q: \\%c inside a character class is not supported", p, e)
				}
				set := nameStart
				if e == 'C' {
					set = nameChar
				}
				out = `[^` + set + `]`
			case 'p', 'P':
				j := i + 1
				if j >= len(rs) || rs[j] != '{' {
					return nil, fmt.Errorf("pattern %q: \\%c needs {name}", p, e)
				}
				k := j
				for k < len(rs) && rs[k] != '}' {
					k++
				}
				if k >= len(rs) {
					return nil, fmt.Errorf("pattern %q: unclosed \\%c{", p, e)
				}
				name := string(rs[j+1 : k])
				i = k
				if strings.HasPrefix(name, "Is") {
					set, err := escapeSet(e, name)
					if err != nil {
						return nil, fmt.Errorf("pattern %q: Unicode block escape \\%c{%s} is not supported", p, e, name)
					}
					// \P{IsBlock} inside a class goes through parseClass
					in, out = set.String(), set.class()
					break
				}
				in = `\` + string(e) + `{` + name + `}`
				out = in
			case 'n':
				in, out = `\n`, `\n`
			case 'r':
				in, out = `\r`, `\r`
			case 't':
				in, out = `\t`, `\t`
			case '\\', '|', '.', '-', '^', '?', '*', '+', '{', '}', '(', ')', '[', ']', '$':
				in = `\` + string(e)
				out = in
			default:
				return nil, fmt.Errorf("pattern %q: unknown escape \\%c", p, e)
			}
			if inClass {
				b.WriteString(in)
			} else {
				b.WriteString(out)
			}
		case inClass:
			switch c {
			case ']':
				inClass = false
				b.WriteByte(']')
			case '[':
				return nil, fmt.Errorf("pattern %q: character class subtraction or nested classes are not supported", p)
			case '^':
				if atStart {
					b.WriteByte('^')
				} else {
					b.WriteString(`\^`)
				}
			case '-':
				if i+1 < len(rs) && rs[i+1] == '[' {
					return nil, fmt.Errorf("pattern %q: character class subtraction is not supported", p)
				}
				b.WriteByte('-')
			default:
				b.WriteString(regexp.QuoteMeta(string(c)))
			}
		default:
			switch c {
			case '[':
				if needsSets(rs, i) {
					set, end, err := parseClass(rs, i)
					if err != nil {
						return nil, fmt.Errorf("pattern %q: %w", p, err)
					}
					b.WriteString(set.class())
					i = end
					continue
				}
				inClass, classStart = true, true
				b.WriteByte('[')
			case '^', '$':
				b.WriteString(`\` + string(c))
			case '.':
				b.WriteString(`[^\n\r]`)
			case '(':
				if i+1 < len(rs) && rs[i+1] == '?' {
					return nil, fmt.Errorf("pattern %q: (? is not XSD regular expression syntax", p)
				}
				b.WriteString("(?:")
			default:
				b.WriteRune(c)
			}
		}
	}
	if inClass {
		return nil, fmt.Errorf("pattern %q: unclosed character class", p)
	}
	re, err := regexp.Compile(`^(?:` + b.String() + `)$`)
	if err != nil {
		return nil, fmt.Errorf("pattern %q: %w", p, err)
	}
	return re, nil
}

// unicodeBlocks are the code point ranges of the Unicode 3.1 blocks (the
// Unicode Character Database's Blocks.txt for 3.1), by the names XSD 1.0
// gives them: the block name without spaces.
var unicodeBlocks = map[string][]runeRange{
	"BasicLatin":                           {{0x0000, 0x007F}},
	"Latin-1Supplement":                    {{0x0080, 0x00FF}},
	"LatinExtended-A":                      {{0x0100, 0x017F}},
	"LatinExtended-B":                      {{0x0180, 0x024F}},
	"IPAExtensions":                        {{0x0250, 0x02AF}},
	"SpacingModifierLetters":               {{0x02B0, 0x02FF}},
	"CombiningDiacriticalMarks":            {{0x0300, 0x036F}},
	"Greek":                                {{0x0370, 0x03FF}},
	"Cyrillic":                             {{0x0400, 0x04FF}},
	"Armenian":                             {{0x0530, 0x058F}},
	"Hebrew":                               {{0x0590, 0x05FF}},
	"Arabic":                               {{0x0600, 0x06FF}},
	"Syriac":                               {{0x0700, 0x074F}},
	"Thaana":                               {{0x0780, 0x07BF}},
	"Devanagari":                           {{0x0900, 0x097F}},
	"Bengali":                              {{0x0980, 0x09FF}},
	"Gurmukhi":                             {{0x0A00, 0x0A7F}},
	"Gujarati":                             {{0x0A80, 0x0AFF}},
	"Oriya":                                {{0x0B00, 0x0B7F}},
	"Tamil":                                {{0x0B80, 0x0BFF}},
	"Telugu":                               {{0x0C00, 0x0C7F}},
	"Kannada":                              {{0x0C80, 0x0CFF}},
	"Malayalam":                            {{0x0D00, 0x0D7F}},
	"Sinhala":                              {{0x0D80, 0x0DFF}},
	"Thai":                                 {{0x0E00, 0x0E7F}},
	"Lao":                                  {{0x0E80, 0x0EFF}},
	"Tibetan":                              {{0x0F00, 0x0FFF}},
	"Myanmar":                              {{0x1000, 0x109F}},
	"Georgian":                             {{0x10A0, 0x10FF}},
	"HangulJamo":                           {{0x1100, 0x11FF}},
	"Ethiopic":                             {{0x1200, 0x137F}},
	"Cherokee":                             {{0x13A0, 0x13FF}},
	"UnifiedCanadianAboriginalSyllabics":   {{0x1400, 0x167F}},
	"Ogham":                                {{0x1680, 0x169F}},
	"Runic":                                {{0x16A0, 0x16FF}},
	"Khmer":                                {{0x1780, 0x17FF}},
	"Mongolian":                            {{0x1800, 0x18AF}},
	"LatinExtendedAdditional":              {{0x1E00, 0x1EFF}},
	"GreekExtended":                        {{0x1F00, 0x1FFF}},
	"GeneralPunctuation":                   {{0x2000, 0x206F}},
	"SuperscriptsandSubscripts":            {{0x2070, 0x209F}},
	"CurrencySymbols":                      {{0x20A0, 0x20CF}},
	"CombiningMarksforSymbols":             {{0x20D0, 0x20FF}},
	"LetterlikeSymbols":                    {{0x2100, 0x214F}},
	"NumberForms":                          {{0x2150, 0x218F}},
	"Arrows":                               {{0x2190, 0x21FF}},
	"MathematicalOperators":                {{0x2200, 0x22FF}},
	"MiscellaneousTechnical":               {{0x2300, 0x23FF}},
	"ControlPictures":                      {{0x2400, 0x243F}},
	"OpticalCharacterRecognition":          {{0x2440, 0x245F}},
	"EnclosedAlphanumerics":                {{0x2460, 0x24FF}},
	"BoxDrawing":                           {{0x2500, 0x257F}},
	"BlockElements":                        {{0x2580, 0x259F}},
	"GeometricShapes":                      {{0x25A0, 0x25FF}},
	"MiscellaneousSymbols":                 {{0x2600, 0x26FF}},
	"Dingbats":                             {{0x2700, 0x27BF}},
	"BraillePatterns":                      {{0x2800, 0x28FF}},
	"CJKRadicalsSupplement":                {{0x2E80, 0x2EFF}},
	"KangxiRadicals":                       {{0x2F00, 0x2FDF}},
	"IdeographicDescriptionCharacters":     {{0x2FF0, 0x2FFF}},
	"CJKSymbolsandPunctuation":             {{0x3000, 0x303F}},
	"Hiragana":                             {{0x3040, 0x309F}},
	"Katakana":                             {{0x30A0, 0x30FF}},
	"Bopomofo":                             {{0x3100, 0x312F}},
	"HangulCompatibilityJamo":              {{0x3130, 0x318F}},
	"Kanbun":                               {{0x3190, 0x319F}},
	"BopomofoExtended":                     {{0x31A0, 0x31BF}},
	"EnclosedCJKLettersandMonths":          {{0x3200, 0x32FF}},
	"CJKCompatibility":                     {{0x3300, 0x33FF}},
	"CJKUnifiedIdeographsExtensionA":       {{0x3400, 0x4DB5}},
	"CJKUnifiedIdeographs":                 {{0x4E00, 0x9FFF}},
	"YiSyllables":                          {{0xA000, 0xA48F}},
	"YiRadicals":                           {{0xA490, 0xA4CF}},
	"HangulSyllables":                      {{0xAC00, 0xD7A3}},
	"HighSurrogates":                       {{0xD800, 0xDB7F}},
	"HighPrivateUseSurrogates":             {{0xDB80, 0xDBFF}},
	"LowSurrogates":                        {{0xDC00, 0xDFFF}},
	"PrivateUse":                           {{0xE000, 0xF8FF}, {0xF0000, 0xFFFFD}, {0x100000, 0x10FFFD}},
	"CJKCompatibilityIdeographs":           {{0xF900, 0xFAFF}},
	"AlphabeticPresentationForms":          {{0xFB00, 0xFB4F}},
	"ArabicPresentationForms-A":            {{0xFB50, 0xFDFF}},
	"CombiningHalfMarks":                   {{0xFE20, 0xFE2F}},
	"CJKCompatibilityForms":                {{0xFE30, 0xFE4F}},
	"SmallFormVariants":                    {{0xFE50, 0xFE6F}},
	"ArabicPresentationForms-B":            {{0xFE70, 0xFEFE}},
	"Specials":                             {{0xFEFF, 0xFEFF}, {0xFFF0, 0xFFFD}},
	"HalfwidthandFullwidthForms":           {{0xFF00, 0xFFEF}},
	"OldItalic":                            {{0x10300, 0x1032F}},
	"Gothic":                               {{0x10330, 0x1034F}},
	"Deseret":                              {{0x10400, 0x1044F}},
	"ByzantineMusicalSymbols":              {{0x1D000, 0x1D0FF}},
	"MusicalSymbols":                       {{0x1D100, 0x1D1FF}},
	"MathematicalAlphanumericSymbols":      {{0x1D400, 0x1D7FF}},
	"CJKUnifiedIdeographsExtensionB":       {{0x20000, 0x2A6D6}},
	"CJKCompatibilityIdeographsSupplement": {{0x2F800, 0x2FA1F}},
	"Tags":                                 {{0xE0000, 0xE007F}},
}
