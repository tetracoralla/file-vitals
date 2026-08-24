package inspector

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tetracoralla/file-vitals/schemas"
)

func TestSVGAbsurdDimensionsAreNotPublished(t *testing.T) {
	cases := []struct {
		width  string
		valid  bool
		expect int
	}{
		{"12345", true, 12345},                       // ordinary dimension
		{"0.4", false, 0},                            // positive, but not representable as a non-zero integer pixel dimension
		{"9007199254740991", true, 9007199254740991}, // 2^53-1 remains exact
		{"9007199254740992", false, 0},               // 2^53: rejected at the bound
		{"9223372036854775808", false, 0},            // 2^63: saturated on arm64 before the fix
		{"1e400", false, 0},                          // +Inf
	}
	for _, item := range cases {
		svg := `<svg xmlns="http://www.w3.org/2000/svg" width="` + item.width + `" height="1"/>`
		result := inspectBytes(t, "dim.svg", []byte(svg), ModeStandard)
		if item.valid {
			if result.Status != "ok" || result.Image == nil || result.Image.Width != item.expect || result.Image.Height != 1 {
				t.Fatalf("valid dimension %q was rejected: %#v", item.width, result)
			}
			continue
		}
		if result.Image != nil {
			t.Fatalf("dimension %q was published: %#v", item.width, result.Image)
		}
		if result.Status != "partial" || !hasDiagnostic(result, "IMAGE_PROPERTIES_UNAVAILABLE") {
			t.Fatalf("dimension %q did not degrade to a bounded partial: status=%s diagnostics=%#v", item.width, result.Status, result.Diagnostics)
		}
	}
}

func TestTextStartingWithTrueStaysText(t *testing.T) {
	result := inspectBytes(t, "story.txt", []byte("true story about fonts\n"), ModeStandard)
	if result.Identity.Kind != "text" || result.Identity.MediaType != "text/plain" {
		t.Fatalf("text was hijacked by the legacy sfnt magic: %#v", result.Identity)
	}
	if result.Status == "corrupt" || result.Font != nil {
		t.Fatalf("plain text was reported as a broken font: status=%s font=%#v", result.Status, result.Font)
	}
	if !hasDiagnostic(result, "IDENTITY_PROBE_CONFLICT") && !hasDiagnostic(result, "IDENTITY_CONFLICT") {
		// The conflict diagnostic is optional; identity truth is the contract.
		_ = result
	}
}

func TestLegacyTrueMagicFontWithRealDirectoryStillIdentifies(t *testing.T) {
	font := []byte("true")
	font = append(font, 0, 1, 0, 0, 0, 0, 0, 0) // numTables=1
	font = append(font, 'O', 'S', '/', '2', 0, 0, 0, 0, 0, 0, 0, 28, 0, 0, 0, 6)
	font = append(font, 0, 1, 0x90, 0) // usWeightClass 400
	result := inspectBytes(t, "legacy.ttf", font, ModeStandard)
	if result.Identity.MediaType != "font/ttf" || result.Identity.Kind != "font" {
		t.Fatalf("legacy magic with a real table directory lost its identity: %#v", result.Identity)
	}
}

