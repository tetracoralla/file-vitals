package inspector

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func inspectBytes(t *testing.T, name string, data []byte, mode Mode) Result {
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
	return New().Inspect(ctx, source, Options{Mode: mode, Timeout: 5 * time.Second})
}

func TestSignatureBeatsMismatchedExtension(t *testing.T) {
	var data bytes.Buffer
	imageValue := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	imageValue.Set(0, 0, color.NRGBA{R: 255, A: 128})
	if err := png.Encode(&data, imageValue); err != nil {
		t.Fatal(err)
	}
	result := inspectBytes(t, "wrong.jpg", data.Bytes(), ModeStandard)
	if result.Identity.MediaType != "image/png" || result.Identity.ExtensionMatch == nil || *result.Identity.ExtensionMatch {
		t.Fatalf("identity mismatch: %#v", result.Identity)
	}
	if result.Image == nil || result.Image.Width != 2 || result.Image.Height != 3 || result.Image.HasAlpha == nil || !*result.Image.HasAlpha {
		t.Fatalf("image facts missing: %#v", result.Image)
	}
	if !hasDiagnostic(result, "EXTENSION_MISMATCH") {
		t.Fatalf("missing mismatch diagnostic: %#v", result.Diagnostics)
	}
}

func TestUnknownBinaryIsNotGuessedFromExtension(t *testing.T) {
	for _, mode := range []Mode{ModeQuick, ModeStandard} {
		result := inspectBytes(t, "pretend.json", []byte{0, 1, 2, 3, 4, 5, 6, 7}, mode)
		if result.Status != "unsupported" || result.Identity.Kind != "unknown" || result.Identity.MediaType != "application/octet-stream" || result.Structured != nil {
			t.Fatalf("extension became authority in %s mode: %#v", mode, result)
		}
		if len(result.Identity.Candidates) == 0 || result.Identity.Candidates[0].Source != "extension" {
			t.Fatalf("extension evidence missing: %#v", result.Identity.Candidates)
		}
	}
}

func TestEBMLDocTypeAndJavaMagicAreDistinguished(t *testing.T) {
	webm := inspectBytes(t, "sample.webm", append([]byte("\x1aE\xdf\xa3\x87\x42\x82\x84"), []byte("webm")...), ModeQuick)
	if webm.Identity.MediaType != "video/webm" || webm.Identity.ExtensionMatch == nil || !*webm.Identity.ExtensionMatch {
		t.Fatalf("WebM identity: %#v", webm.Identity)
	}
	matroska := inspectBytes(t, "sample.mkv", append([]byte("\x1aE\xdf\xa3\x8b\x42\x82\x88"), []byte("matroska")...), ModeQuick)
	if matroska.Identity.MediaType != "video/x-matroska" {
		t.Fatalf("Matroska identity: %#v", matroska.Identity)
	}
	java := inspectBytes(t, "Example.class", []byte{0xca, 0xfe, 0xba, 0xbe, 0, 0, 0, 61, 0, 1}, ModeStandard)
	if java.Status != "ok" || java.Identity.MediaType != "application/java-vm" || java.Identity.Format != "Java class" || java.Binary == nil {
		t.Fatalf("Java class identity: %#v", java)
	}
}

func TestTextEncodingCertainty(t *testing.T) {
	plain := inspectBytes(t, "plain.txt", []byte("hello\n"), ModeStandard)
	if plain.Text == nil || plain.Text.Encoding.Value != "utf-8" || plain.Text.Encoding.Certainty != "probable" {
		t.Fatalf("plain certainty: %#v", plain.Text)
	}
	bom := inspectBytes(t, "bom.txt", append([]byte{0xef, 0xbb, 0xbf}, []byte("hello\n")...), ModeStandard)
	if bom.Text == nil || bom.Text.Encoding.Certainty != "exact" || bom.Text.BOM != "utf-8" {
		t.Fatalf("BOM certainty: %#v", bom.Text)
	}
}

func TestSQLiteIsBinaryDataNotTextExtractable(t *testing.T) {
	data := make([]byte, 100)
	copy(data, []byte("SQLite format 3\x00"))
	result := inspectBytes(t, "records.sqlite", data, ModeStandard)
	if result.Status != "ok" || result.Identity.MediaType != "application/vnd.sqlite3" || result.Identity.Kind != "data" {
		t.Fatalf("SQLite identity: %#v", result.Identity)
	}
	if result.Text != nil {
		t.Fatalf("SQLite received text metadata: %#v", result.Text)
	}
	for _, trait := range result.Traits {
		if trait == "text_extractable" {
			t.Fatalf("SQLite received a text routing trait: %#v", result.Traits)
		}
	}
}

