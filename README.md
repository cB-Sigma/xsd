# xsd

W3C XML Schema 1.0 validation in pure Go, with the standard library only.

[![Go Reference](https://pkg.go.dev/badge/github.com/cB-Sigma/xsd.svg)](https://pkg.go.dev/github.com/cB-Sigma/xsd) [![test](https://github.com/cB-Sigma/xsd/actions/workflows/test.yml/badge.svg)](https://github.com/cB-Sigma/xsd/actions/workflows/test.yml) [![Licence: Apache-2.0](https://img.shields.io/badge/licence-Apache--2.0-blue.svg)](LICENSE) [![Go](https://img.shields.io/badge/go-1.24-00ADD8.svg)](go.mod)

Load a schema set from a file system or a zip, following `xs:include`,
`xs:import` and `xs:redefine` and checking the schema's own correctness,
and validate documents against it: content models, all the
XSD 1.0 built-in datatypes and facets, identity constraints, and hooks for
vocabularies that have their own extension rules. Parsing is bounded for
documents from untrusted peers. No cgo. Written for SigmaM2M, a oneM2M
platform, where it validates IEC 61850 SCL files, IEC 61968 (CIM)
payloads and OpenADR 2.0 messages.

## Features

| Feature | Status |
|---|---|
| Element and attribute declarations: global and local, `ref`, `form`, `fixed`, `default`, `nillable`, `abstract`, substitution groups | yes |
| Complex types: sequence, choice, all and named groups under occurrence bounds; mixed and empty content; simple and complex content by extension and restriction | yes |
| Wildcards (`xs:any`, `xs:anyAttribute`) with strict, lax and skip processing | yes |
| Simple types by restriction (every XSD 1.0 facet), list and union; all XSD 1.0 built-in datatypes; range facets on numbers, dates, times, the partial dates (`gYear` ...) and durations | yes |
| Regular expressions: XSD syntax translated to RE2, with the Unicode 3.1 block escapes and class subtraction | yes |
| `xsi:type`, `xsi:nil`, `block` and `final` (on substitution and `xsi:type`), ID and IDREF | yes |
| Identity constraints (`unique`, `key`, `keyref`) with the XSD 1.0 XPath subset, compared in the value space | yes |
| `xs:include` (chameleon includes too), `xs:import` and `xs:redefine` within the given file system; several root documents (`LoadFiles`) | yes |
| Documents named by URL, read through a caller's `LoadOptions.Fetch` (never by the loader itself), with a bound on fetches | yes |
| The schema's own correctness checked when it loads: the schema for schema documents, facet rules, component constraints, restriction of complex types (Particle Valid (Restriction)), Element Declarations Consistent, Unique Particle Attribution, the XSD regular expression grammar | yes |
| Bounded parsing (`Limits`: depth by default; elements, attributes, bytes on request) and a work bound on content-model checking | yes |
| `ValidateTree`: validate a tree the caller parsed, with hooks for undeclared elements and attributes and extra value checks; graded issues (`IssueKind`) | yes |
| XML 1.1 documents | read as XML 1.0 reads them (encoding/xml): the XML 1.1 name and control characters are not |
| `xsi:schemaLocation` and `xsi:noNamespaceSchemaLocation` hints | opt-in: `LoadHinted` assembles a schema from a document's hints (and the caller's roots, which win); `Validate` never follows them |
| Internal general entities declared in a document's DTD | opt-in: `DocOptions.Entities`, with the total expansion bounded (`Limits.MaxEntityBytes`); external and parameter entities, entities holding markup and the rest of the DTD are not read |
| XSD 1.1 (assertions, open content, conditional types, `xs:override`, ...) | no: refused at load (see "Not yet") |

## Choosing a Go XML Schema validator

Compared from each project's source at the versions named, in October
2026. Check the projects themselves before relying on a row.

| | this module v0.3.0 | lestrrat-go/helium `xsd` v0.10.0 (v0.11.0 since, still the newest on 2026-10-11) | lestrrat-go/libxml2 (master 3f0f7bd) | terminalstatic/go-xsd-validate v0.1.8 |
|---|---|---|---|---|
| Engine | own, Go standard library only | own, pure Go | libxml2 through cgo | libxml2 through cgo |
| Builds with `CGO_ENABLED=0` | yes | yes | no | no |
| Minimum Go | 1.24 | 1.26.1 | | |
| W3C XSD 1.0 test suite | 99.6 % of all 39,369 XSD 1.0 tests, 99.8 % with the two imported schemas supplied (below) | 14,383 of the 14,399 tests it selects (its README) | libxml2's results | libxml2's results |
| Checks the schema's own correctness | yes | yes | yes | yes |
| `xs:redefine` | yes | yes | yes | yes |
| Hooks for vocabulary extension rules on a parsed tree | yes (`ValidateTree`) | no | no | no |
| DTD entities in documents | internal ones, opt-in, with an expansion cap | yes, with an expansion cap | yes | yes |
| `xsi:schemaLocation` hints | opt-in (`LoadHinted`) | not compared | yes | yes |
| Size of a minimal validating program (stripped, darwin/arm64; our measurement) | 3.0 MB (2.7 MB at v0.1.0) | 14.5 MB | not measured | not measured |

The test-suite rows are not comparable as they stand: helium runs the
tests it selects, this module every test that applies to XSD 1.0.

Pick this module when you want a small, cgo-free validator with no
dependencies, especially when a vocabulary needs its own extension hooks
(IEC 61850 SCL is one). Pick helium when you need external entities or
the rest of the DTD (attribute defaults) and can use Go 1.26. Pick a libxml2 binding when cgo is fine
and you want the reference behaviour most other tools share. None of
these, nor this module, does XSD 1.1: for assertions and the rest of
1.1, Xerces-J or Saxon-EE (Java) are the options.

## Not yet

- **XSD 1.1.** Assertions (`xs:assert`, `xs:assertion`) and conditional
  type assignment need XPath 2.0: its typed data model, sequences,
  comparisons and arithmetic over the XSD types, `if`, `for`, `some` and
  `every`, and the function library. Nothing in Go stops a pure-Go
  implementation of the subset assertions use: the types and their
  comparisons are already here (simple.go). It is a large piece of work
  (a parser, a typed evaluator and about a hundred functions), not done
  in v0.3.0; until then a 1.1 schema is refused at load, not half
  applied. Open content, `xs:override`, `notQName` and `notNamespace` and
  the other 1.1 additions wait for the same release.
- **Missing components** that no document uses: XSD 1.0 lets a schema
  refer to a type or element that is never defined as long as nothing
  needs it; this module refuses such a schema at load (12 XSTS tests).
- **External entities and the rest of the DTD** (attribute defaults,
  parameter entities): not read, on purpose; documents from peers should
  not make the validator read files or the network.
- **XML 1.1** name and control characters (encoding/xml reads XML 1.0).

## Install

```
go get github.com/cB-Sigma/xsd@latest
```

Requires Go 1.24 or later.

## Quick start

```go
import "github.com/cB-Sigma/xsd"

s, err := xsd.Load(os.DirFS("schemas"), "meter.xsd") // follows include and import
if err != nil {
	return err
}
err = s.Validate([]byte(`<meter id="m1"><reading>1.5</reading></meter>`))
// nil, or *xsd.Errors: one Issue per failure, with line, path, message and kind
```

`Example` in example_test.go runs this with an in-memory schema and checks
its output.

## Documentation

API reference: https://pkg.go.dev/github.com/cB-Sigma/xsd

| Package | What it does |
|---|---|
| `xsd` (module root) | `Load`, `LoadFiles`, `LoadWith` (`LoadOptions`: `Fetch`, `MaxFetches`), `LoadZip`; `Hints`, `LoadHinted` (`HintOptions`: schema location hints, opt-in); `Schema.Validate`, `ValidateDoc` (warnings, `DocOptions`, `Entities` for DTD entities), `ValidateLimits`, `ValidateTree` with `Options` hooks; `LoadWarnings`, `CheckValue`, `SimpleTypes` |
| `xsts` (nested module, tests only) | runs the W3C XML Schema Test Suite against this module |

Limits: documents often come from untrusted peers. By default a document
nested deeper than 256 elements (`DefaultMaxDepth`) is refused before it
is validated, with one issue of kind `IssueLimit`; `Limits` also bounds
elements, attributes per element and bytes on request. Checking one
element's children against its content model has a work bound; past it
the element gets an `IssueLimit` issue instead of a verdict.

Schemas are checked for their own correctness when they load, which
costs time in proportion to the schema; it stays bounded for schemas
from untrusted sources (the restriction check compares particles pairwise,
the Unique Particle Attribution check has position and link bounds),
and `LoadOptions.MaxFetches` bounds what one load fetches.

Documents must be well-formed XML 1.0 (an XML 1.1 declaration is read
as XML 1.0). White space before the XML
declaration is accepted with an `IssueLeadingSpace` warning unless
`DocOptions.StrictProlog` is set.

## Testing

```
go test -race ./...
```

Tests carry `// Covers: SIG-...` comments: these are requirement IDs of
the SigmaM2M project's compliance report and have no meaning outside it.
Two tests read schemas that cannot be redistributed and skip without
them: the IEC 61850-6 SCL code component (`XSD_TEST_SCL_DIR`) and the
OpenADR 2.0 schemas (`SIGMA_TEST_OPENADR_XSD_DIR`).

### W3C XML Schema Test Suite

`xsts/` is a nested module that runs the
[W3C XML Schema Test Suite](https://github.com/w3c/xsdtests) (XSTS). The
suite is not copied here (it is 214 MB and under the W3C document
licence); fetch it and point `XSTS_DIR` at it:

```
git clone --depth 1 https://github.com/w3c/xsdtests
cd xsts && XSTS_DIR=../xsdtests go test -v ./...
```

Every test that applies to an XSD 1.0 processor is run (version tokens
1.0, 1.0-2e, XML-1.0), with its accepted or stable expectation; queried
or disputed tests are skipped (150). A schema test passes when loading
succeeds or fails as expected, an instance test when validation does. An
instance document with `xsi:schemaLocation` or
`xsi:noNamespaceSchemaLocation` is validated against the group's schema
documents and the ones its hints name (`LoadHinted`); one whose hinted
schema cannot be assembled counts as not valid, which some tests check.
The test fails on any change from `xsts/testdata/known-failures.txt`: a
new failure, or a listed test that now passes. `XSTS_DUMP=file` writes
every failure with its documents; `XSTS_LOAD` and `XSTS_DOC` with `-run
TestXSTSLoad` show one test's full outcome. `XSTS_FETCH_DIR` names a
folder of local copies for the documents the suite imports by URL. Results on 2026-10-11,
xsdtests master (7bc3365, 2026-04-01), without `XSTS_FETCH_DIR`
(v0.2.0's in brackets):

| Contributor | Schema tests, valid | Schema tests, invalid | Instance tests, valid | Instance tests, invalid | Total |
|---|---|---|---|---|---|
| NIST (datatypes) | 3,953/3,953 | – | 10,608/10,609 | 8,608/8,608 | 23,169/23,170 (23,169/23,170) |
| Microsoft | 6,484/6,487 | 3,165/3,168 | 2,733/2,739 | 1,913/1,922 | 14,295/14,316 (14,258/14,283) |
| Sun | 533/533 | 144/144 | 536/538 | 379/379 | 1,592/1,594 (1,590/1,594) |
| Boeing | 6/6 | – | 12/12 | – | 18/18 (18/18) |
| XSD 1.0 tests in the Saxonica and IBM sets | 39/52 | 23/23 | 29/45 | 47/59 | 138/179 (138/179) |
| Suite metadata (introspection) | 0/1 | – | 0/91 | – | 0/92 (0/92) |
| **All** | **11,015/11,032** | **3,332/3,335** | **13,918/14,034** | **10,947/10,968** | **39,212/39,369, 99.6 % (39,173/39,336)** |

With `XSTS_FETCH_DIR` holding copies of `http://www.w3.org/XML/2008/06/xlink.xsd`
and `http://www.w3.org/2001/xml.xsd` (fetched from W3C on 2026-10-11, not
included here), the 92 introspection tests pass too: **39,304/39,369,
99.8 %**.

The 157 failures, by cause (each is listed with its reason in
`known-failures.txt`):

- 92 introspection tests: their schema imports XLink from a URL that is
  not part of the suite (they pass when the harness is given a copy,
  above).
- 22 tests in the IBM and Saxonica sets that carry no version marking but
  use XSD 1.1 (`notQName`, `notNamespace`, assertions, `xs:error`), which
  this module refuses.
- 12 Saxonica tests of schemas with references that never resolve to
  components no document uses; this module refuses such a schema (XSD
  1.0 allows missing components until they are needed).
- 7 XML 1.1 documents with characters encoding/xml refuses or accepts as
  XML 1.0 does.
- 24 others: 5 instance tests whose schema is refused (an attribute
  group redefined twice, a circular union the suite queries, a local type
  that restricts the type it is declared in); 2 restrictions that only
  narrow their base but that the letter of Particle Valid (Restriction)
  refuses, accepted here; a child matched by an element declaration where
  the content model's path gives it to a wildcard; the content-model work
  bound reached on purpose; and single cases of identity constraints,
  `QName` values, fixed values, prohibited attributes and lists of huge
  doubles.

v0.3.0 runs 33 more tests (instance documents that name their schema by
hints only), all passing, and passes 6 more: 4 strict wildcards met only
through hints, and 2 documents whose hinted schema adds the declaration
of their root or `xsi:type`. It also found and fixed an element with
`xsi:type` but no declaration refused for `xsi:nil`.

v0.1.0 accepted 1,310 invalid schemas and 99 invalid documents. v0.2.0
checks the schema's own correctness (the schema for schema documents,
facets, components, restriction, regular expression syntax), supports
`xs:redefine`, applies blocks in documents and fixes the other causes;
3 invalid schemas and 7 invalid documents are still accepted (above).

Running the suite found and fixed, in v0.1.0: content-model checking that
took minutes on a document with about 2,000 children (now remembered per
position, with a work bound); range facets on the partial date types and
durations, class subtraction and most Unicode block escapes in patterns;
one schema from several root documents (`LoadFiles`).

### Interop tested against

- The W3C XML Schema Test Suite, above.
- helium's `xsd` v0.10.0, on every schema set and document SigmaM2M's
  tests use, plus mutations (2026-10-06): after fixing the four bugs that
  comparison found here, the two engines decided 14,830 of 14,854 cases
  alike; the other 24 are points where helium departs from XSD 1.0, or
  policy choices.
- In SigmaM2M: the IEC 61850-6 SCL 2007C5 schema and its examples, the
  IEC 61968-9 and 61968-3 payload schemas, the OpenADR 2.0a and 2.0b
  schema sets.

## Related repositories

None. This module has no simulator.

## Status

v0.x: the API may change before v1.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

See [SECURITY.md](SECURITY.md) to report a vulnerability privately.

## Licence

Apache-2.0 (see [LICENSE](LICENSE) and [NOTICE](NOTICE)). Copyright 2026
Abhinav Gautam.