func TestStructuredScanLimitsReportPartialNotCorrupt(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"limited.jsonl", strings.Repeat("{\"i\":0}\n", MaxArchiveHeaders+1)},
		{"limited.csv", strings.Repeat("a,b\n", MaxArchiveHeaders+1)},
		{"limited.yaml", strings.Repeat("a: 1\n---\n", 1005)},
		{"limited.xml", "<r>" + strings.Repeat("<a/>", 100001) + "</r>"},
	}
	for _, item := range cases {
		result := inspectBytes(t, item.name, []byte(item.data), ModeStandard)
		if result.Status != "partial" {
			t.Fatalf("%s: scan limit was not a partial: status=%s diagnostics=%#v", item.name, result.Status, result.Diagnostics)
		}
		if !hasDiagnostic(result, "STRUCTURED_PARSE_LIMIT") {
			t.Fatalf("%s: limit diagnostic missing: %#v", item.name, result.Diagnostics)
		}
		if result.Structured == nil || result.Structured.Parseable != nil {
			t.Fatalf("%s: parseability was claimed across a limit: %#v", item.name, result.Structured)
		}
		if result.Integrity.Parseable != nil {
			t.Fatalf("%s: integrity parseability must stay unknown: %#v", item.name, result.Integrity)
		}
		if result.Status == "corrupt" || hasDiagnostic(result, "STRUCTURED_PARSE_FAILED") {
			t.Fatalf("%s: valid file was shown as broken: %#v", item.name, result.Diagnostics)
		}
		if result.Identity.ExtensionMatch != nil || hasDiagnostic(result, "EXTENSION_MISMATCH") || hasDiagnostic(result, "IDENTITY_CONFLICT") {
			t.Fatalf("%s: inconclusive validation produced an identity conflict: identity=%#v diagnostics=%#v", item.name, result.Identity, result.Diagnostics)
		}
	}
}

func TestTrulyInvalidStructuredFilesRemainCorrupt(t *testing.T) {
	result := inspectBytes(t, "broken.json", []byte("{\"broken\":"), ModeStandard)
	if result.Status != "corrupt" || !hasDiagnostic(result, "STRUCTURED_PARSE_FAILED") {
		t.Fatalf("invalid JSON stopped being corrupt: status=%s diagnostics=%#v", result.Status, result.Diagnostics)
	}
}

func TestStructuredSizeWindowDoesNotInventExtensionConflict(t *testing.T) {
	data := append([]byte(strings.Repeat(" ", MaxTextBytes)), []byte("{}")...)
	result := inspectBytes(t, "large.json", data, ModeStandard)
	if result.Status != "partial" || !hasDiagnostic(result, "STRUCTURED_SIZE_LIMIT") {
		t.Fatalf("size-limited structure was not partial: status=%s diagnostics=%#v", result.Status, result.Diagnostics)
	}
	if result.Structured != nil || result.Identity.ExtensionMatch != nil || hasDiagnostic(result, "EXTENSION_MISMATCH") {
		t.Fatalf("inconclusive size window produced a structure or conflict claim: %#v", result)
	}
}

func TestEmptyFilesMakeNoStructuredClaim(t *testing.T) {
	for _, name := range []string{"empty.json", "empty.jsonl", "empty.yaml", "empty.yml", "empty.toml", "empty.csv", "empty.tsv", "empty.xml", "empty.svg"} {
		result := inspectBytes(t, name, nil, ModeStandard)
		if result.Structured != nil {
			t.Fatalf("%s: empty file made a structured claim: %#v", name, result.Structured)
		}
		if result.Status != "unsupported" || result.Identity.Kind != "unknown" {
			t.Fatalf("%s: empty file was not honestly unknown: status=%s kind=%s", name, result.Status, result.Identity.Kind)
		}
	}
}

