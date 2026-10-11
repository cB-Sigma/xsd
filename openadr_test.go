package xsd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// openADRDir is where SIGMA_TEST_OPENADR_XSD_DIR points: a/xsd holds the
// 2.0a schema, b/xsd the 2.0b one. The files are not ours to commit.
func openADRDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("SIGMA_TEST_OPENADR_XSD_DIR")
	if dir == "" {
		t.Skip("SIGMA_TEST_OPENADR_XSD_DIR is not set: point it at a folder holding a/xsd/oadr_20a.xsd (from the OpenADR Alliance oadr20a_schema.zip) and b/xsd/oadr_20b.xsd (from Profile_B_Schema_20130701.zip, both unzipped); the schemas are not redistributable")
	}
	return dir
}

// The OpenADR 2.0a and 2.0b schema sets (several namespaces, substitution
// groups, xmldsig) load and validate documents written for this test.
//
// Covers: SIG-IPE-2340
func TestOpenADRSchemasLoad(t *testing.T) {
	dir := openADRDir(t)
	for _, c := range []struct{ sub, root string }{{"a/xsd", "oadr_20a.xsd"}, {"b/xsd", "oadr_20b.xsd"}} {
		s, err := Load(os.DirFS(filepath.Join(dir, c.sub)), c.root)
		if err != nil {
			t.Fatalf("%s: %v", c.root, err)
		}
		t.Logf("%s: %d elements, %d types", c.root, len(s.elems), len(s.types))
		for _, d := range openADRDocs[c.root] {
			err := s.Validate([]byte(d.doc))
			if d.want == "" && err != nil {
				t.Errorf("%s %s: %v", c.root, d.name, err)
			}
			if d.want != "" && (err == nil || !strings.Contains(err.Error(), d.want)) {
				t.Errorf("%s %s: %v, want %q", c.root, d.name, err, d.want)
			}
		}
	}
}

const (
	nsA = ` xmlns:oadr="http://openadr.org/oadr-2.0a/2012/07" xmlns:ei="http://docs.oasis-open.org/ns/energyinterop/201110" xmlns:pyld="http://docs.oasis-open.org/ns/energyinterop/201110/payloads"`
	nsB = ` xmlns:oadr="http://openadr.org/oadr-2.0b/2012/07" xmlns:ei="http://docs.oasis-open.org/ns/energyinterop/201110" xmlns:pyld="http://docs.oasis-open.org/ns/energyinterop/201110/payloads"`
)

var openADRDocs = map[string][]struct{ name, doc, want string }{
	"oadr_20a.xsd": {
		{"response", `<oadr:oadrResponse` + nsA + `><ei:eiResponse><ei:responseCode>200</ei:responseCode><pyld:requestID>r1</pyld:requestID></ei:eiResponse></oadr:oadrResponse>`, ""},
		{"request event", `<oadr:oadrRequestEvent` + nsA + `><pyld:eiRequestEvent><pyld:requestID>r1</pyld:requestID><ei:venID>v1</ei:venID></pyld:eiRequestEvent></oadr:oadrRequestEvent>`, ""},
		{"request event missing venID", `<oadr:oadrRequestEvent` + nsA + `><pyld:eiRequestEvent><pyld:requestID>r1</pyld:requestID></pyld:eiRequestEvent></oadr:oadrRequestEvent>`, "needs at least 1 venID"},
		{"2.0b root", `<oadr:oadrPayload` + nsB + `/>`, "not a global element"},
	},
	"oadr_20b.xsd": {
		{"poll", `<oadr:oadrPayload` + nsB + `><oadr:oadrSignedObject><oadr:oadrPoll ei:schemaVersion="2.0b"><ei:venID>v1</ei:venID></oadr:oadrPoll></oadr:oadrSignedObject></oadr:oadrPayload>`, ""},
		{"poll without venID", `<oadr:oadrPayload` + nsB + `><oadr:oadrSignedObject><oadr:oadrPoll ei:schemaVersion="2.0b"/></oadr:oadrSignedObject></oadr:oadrPayload>`, "needs at least 1 venID"},
		{"two messages", `<oadr:oadrPayload` + nsB + `><oadr:oadrSignedObject><oadr:oadrPoll><ei:venID>v1</ei:venID></oadr:oadrPoll><oadr:oadrPoll><ei:venID>v1</ei:venID></oadr:oadrPoll></oadr:oadrSignedObject></oadr:oadrPayload>`, "at most 1"},
		{"unqualified Id", `<oadr:oadrPayload` + nsB + `><oadr:oadrSignedObject Id="x"><oadr:oadrPoll><ei:venID>v1</ei:venID></oadr:oadrPoll></oadr:oadrSignedObject></oadr:oadrPayload>`, "attribute Id is not allowed"},
		{"qualified Id", `<oadr:oadrPayload` + nsB + `><oadr:oadrSignedObject oadr:Id="x"><oadr:oadrPoll><ei:venID>v1</ei:venID></oadr:oadrPoll></oadr:oadrSignedObject></oadr:oadrPayload>`, ""},
	},
}
