package xsd

import "fmt"

// DefaultMaxDepth is the deepest element nesting accepted unless Limits
// say otherwise. Real documents stay far below it (IEC 61850 SCL, CIM and
// OpenADR payloads nest fewer than 20 levels); without a bound, a small
// document of nested start tags costs time and memory out of proportion
// to its size.
const DefaultMaxDepth = 256

// Limits bound the documents a validation accepts, for input from
// untrusted peers. A zero field takes its default and a negative one
// removes the bound. A document beyond a limit is refused with a single
// IssueLimit issue and is not validated.
//
// The validator's costs grow linearly with the document's size, apart
// from depth, which is why only MaxDepth has a default. The other bounds
// are for callers that want to cap the work per message on top of the
// size their transport already allows. encoding/xml, which parses the
// documents, has no bounds of its own here.
type Limits struct {
	// MaxDepth is the deepest element nesting (the root is at depth 1).
	// Default DefaultMaxDepth.
	MaxDepth int
	// MaxElements is the most elements in one document. Default: none.
	MaxElements int
	// MaxAttrs is the most attributes on one element, namespace
	// declarations included. Default: none.
	MaxAttrs int
	// MaxBytes is the largest document, in bytes, ValidateLimits accepts
	// (ValidateTree, given a tree, ignores it). Default: none.
	MaxBytes int
	// MaxEntityBytes bounds the text that entity references add to a
	// document read with DocOptions.Entities. Default
	// DefaultMaxEntityBytes.
	MaxEntityBytes int
}

func (l Limits) entityBytes() int {
	if l.MaxEntityBytes == 0 {
		return DefaultMaxEntityBytes
	}
	return l.MaxEntityBytes
}

func (l Limits) withDefaults() Limits {
	if l.MaxDepth == 0 {
		l.MaxDepth = DefaultMaxDepth
	}
	return l
}

// limitError is a document refused for a limit, not for its content.
type limitError struct {
	line int
	msg  string
}

func (e *limitError) Error() string {
	if e.line > 0 {
		return fmt.Sprintf("line %d: %s", e.line, e.msg)
	}
	return e.msg
}

// ValidateLimits checks doc against the schema like Validate, with
// explicit limits instead of the defaults.
func (s *Schema) ValidateLimits(doc []byte, lim Limits) error {
	_, err := s.ValidateDoc(doc, DocOptions{Limits: lim})
	return err
}

// DocOptions adjust how ValidateDoc reads a document.
type DocOptions struct {
	Limits Limits
	// StrictProlog refuses XML white space before the XML declaration.
	// By default it is accepted with an IssueLeadingSpace warning: XML 1.0
	// wants the declaration first, but some senders write a line break
	// before it.
	StrictProlog bool
	// Entities expands the internal general entities a document's DTD
	// declares, within Limits.MaxEntityBytes (see dtd.go for what is and
	// is not read). Without it a reference to a declared entity makes the
	// document not well-formed.
	Entities bool
}

// ValidateDoc checks doc against the schema. It returns the warnings
// (issues that do not make the document invalid, such as
// IssueLeadingSpace) and, like Validate, nil or *Errors.
func (s *Schema) ValidateDoc(doc []byte, opt DocOptions) (Errors, error) {
	if s == nil {
		return nil, errNoSchema
	}
	root, warn, err := parseDoc(doc, opt.Limits, opt.StrictProlog, opt.Entities)
	if err != nil {
		if le, ok := err.(*limitError); ok {
			return nil, &Errors{{Line: le.line, Msg: "document refused: " + le.msg, Kind: IssueLimit}}
		}
		return nil, &Errors{{Msg: "not well-formed XML: " + err.Error()}}
	}
	r := &run{s: s, ids: map[string]int{}}
	r.validate(root)
	if len(r.errs) == 0 {
		return warn, nil
	}
	return warn, &r.errs
}

// treeLimits checks a caller's tree against the limits before it is
// validated; it returns the issue for the first limit passed, if any.
func treeLimits(root *Element, lim Limits) *Issue {
	lim = lim.withDefaults()
	elements := 0
	type item struct {
		e     *Element
		depth int
	}
	stack := []item{{root, 1}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		elements++
		msg := ""
		switch {
		case lim.MaxDepth > 0 && it.depth > lim.MaxDepth:
			msg = fmt.Sprintf("elements are nested deeper than %d (the limit)", lim.MaxDepth)
		case lim.MaxElements > 0 && elements > lim.MaxElements:
			msg = fmt.Sprintf("the document has more than %d elements (the limit)", lim.MaxElements)
		case lim.MaxAttrs > 0 && len(it.e.Attrs) > lim.MaxAttrs:
			msg = fmt.Sprintf("an element has more than %d attributes (the limit)", lim.MaxAttrs)
		}
		if msg != "" {
			return &Issue{Line: it.e.Line, Msg: "document refused: " + msg, Kind: IssueLimit, Elem: it.e}
		}
		for i := len(it.e.Children) - 1; i >= 0; i-- {
			stack = append(stack, item{it.e.Children[i], it.depth + 1})
		}
	}
	return nil
}
