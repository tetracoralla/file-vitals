package inspector

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func inspectBytesWithOptions(t *testing.T, name string, data []byte, options Options) Result {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	source, err := SourceFromFile(file, name)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	options.Timeout = 5 * time.Second
	return New().Inspect(ctx, source, options)
}

func TestExpectedSHA256ReturnsPredicate(t *testing.T) {
	content := []byte("verified\n")
	digest := sha256.Sum256(content)
	expected := hex.EncodeToString(digest[:])
	matched := inspectBytesWithOptions(t, "value.txt", content, Options{Mode: ModeQuick, ExpectedSHA256: strings.ToUpper(expected)})
	if matched.Status != "ok" || matched.Integrity.SHA256 != expected || matched.Integrity.ExpectedSHA256 != expected || matched.Integrity.SHA256Matches == nil || !*matched.Integrity.SHA256Matches {
		t.Fatalf("expected digest was not verified: %#v", matched)
	}
	mismatch := inspectBytesWithOptions(t, "value.txt", content, Options{Mode: ModeQuick, ExpectedSHA256: strings.Repeat("0", 64)})
	if mismatch.Integrity.SHA256Matches == nil || *mismatch.Integrity.SHA256Matches || !hasDiagnostic(mismatch, "SHA256_MISMATCH") || !containsString(mismatch.Constraints, "integrity_mismatch") {
		t.Fatalf("digest mismatch was not explicit: %#v", mismatch)
	}
	invalid := inspectBytesWithOptions(t, "value.txt", content, Options{Mode: ModeQuick, ExpectedSHA256: "not-a-digest"})
	if invalid.Status != "error" || invalid.Error == nil || invalid.Error.Code != "E_INVALID_OPTIONS" {
		t.Fatalf("invalid digest was accepted: %#v", invalid)
	}
}

func TestGitLFSPointerIsExplicitIndirection(t *testing.T) {
	oid := strings.Repeat("a", 64)
	pointer := []byte("version https://git-lfs.github.com/spec/v1\noid sha256:" + oid + "\nsize 12345\n")
	result := inspectBytes(t, "asset.psd", pointer, ModeQuick)
	if result.Indirection == nil || result.Indirection.Kind != "git_lfs_pointer" || result.Indirection.OID != "sha256:"+oid || result.Indirection.DeclaredSize != 12345 || result.Identity.Format != "Git LFS pointer" || !containsString(result.Constraints, "indirect_content") {
		t.Fatalf("LFS pointer was not characterized: %#v", result)
	}
	malformed := inspectBytes(t, "asset.psd", []byte("version https://git-lfs.github.com/spec/v1\noid sha256:short\nsize 1\n"), ModeQuick)
	if malformed.Indirection != nil || malformed.Identity.Format == "Git LFS pointer" {
		t.Fatalf("malformed pointer was promoted: %#v", malformed)
	}
	extended := []byte("version https://git-lfs.github.com/spec/v1\next-0-clean sha256:" + strings.Repeat("b", 64) + "\noid sha256:" + oid + "\nsize 12345\n")
	extendedResult := inspectBytes(t, "extended.psd", extended, ModeQuick)
	if extendedResult.Indirection == nil || extendedResult.Indirection.OID != "sha256:"+oid {
		t.Fatalf("valid Git LFS extension pointer was not recognized: %#v", extendedResult)
	}
	duplicateExtension := []byte("version https://git-lfs.github.com/spec/v1\next-0-a sha256:" + strings.Repeat("b", 64) + "\next-0-b sha256:" + strings.Repeat("c", 64) + "\noid sha256:" + oid + "\nsize 12345\n")
	if result := inspectBytes(t, "duplicate.psd", duplicateExtension, ModeQuick); result.Indirection != nil {
		t.Fatalf("invalid duplicate extension priority was accepted: %#v", result.Indirection)
	}
}

func TestModernDataAndBuildSignatures(t *testing.T) {
	cases := []struct {
		name, media, format string
		data                []byte
	}{
		{"sample.parquet", "application/vnd.apache.parquet", "Apache Parquet", append(append([]byte("PAR1"), make([]byte, 8)...), []byte("PAR1")...)},
		{"sample.arrow", "application/vnd.apache.arrow.file", "Arrow IPC file", append(append([]byte("ARROW1"), make([]byte, 8)...), []byte("ARROW1")...)},
		{"sample.feather", "application/vnd.apache.arrow.file", "Feather v1", append(append([]byte("FEA1"), make([]byte, 8)...), []byte("FEA1")...)},
		{"sample.orc", "application/vnd.apache.orc", "Apache ORC", []byte("ORC\x00\x01")},
		{"sample.avro", "application/avro", "Avro object container", []byte("Obj\x01payload")},
		{"sample.npy", "application/x-npy", "NumPy array", []byte("\x93NUMPY\x01\x00payload")},
		{"sample.h5", "application/x-hdf5", "HDF5", []byte("\x89HDF\r\n\x1a\n")},
		{"sample.wasm", "application/wasm", "WebAssembly", []byte("\x00asm\x01\x00\x00\x00")},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := inspectBytes(t, test.name, test.data, ModeQuick)
			if result.Status != "ok" || result.Identity.MediaType != test.media || result.Identity.Format != test.format || result.Identity.Confidence != "exact" {
				t.Fatalf("signature mismatch: %#v", result.Identity)
			}
		})
	}
	truncatedParquet := inspectBytes(t, "broken.parquet", []byte("PAR1not-a-footer"), ModeQuick)
	if truncatedParquet.Identity.Format == "Apache Parquet" || truncatedParquet.Identity.Confidence == "exact" {
		t.Fatalf("one-sided Parquet magic was promoted: %#v", truncatedParquet.Identity)
	}
}