func TestRealLosslessWebPHeaderIsValid(t *testing.T) {
	// Real 1x1 opaque lossless WebP generated by libwebp. The VP8L chunk has an
	// odd payload length and therefore includes the required zero padding byte.
	data := []byte{
		'R', 'I', 'F', 'F', 28, 0, 0, 0, 'W', 'E', 'B', 'P',
		'V', 'P', '8', 'L', 15, 0, 0, 0,
		0x2f, 0, 0, 0, 0, 0x07, 0x10, 0xf5, 0x8f, 0xfe, 0x07, 0x22, 0xa2, 0xff, 0x01, 0,
	}
	result := inspectBytes(t, "tiny.webp", data, ModeStandard)
	if result.Status != "ok" || result.Image == nil {
		t.Fatalf("real VP8L WebP was rejected: status=%s diagnostics=%#v", result.Status, result.Diagnostics)
	}
	if result.Image.Width != 1 || result.Image.Height != 1 {
		t.Fatalf("real VP8L dimensions wrong: %#v", result.Image)
	}
	if result.Image.HasAlpha != nil {
		t.Fatalf("VP8L alpha hint was over-reported as decoded pixel truth: %#v", result.Image)
	}
	if truncated := inspectBytes(t, "cut.webp", data[:24], ModeStandard); truncated.Status != "corrupt" {
		t.Fatalf("truncated VP8L was not corrupt: %s", truncated.Status)
	}
	badRIFF := append([]byte(nil), data...)
	badRIFF[4] = 13
	if invalid := inspectBytes(t, "bad-size.webp", badRIFF, ModeStandard); invalid.Status != "corrupt" {
		t.Fatalf("inconsistent RIFF size was accepted: %#v", invalid)
	}
	badPadding := append([]byte(nil), data...)
	badPadding[len(badPadding)-1] = 1
	if invalid := inspectBytes(t, "bad-padding.webp", badPadding, ModeStandard); invalid.Status != "corrupt" {
		t.Fatalf("non-zero RIFF padding was accepted: %#v", invalid)
	}
	missingImage := []byte{
		'R', 'I', 'F', 'F', 22, 0, 0, 0, 'W', 'E', 'B', 'P',
		'V', 'P', '8', 'X', 10, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	}
	if invalid := inspectBytes(t, "header-only.webp", missingImage, ModeStandard); invalid.Status != "corrupt" {
		t.Fatalf("VP8X without image data was accepted: %#v", invalid)
	}
}

func TestJSONValidationCoversMultipleValues(t *testing.T) {
	valid := [][]byte{[]byte(`{}`), []byte(`  [1, 2, 3]  `), []byte(`"x"`), []byte(`null`)}
	for _, data := range valid {
		if err := validateStructured("json", data); err != nil {
			t.Fatalf("valid JSON rejected: %q %v", data, err)
		}
	}
	invalid := [][]byte{[]byte(`{} {}`), []byte(`1 2`), []byte(`{}x`), []byte(`{`), []byte(``)}
	for _, data := range invalid {
		if err := validateStructured("json", data); err == nil {
			t.Fatalf("invalid JSON accepted: %q", data)
		}
	}
}

func TestPublicErrorSanitizesInvalidModeAndMissingName(t *testing.T) {
	result := PublicError("", "bogus", 0, "E_TEST", "message")
	if result.Limits.Mode != ModeStandard || result.Limits.TimeoutMS != 5000 {
		t.Fatalf("limits were not normalized: %#v", result.Limits)
	}
	if result.File.Name != "" {
		t.Fatalf("missing name became %q instead of staying empty", result.File.Name)
	}
	if err := schemas.ValidateInspectionResult(result); err != nil {
		t.Fatalf("sanitized error left the schema: %v", err)
	}
}

func TestDescriptorStatOverridesCallerSuppliedSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actual.json")
	data := []byte("{\"actual\":true}\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	result := New().Inspect(context.Background(), Source{File: file, Name: "actual.json", Size: 0}, Options{Mode: ModeStandard, Timeout: 5 * time.Second})
	if result.File.SizeBytes != int64(len(data)) || result.Identity.MediaType != "application/json" || result.Status != "ok" {
		t.Fatalf("caller-supplied size overrode descriptor facts: %#v", result)
	}
}

func TestLibraryRejectsInvalidOptions(t *testing.T) {
	cases := []Options{
		{Mode: Mode("bogus"), Hash: HashNone, Timeout: 5 * time.Second},
		{Mode: ModeQuick, Hash: HashMode("md5"), Timeout: 5 * time.Second},
	}
	for _, options := range cases {
		result := New().Inspect(context.Background(), Source{Name: "input.bin"}, options)
		if result.Status != "error" || result.Error == nil || result.Error.Code != "E_INVALID_OPTIONS" {
			t.Fatalf("invalid library options were accepted: options=%#v result=%#v", options, result)
		}
		if err := schemas.ValidateInspectionResult(result); err != nil {
			t.Fatalf("invalid-option result left the schema: %v", err)
		}
	}
}
