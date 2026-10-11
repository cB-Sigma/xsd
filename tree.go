package xsd

// IssueKind classifies an issue.
type IssueKind int

const (
	IssueOther        IssueKind = iota
	IssueRoot                   // the root element is not a global element of the schema
	IssueContent                // child elements depart from the content model
	IssueCharacters             // character data where the type allows none
	IssueAttribute              // an attribute not allowed, missing or not of its fixed value
	IssueValue                  // a value outside its simple type (Attr, Value, Why are set)
	IssueIdentity               // unique, key, keyref
	IssueID                     // ID uniqueness, IDREF resolution
	IssueLimit                  // the document passes a Limits bound and was not validated
	IssueLeadingSpace           // a warning: white space before the XML declaration (accepted)
)

// Element is one element of a document the caller has parsed itself.
type Element struct {
	Space, Local, Prefix string
	Attrs                []Attr
	Children             []*Element
	Text                 string // character data directly inside the element
	Line                 int
	// NS holds the prefix bindings in scope, for QName-typed values ("" is
	// the default namespace); nil means none.
	NS map[string]string
	// Ref is the caller's own handle for the element; the validator does
	// not look at it.
	Ref any
}

// Attr is one attribute of an Element (xmlns declarations excluded).
type Attr struct {
	Space, Local, Prefix, Value string
}

// Child describes a child element to Options.Child before its parent's
// content is matched.
type Child struct {
	Parent, Elem *Element
	// Declared: the parent's content model has an element particle that
	// admits the child.
	Declared bool
	// Wildcard: the parent's content model has an element wildcard.
	Wildcard bool
}

// Options adjust ValidateTree for a vocabulary with its own extension
// rules. Every hook is optional.
type Options struct {
	// Child is asked about each child element of an element of complex
	// type, in document order, before the content is matched. Returning
	// false leaves the child out: it is neither matched nor validated (the
	// hook reports whatever it wants to).
	Child func(Child) bool
	// Undeclared is called for an attribute the element's type does not
	// declare; allowed reports whether an attribute wildcard admits it.
	// Returning true means the hook has dealt with the attribute: nothing
	// is reported and it is not validated further.
	Undeclared func(e *Element, a Attr, allowed bool) bool
	// Check adds a check to every declared attribute value: typ is the
	// local name of the attribute's named simple type ("" when anonymous)
	// and the return value, if not empty, is why the value is not valid.
	// It takes precedence over the schema's own explanation.
	Check func(typ, value string) string
	// Opaque elements have their attributes validated but not their
	// content.
	Opaque func(e *Element) bool
	// NoFixed exempts an attribute from its fixed value and, for an
	// attribute with a fixed value, from a required use.
	NoFixed func(e *Element, local string) bool
	// Limits bound the tree (MaxBytes does not apply); the zero value
	// takes the defaults.
	Limits Limits
}

// ValidateTree checks a parsed document against the schema with options.
// The root must be a global element. It returns every issue found (nil if
// none); the caller grades them.
func (s *Schema) ValidateTree(root *Element, opt Options) Errors {
	if s == nil {
		return Errors{{Msg: errNoSchema.Error()}}
	}
	if is := treeLimits(root, opt.Limits); is != nil {
		return Errors{*is}
	}
	r := &run{s: s, ids: map[string]int{}, opt: opt, el: map[*node]*Element{}, byEl: map[*Element]*node{}}
	n := r.fromTree(root, nil, newScope(map[string]string{"xml": nsXML}))
	r.validate(n)
	return r.errs
}

func (r *run) fromTree(e *Element, parent *node, ns *nsScope) *node {
	if e.NS != nil {
		ns = newScope(e.NS)
	}
	n := &node{name: qname{e.Space, e.Local}, prefix: e.Prefix, parent: parent, ns: ns, line: e.Line}
	for _, a := range e.Attrs {
		n.attrs = append(n.attrs, attr{name: qname{a.Space, a.Local}, prefix: a.Prefix, value: a.Value})
	}
	n.text.WriteString(e.Text)
	r.el[n] = e
	r.byEl[e] = n
	for _, c := range e.Children {
		n.children = append(n.children, r.fromTree(c, n, ns))
	}
	return n
}

func publicAttr(a attr) Attr {
	return Attr{Space: a.name.Space, Local: a.name.Local, Prefix: a.prefix, Value: a.value}
}

// CheckValue checks a value against a global simple type, or a built-in
// one when space is the XML Schema namespace. It returns why the value is
// not valid ("" if it is) and false if there is no such type.
func (s *Schema) CheckValue(space, local, value string) (string, bool) {
	t := s.types[qname{space, local}]
	if t == nil || t.complex {
		return "", false
	}
	_, why := s.checkSimple(t, value, &node{ns: newScope(map[string]string{"xml": nsXML})})
	return why, true
}

// SimpleTypes lists the names of the global simple types of a namespace.
func (s *Schema) SimpleTypes(space string) []string {
	var out []string
	for q, t := range s.types {
		if q.Space == space && !t.complex {
			out = append(out, q.Local)
		}
	}
	return out
}

// Namespace is the XML Schema namespace, for CheckValue on built-in types.
const Namespace = nsXS