func TestArchivePathAndSpecialEntryFacts(t *testing.T) {
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for _, name := range []string{"../escape.txt", "/absolute.txt", "safe.txt"} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = entry.Write([]byte("x"))
	}
	header := &zip.FileHeader{Name: "link"}
	header.SetMode(os.ModeSymlink | 0o777)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("safe.txt"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	result := inspectBytes(t, "paths.zip", data.Bytes(), ModeStandard)
	if result.Archive == nil || result.Archive.PathFacts.AbsolutePaths != 1 || result.Archive.PathFacts.ParentPaths != 1 || result.Archive.PathFacts.LinkEntries != 1 || !result.Archive.PathFacts.InspectionComplete || !containsString(result.Constraints, "archive_unsafe_paths") || !containsString(result.Constraints, "archive_links") {
		t.Fatalf("archive blockers were not reported: %#v", result)
	}
}

func TestSVGActiveContentAndExternalReferences(t *testing.T) {
	result := inspectBytes(t, "active.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>noop()</script><image href="https://example.test/a.png"/><use href="#local"/></svg>`), ModeStandard)
	if result.SVG == nil || !result.SVG.InspectionComplete || result.SVG.ScriptCount != 1 || result.SVG.ExternalHrefCount != 1 || !containsString(result.Constraints, "active_content") || !containsString(result.Constraints, "external_references") {
		t.Fatalf("SVG routing constraints were not reported: %#v", result)
	}
}

func TestMacroWorkbookStructuralFacts(t *testing.T) {
	entries := map[string]string{
		"[Content_Types].xml":        `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/xl/workbook.xml" ContentType="application/vnd.ms-excel.sheet.macroEnabled.main+xml"/><Override PartName="/xl/vbaProject.bin" ContentType="application/vnd.ms-office.vbaProject"/></Types>`,
		"_rels/.rels":                `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml":            `<workbook><sheets><sheet name="A"/><sheet name="B"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Type="externalLink" Target="https://example.test/data" TargetMode="External"/></Relationships>`,
		"xl/vbaProject.bin":          "macro",
		"xl/embeddings/object1.bin":  "object",
	}
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = entry.Write([]byte(content))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	result := inspectBytes(t, "book.xlsm", data.Bytes(), ModeStandard)
	if result.OOXML == nil || result.OOXML.Kind != "xlsm" || result.OOXML.SheetCount == nil || *result.OOXML.SheetCount != 2 || !result.OOXML.MacroEnabled || result.OOXML.ExternalRelationships != 1 || result.OOXML.EmbeddedObjects != 1 || !result.OOXML.InspectionComplete {
		t.Fatalf("OOXML structural facts were not reported: %#v", result)
	}
	for _, constraint := range []string{"active_content", "external_references", "embedded_objects"} {
		if !containsString(result.Constraints, constraint) {
			t.Fatalf("missing %s: %#v", constraint, result.Constraints)
		}
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func TestUnclassifiableTextKeepsIdentityButOmitsTextTrait(t *testing.T) {
	// A file the bounded text probe cannot classify (for example NUL-laden
	// bytes promoted by the system identity probe) must not claim the
	// text_extractable routing trait.
	uncertain := deriveTraits(Result{Identity: Identity{Kind: "text"}, Text: &TextInfo{Encoding: EncodingInfo{Value: "unknown", Certainty: "unknown"}}})
	for _, trait := range uncertain {
		if trait == "text_extractable" {
			t.Fatalf("unclassifiable text claimed extractability: %#v", uncertain)
		}
	}
	quickMode := deriveTraits(Result{Identity: Identity{Kind: "text"}})
	if !containsString(quickMode, "text_extractable") {
		t.Fatalf("quick-mode text lost the identity-backed trait: %#v", quickMode)
	}
	decodable := deriveTraits(Result{Identity: Identity{Kind: "text"}, Text: &TextInfo{Encoding: EncodingInfo{Value: "utf-8", Certainty: "probable"}}})
	if !containsString(decodable, "text_extractable") {
		t.Fatalf("decodable text lost its trait: %#v", decodable)
	}
}
