package xsd

import (
	"os"
	"testing"
)

// sclDir is the IEC 61850-6 SCL schema code component (SCL.2007C5, from
// the IEC; its code component licence allows keeping it verbatim, but it is
// not part of this module). The test looks for it where the SigmaM2M
// repository keeps it, or in XSD_TEST_SCL_DIR, and is skipped without it.
var sclDir = "../../spec/data/iec61850/IEC_61850-6.SCL.2007C5"

// The SCL schema (several documents, identity constraints with an XPath
// subset, patterns, unions and lists) loads, and its own examples validate.
// Covers: SIG-IPE-2395
func TestSCLSchemaLoads(t *testing.T) {
	if d := os.Getenv("XSD_TEST_SCL_DIR"); d != "" {
		sclDir = d
	}
	if _, err := os.Stat(sclDir); err != nil {
		t.Skip("the IEC 61850-6 SCL.2007C5 code component is not here; set XSD_TEST_SCL_DIR to the unzipped folder")
	}
	s, err := Load(os.DirFS(sclDir), "SCL.xsd")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"9.2.8 - Susbtation example.ssd", "Annex D - SCL example.scd"} {
		raw, err := os.ReadFile(sclDir + "/" + f)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(raw); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}
