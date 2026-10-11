# xsd

W3C XML Schema 1.0 validation in pure Go, with the standard library only.

[![Go Reference](https://pkg.go.dev/badge/github.com/cB-Sigma/xsd.svg)](https://pkg.go.dev/github.com/cB-Sigma/xsd) [![test](https://github.com/cB-Sigma/xsd/actions/workflows/test.yml/badge.svg)](https://github.com/cB-Sigma/xsd/actions/workflows/test.yml) [![Licence: Apache-2.0](https://img.shields.io/badge/licence-Apache--2.0-blue.svg)](LICENSE) [![Go](https://img.shields.io/badge/go-1.24-00ADD8.svg)](go.mod)

Load a schema set from a file system or a zip, following `xs:include` and
`xs:import`, and validate documents against it: content models, all the
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
| `xsi:type`, `xsi:nil`, ID and IDREF | yes |
| Identity constraints (`unique`, `key`, `keyref`) with the XSD 1.0 XPath subset, compared in the value space | yes |
| `xs:include` (chameleon includes too) and `xs:import` within the given file system; several root documents (`LoadFiles`) | yes |
| Unique Particle Attribution checked when a schema loads | yes |
| Bounded parsing (`Limits`: depth by default; elements, attributes, bytes on request) and a work bound on content-model checking | yes |
| `ValidateTree`: validate a tree the caller parsed, with hooks for undeclared elements and attributes and extra value checks; graded issues (`IssueKind`) | yes |
| `xs:redefine` | no: refused at load |
| Constraints on the schema's own correctness (that a restriction restricts, element declarations consistent, ...) | no: the schema is trusted (see the suite results) |
| `xsi:schemaLocation` hints, remote imports, DTDs and entities | no |
| XSD 1.1 (assertions, open content, conditional types, ...) | no: refused at load |

## Choosing a Go XML Schema validator

Compared from each project's source at the versions named, in October
2026. Check the projects themselves before relying on a row.

| | this module | lestrrat-go/helium `xsd` v0.10.0 (v0.11.0 since) | lestrrat-go/libxml2 (master 3f0f7bd) | terminalstatic/go-xsd-validate v0.1.8 |
|---|---|---|---|---|
| Engine | own, Go standard library only | own, pure Go | libxml2 through cgo | libxml2 through cgo |
| Builds with `CGO_ENABLED=0` | yes | yes | no | no |
| Minimum Go | 1.24 | 1.26.1 | | |
| W3C XSD 1.0 test suite | 95.5 % of the XSD 1.0 tests (below) | 14,383 of the 14,399 tests it selects (its README) | libxml2's results | libxml2's results |
| Checks the schema's own correctness | partly (UPA, references, cycles) | yes | yes | yes |
| `xs:redefine` | no | yes | yes | yes |
| Hooks for vocabulary extension rules on a parsed tree | yes (`ValidateTree`) | no | no | no |
| DTD entities in documents | no | yes, with an expansion cap | yes | yes |
| Size of a minimal validating program (stripped; our measurement) | 2.7 MB | 14.5 MB | not measured | not measured |

Pick this module when you want a small, cgo-free validator with no
dependencies for schema sets you trust, especially when a vocabulary
needs its own extension hooks (IEC 61850 SCL is one). Pick helium when you
also need the schema itself checked, `xs:redefine` or DTD entities, and
can use Go 1.26. Pick a libxml2 binding when cgo is fine and you want the
reference behaviour most other tools share.

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
| `xsd` (module root) | `Load`, `LoadFiles`, `LoadZip`; `Schema.Validate`, `ValidateDoc` (warnings, `DocOptions`), `ValidateLimits`, `ValidateTree` with `Options` hooks; `LoadWarnings`, `CheckValue`, `SimpleTypes` |
| `xsts` (nested module, tests only) | runs the W3C XML Schema Test Suite against this module |

Limits: documents often come from untrusted peers. By default a document
nested deeper than 256 elements (`DefaultMaxDepth`) is refused before it
is validated, with one issue of kind `IssueLimit`; `Limits` also bounds
elements, attributes per element and bytes on request. Checking one
element's children against its content model has a work bound; past it
the element gets an `IssueLimit` issue instead of a verdict.

Documents must be well-formed XML 1.0. White space before the XML
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
1.0, 1.0-2e, XML-1.0), with its accepted or stable expectation; queried or
disputed tests, and instance tests that rely on `xsi:schemaLocation`, are
skipped (183). A schema test passes when loading succeeds or fails as
expected, an instance test when validation does. The test fails on any
change from `xsts/testdata/known-failures.txt`: a new failure, or a
listed test that now passes. Results on 2026-10-11, xsdtests master:

| Contributor | Schema tests, valid | Schema tests, invalid | Instance tests, valid | Instance tests, invalid | Total |
|---|---|---|---|---|---|
| NIST (datatypes) | 3,953/3,953 | – | 10,608/10,609 | 8,608/8,608 | 23,169/23,170 |
| Microsoft | 6,397/6,487 | 1,962/3,168 | 2,674/2,720 | 1,831/1,908 | 12,864/14,283 |
| Sun | 524/533 | 48/144 | 515/538 | 335/379 | 1,422/1,594 |
| Boeing | 5/6 | – | 10/12 | – | 15/18 |
| XSD 1.0 tests in the Saxonica and IBM sets | 32/52 | 15/23 | 17/45 | 38/59 | 102/179 |
| Suite metadata (introspection) | 0/1 | – | 0/91 | – | 0/92 |
| **All** | **10,911/11,032** | **2,025/3,335** | **13,824/14,015** | **10,812/10,954** | **37,572/39,336 (95.5 %)** |

The 1,764 failures, by cause (each is listed with its reason in
`known-failures.txt`):

- 1,310 schemas that XSD 1.0 calls invalid load here: the schema's own
  correctness is not checked.
- 198 instance tests whose schema does not load, most of them through
  the next two causes.
- 61 schemas use `xs:redefine`; the introspection set imports XLink from
  a URL, and remote imports are not fetched.
- 99 invalid documents accepted, and the rest valid documents or schemas
  refused: wildcards, `xsi:type`, identity constraints, name characters,
  XML 1.1 documents and a few others.

Running the suite found and fixed: content-model checking that took
minutes on a document with about 2,000 children (now remembered per
position, with a work bound); range facets on the partial date types and
durations, class subtraction and most Unicode block escapes in patterns
(all refused before); one schema from several root documents
(`LoadFiles`).

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