func TestMixedLFAndStandaloneCR(t *testing.T) {
	result := inspectBytes(t, "mixed.txt", []byte("one\ntwo\rthree"), ModeStandard)
	if result.Text == nil || result.Text.LineEnding != "mixed" {
		t.Fatalf("line ending: %#v", result.Text)
	}
}

func TestStructuredParsersAndInvalidJSON(t *testing.T) {
	cases := []struct{ name, content, format string }{
		{"good.json", `{"a":1}`, "json"},
		{"good.yaml", "a: 1\nb:\n  - 2\n", "yaml"},
		{"good.toml", "a = 1\n", "toml"},
		{"good.csv", "a,b\n1,2\n", "csv"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := inspectBytes(t, test.name, []byte(test.content), ModeStandard)
			if result.Structured == nil || result.Structured.Format != test.format || result.Structured.Parseable == nil || !*result.Structured.Parseable || result.Status != "ok" {
				t.Fatalf("structured result: %#v", result)
			}
		})
	}
	invalid := inspectBytes(t, "bad.json", []byte(`{"a":`), ModeStandard)
	if invalid.Status != "corrupt" || invalid.Structured == nil || invalid.Structured.Parseable == nil || *invalid.Structured.Parseable || invalid.Identity.ExtensionMatch == nil || *invalid.Identity.ExtensionMatch || !hasDiagnostic(invalid, "STRUCTURED_PARSE_FAILED") || !hasDiagnostic(invalid, "EXTENSION_MISMATCH") {
		t.Fatalf("invalid JSON result: %#v", invalid)
	}
}

func TestDeepZipAndOOXMLCharacterization(t *testing.T) {
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"/>`,
	}
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
	result := inspectBytes(t, "document.bin", data.Bytes(), ModeDeep)
	if result.Identity.MediaType != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" || result.Archive == nil {
		t.Fatalf("OOXML not promoted: %#v", result)
	}
	if result.Archive.EntryCount == nil || *result.Archive.EntryCount != len(entries) || len(result.Archive.Entries) != len(entries) || result.Archive.ScanTruncated {
		t.Fatalf("archive facts: %#v", result.Archive)
	}
}

func TestOOXMLPromotionRequiresVerifiedPackageMetadata(t *testing.T) {
	makeZIP := func(entries map[string]string) []byte {
		t.Helper()
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
		return data.Bytes()
	}
	spoof := makeZIP(map[string]string{"[Content_Types].xml": "x", "word/document.xml": "x"})
	for _, name := range []string{"spoof.bin", "spoof.docx"} {
		result := inspectBytes(t, name, spoof, ModeStandard)
		if result.Identity.MediaType != "application/zip" {
			t.Fatalf("spoof promoted for %s: %#v", name, result.Identity)
		}
		if strings.HasSuffix(name, ".docx") && (result.Identity.ExtensionMatch == nil || *result.Identity.ExtensionMatch || !hasDiagnostic(result, "EXTENSION_MISMATCH")) {
			t.Fatalf("OOXML extension mismatch absent: %#v", result)
		}
	}
	generic := makeZIP(map[string]string{"hello.txt": "hello"})
	result := inspectBytes(t, "generic.docx", generic, ModeStandard)
	if result.Identity.MediaType != "application/zip" || result.Identity.ExtensionMatch == nil || *result.Identity.ExtensionMatch {
		t.Fatalf("generic ZIP accepted as DOCX: %#v", result.Identity)
	}
	backslashSpoof := makeZIP(map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		`word\document.xml`:   "x",
	})
	result = inspectBytes(t, "backslash.docx", backslashSpoof, ModeStandard)
	if result.Identity.MediaType != "application/zip" || result.Identity.ExtensionMatch == nil || *result.Identity.ExtensionMatch {
		t.Fatalf("non-OPC backslash part accepted: %#v", result.Identity)
	}
}

