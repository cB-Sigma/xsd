# Provenance

## How the code was written

Written for SigmaM2M in Go, with the standard library only, from the W3C
XML Schema 1.0 recommendations (Part 1: Structures, Part 2: Datatypes,
Second Edition) and the XPath subset XSD 1.0 defines for identity
constraints. No code was ported or copied from another implementation
(libxml2, Xerces and others were not used as sources). Comments name the
constructs supported and refused in our own words.

The code came with SigmaM2M's first commit (2026-09-27), where it served
the IEC 61850 SCL validator, the CIM payload schema gate and the optional
OpenADR 2.0 message validation, and became this module on 2026-10-06.

On 2026-10-11 the W3C XML Schema Test Suite was run against it for the
first time (the nested module `xsts`). That run led to: memoised
content-model checking with a work bound, range facets on the partial date
types and durations, class subtraction and the full Unicode 3.1 block
list in patterns, and `LoadFiles`.

## Sources

| Area (files) | Followed |
|---|---|
| Schema loading, include and import (xsd.go) | XSD 1.0 Part 1 (schema documents, components, xs:include, xs:import by schemaLocation) |
| Content models and validation (validate.go, tree.go, dom.go) | XSD 1.0 Part 1 (validation rules, xsi:type, xsi:nil); the walk over positions is our own design |
| Unique Particle Attribution (upa.go) | XSD 1.0 Part 1 (the constraint on content models), checked on a position automaton built by Glushkov's construction from the textbook description |
| Parsing limits (limits.go) | Our own design; XML 1.0 for the place and form of the XML declaration |
| Simple types and facets (simple.go) | XSD 1.0 Part 2 (built-in datatypes, facets; the order of durations by the four reference dateTimes of 3.2.6.2) |
| Regular expressions (regex.go, regexclass.go) | XSD 1.0 Part 2 appendix F, translated to RE2; block ranges from the Unicode Character Database's Blocks.txt for Unicode 3.1, as facts |
| Identity constraints (idc.go) | XSD 1.0 Part 1 section 3.11 and its XPath subset |

## Test data

`testdata/set` and `testdata/idc` are schemas written for the tests.
`TestSCLSchemaLoads` reads the IEC 61850-6 SCL code component from
`XSD_TEST_SCL_DIR` (or where the SigmaM2M repository keeps it) and is
skipped without it; `TestOpenADRSchemasLoad` reads the OpenADR Alliance
schemas from `SIGMA_TEST_OPENADR_XSD_DIR` (skipped without it). Their
claims (`// Covers: SIG-IPE-…`) are SigmaM2M requirement IDs; they mean
nothing outside SigmaM2M's compliance report.

`xsts` reads the W3C XML Schema Test Suite from a checkout the user
supplies (`XSTS_DIR`); none of its files are copied here.
`xsts/testdata/known-failures.txt` is our own record of which of its
tests fail here and why: test names and our reasons, no test content.
