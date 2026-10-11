# Changelog

## v0.3.0 (2026-10-11)

Schema location hints and DTD entities, both opt-in; on the W3C XML
Schema Test Suite 39,212 of 39,369 XSD 1.0 tests pass (99.6 %), and
39,304 (99.8 %) when the harness is given local copies of the two
schemas the suite's metadata imports by URL. Existing functions keep
their signatures; `Validate` still never follows hints or reads a DTD.

### Added

- `Hints` and `LoadHinted` (`HintOptions`): assemble the schema for a
  document from the documents its `xsi:schemaLocation` and
  `xsi:noNamespaceSchemaLocation` attributes name, on any element,
  relative to the document, through `LoadOptions.Fetch` for URLs. Root
  documents the caller gives win over hints for their namespace unless
  `Override`. A new location for a namespace after an element of that
  namespace is refused, as XSD 1.0 says.
- `DocOptions.Entities` and `Limits.MaxEntityBytes`
  (`DefaultMaxEntityBytes`, 1 MiB): internal general entities declared in
  a document's DTD are expanded, nested ones included, with the total
  expansion bounded before parsing and nesting bounded; external and
  parameter entities and entities holding markup are refused when used.
- The XSTS harness runs instance tests that name their schema only by
  hints (33 more tests) and gives hinted documents to every instance test
  that has them.
- `FuzzEntities`.

### Fixed

- An element validated by `xsi:type` alone, without a declaration, was
  refused when it had `xsi:nil="true"`; XSD 1.0 gives xsi:nil meaning
  only through a declaration.

## v0.2.0 (2026-10-11)

The schema's own correctness is now checked when it loads, `xs:redefine`
is supported, and the causes of the invalid documents v0.1 accepted are
fixed. On the W3C XML Schema Test Suite the module passes 39,173 of the
39,336 XSD 1.0 tests (99.6 %, from 95.5 %); see the README for the
results by contributor and the failures that remain. Existing functions
keep their signatures.

### Added

- Schema correctness checking. A schema that breaks a constraint XSD 1.0
  puts on schemas is refused at load, with an error naming the problem:
  - the representation rules of the schema for schema documents: which
    attributes each element takes and their values, the order and number
    of children, ids, name and ref used together, default and fixed
    together, a required attribute with a default, character data where
    none belongs (`s4s.go`);
  - facets: which facets apply to which datatypes, how the facets of one
    restriction agree (lengths, ranges, digits), that a restriction only
    narrows its base (ranges inside the base's, lengths, digits, white
    space no less collapsed, fixed facets unchanged), enumeration values
    valid in the base, a facet given twice (`facetrules.go`);
  - components: default and fixed values valid for their type and never
    on ID types, substitution group members derived from the head's type
    as the head allows, `final` on simple and complex types, list item
    types atomic or unions of atomic types, circular unions, attribute
    uses consistent with their declarations, one ID attribute per type,
    element declarations of one name having one type in a content model,
    all groups alone at the top of a content model, extensions keeping
    mixed or element-only content, notations declared once and NOTATION
    enumerations naming them, references only into the namespaces a
    document imports (`components.go`);
  - complex types derived by restriction: content models compared
    particle by particle (the cases of Particle Valid (Restriction),
    substitution groups expanded), attribute uses and attribute wildcards
    compared with the base's (`restrict.go`);
  - XSD regular expressions read strictly: reluctant quantifiers, a
    quantifier with nothing to repeat, malformed `{n,m}`, unescaped `]`,
    `{` or `}`, unbalanced parentheses and a hyphen in the middle of a
    class are refused.
- `xs:redefine`: simple types, complex types, model groups and attribute
  groups redefined, the old definition reachable from the new one (a
  type's base, a group or attribute group referring to itself once),
  redefinitions without a self-reference checked to restrict the old
  component; nested redefinitions (`redefine.go`).
- `LoadWith` and `LoadOptions`: `Fetch` reads documents named by URL (an
  include, import or redefine outside the file system, and relative
  locations inside such documents), within `MaxFetches`
  (`DefaultMaxFetches`, 64). The loader still never reaches the network
  itself.
- Blocking in documents: `block` and `blockDefault` on element
  declarations and complex types are applied to substitution group
  members and to `xsi:type`.
- Attribute wildcards of attribute groups combine by intersection, those
  of an extension and its base by union, as XSD 1.0 (with its errata)
  defines them; v0.1 refused more than one wildcard.
- A group reached again through an element's type inside the group is
  accepted (v0.1 took it for a group containing itself).
- A root element the schema does not declare is validated by its
  `xsi:type`, when it names one.
- XML 1.1 documents are read (as XML 1.0 reads them).
- `xsts`: `XSTS_DUMP` writes every failure with its documents,
  `TestXSTSLoad` loads one test's schema, `XSTS_FETCH_DIR` serves the
  documents the suite imports by URL.

### Changed

- An `xs:include`, `xs:import` or `xs:redefine` whose location does not
  resolve, or names a URL with no `Fetch`, is no longer an error by
  itself (XSD 1.0 says so); `LoadWarnings` names it, and a reference to
  what it would have declared fails with an error that mentions it.
- Invalid documents v0.1 accepted are refused: substitutes and `xsi:type`
  a block forbids, `xsi:nil` (even `false`) on an element that is not
  nillable, an element with a fixed value holding element children, two
  attributes of type ID on one element, a prohibited attribute, an
  absent attribute's default IDREF or IDREFS naming no ID, an identity
  constraint field selecting an element without a simple value, an
  element of an `xs:all` group with `maxOccurs="0"`.
- Fixed values of elements compare in the value space (`1` and `true`
  are one boolean in a union of boolean and int); a union value is
  normalised as the member type that accepts it.
- Names (`NCName`, `Name`, `NMTOKEN`) and the regular expression escapes
  `\i` and `\c` follow the XML 1.0 fifth edition name characters.
- `gMonth` takes only the `--MM` form (the `--MM--` form was withdrawn by
  an erratum).
- Built-in types record their derivation (restriction, or list for
  `IDREFS`, `NMTOKENS` and `ENTITIES`), which blocks and finals apply to.
- Occurrence bounds too large for an int are read as 2^30 instead of
  refusing the schema.
- The same document reached by two names (a case-insensitive file
  system, a link) is read once.
- White space around the operators of identity-constraint paths is
  accepted.

### Not done (see the README)

- XSD 1.1 (assertions, open content, conditional types, `xs:override`,
  the 1.1 wildcard attributes): refused at load.
- `xsi:schemaLocation` hints: the receiver chooses the schema.
- DTDs and entity declarations in documents.
- Lazy resolution of references to missing components (a schema with a
  reference that does not resolve is refused even when the component is
  never used).

## v0.1.0 (2026-10-11)

First release, from SigmaM2M.