func TestSVGDimensionsAndRelativeLengths(t *testing.T) {
	commented := inspectBytes(t, "commented.svg", []byte(`<!--comment--><svg width="2.54cm" height="1in" xmlns="http://www.w3.org/2000/svg"/>`), ModeStandard)
	if commented.Status != "ok" || commented.Identity.MediaType != "image/svg+xml" || commented.Identity.ExtensionMatch == nil || !*commented.Identity.ExtensionMatch || hasDiagnostic(commented, "EXTENSION_MISMATCH") || commented.Image == nil || commented.Image.Width != 96 || commented.Image.Height != 96 {
		t.Fatalf("commented SVG: %#v", commented)
	}
	viewBox := inspectBytes(t, "relative.svg", []byte(`<svg width="100%" height="100%" viewBox="0 0 120 80" xmlns="http://www.w3.org/2000/svg"/>`), ModeStandard)
	if viewBox.Image == nil || viewBox.Image.Width != 120 || viewBox.Image.Height != 80 || viewBox.Status != "ok" {
		t.Fatalf("relative SVG with viewBox: %#v", viewBox)
	}
	noDimensions := inspectBytes(t, "relative-only.svg", []byte(`<svg width="100%" height="100%" xmlns="http://www.w3.org/2000/svg"/>`), ModeStandard)
	if noDimensions.Status != "partial" || noDimensions.Image != nil || !hasDiagnostic(noDimensions, "IMAGE_PROPERTIES_UNAVAILABLE") || noDimensions.Integrity.Parseable == nil || !*noDimensions.Integrity.Parseable {
		t.Fatalf("relative SVG without viewBox: %#v", noDimensions)
	}
}

func TestAlphaMarkersAfterImageEndAreIgnored(t *testing.T) {
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewGray(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	pngResult := inspectBytes(t, "opaque.png", append(pngData.Bytes(), []byte("tRNS")...), ModeStandard)
	if pngResult.Image == nil || pngResult.Image.HasAlpha == nil || *pngResult.Image.HasAlpha {
		t.Fatalf("PNG trailing marker: %#v", pngResult.Image)
	}
	var gifData bytes.Buffer
	palette := color.Palette{color.Black, color.White}
	if err := gif.Encode(&gifData, image.NewPaletted(image.Rect(0, 0, 2, 2), palette), nil); err != nil {
		t.Fatal(err)
	}
	gifResult := inspectBytes(t, "opaque.gif", append(gifData.Bytes(), []byte{0x21, 0xf9, 0x04, 0x01, 0, 0, 0, 0}...), ModeStandard)
	if gifResult.Image == nil || gifResult.Image.HasAlpha == nil || *gifResult.Image.HasAlpha {
		t.Fatalf("GIF trailing marker: %#v", gifResult.Image)
	}
}

func TestDeepArchiveEntryReturnLimit(t *testing.T) {
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for index := 0; index < MaxArchiveEntryNames+1; index++ {
		entry, err := writer.Create(strings.Repeat("n", 8))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = entry.Write(nil)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	result := inspectBytes(t, "many.zip", data.Bytes(), ModeDeep)
	if result.Archive == nil || len(result.Archive.Entries) != MaxArchiveEntryNames || !result.Archive.EntriesTruncated {
		t.Fatalf("entry limit not applied: %#v", result.Archive)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded)+1 > MaxResponseBytes {
		t.Fatalf("response exceeded limit: %d", len(encoded))
	}
}

func TestFileProbeUsesOnlyFirstMimeLine(t *testing.T) {
	output := []byte("application/x-mach-binary\n/dev/fd/3 (for architecture arm64):\tapplication/x-mach-binary\n")
	if value := parseFileMediaType(output); value != "application/x-mach-binary" {
		t.Fatalf("unexpected MIME: %q", value)
	}
}

func TestKnownMimeAliasesDoNotConflict(t *testing.T) {
	for _, pair := range [][2]string{{"font/ttf", "font/sfnt"}, {"application/x-elf", "application/x-pie-executable"}, {"audio/wav", "audio/x-wav"}} {
		if !mediaCompatible(pair[0], pair[1]) {
			t.Fatalf("expected aliases to be compatible: %v", pair)
		}
	}
}

func TestOggQuickModeDoesNotGuessAudioOrVideo(t *testing.T) {
	result := inspectBytes(t, "container.ogg", []byte("OggS\x00\x02\x00\x00\x00\x00"), ModeQuick)
	if result.Status != "ok" || result.Identity.Kind != "media" || result.Identity.MediaType != "application/ogg" {
		t.Fatalf("Ogg container was over-classified: %#v", result.Identity)
	}
}

func TestCorruptGzipTrailerIsNotReportedParseable(t *testing.T) {
	var data bytes.Buffer
	writer := gzip.NewWriter(&data)
	if _, err := writer.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	encoded := data.Bytes()
	encoded[len(encoded)-1] ^= 0xff
	result := inspectBytes(t, "broken.gz", encoded, ModeStandard)
	if result.Status != "corrupt" || result.Integrity.Parseable == nil || *result.Integrity.Parseable || !hasDiagnostic(result, "ARCHIVE_PARSE_FAILED") {
		t.Fatalf("corrupt gzip was accepted: %#v", result)
	}
}

func hasDiagnostic(result Result, code string) bool {
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
