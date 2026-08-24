package inspector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tetracoralla/file-vitals/schemas"
)

func TestFontWeightOutsidePublishedRangeIsOmitted(t *testing.T) {
	font := []byte{0, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	font = append(font, 'O', 'S', '/', '2', 0, 0, 0, 0, 0, 0, 0, 28, 0, 0, 0, 6)
	font = append(font, 0, 0, 0, 0, 0xff, 0xff) // usWeightClass 65535
	result := inspectBytes(t, "crafted.ttf", font, ModeStandard)
	if result.Status == "error" {
		t.Fatalf("out-of-range weight broke the result contract: %#v", result.Error)
	}
	if result.Font == nil || result.Font.Weight != 0 {
		t.Fatalf("weight was not omitted: %#v", result.Font)
	}
	if err := schemas.ValidateInspectionResult(result); err != nil {
		t.Fatalf("result left the published schema: %v", err)
	}
}

func TestFontconfigWeightIsConvertedToPublishedScale(t *testing.T) {
	cases := map[string]int{
		"0": 100, "49.5": 295, "80": 400, "100": 500, "200": 700, "215": 1000,
	}
	for raw, expected := range cases {
		weight, ok := parseFontconfigWeight(raw)
		if !ok || weight != expected {
			t.Fatalf("fontconfig weight %q: weight=%d ok=%t", raw, weight, ok)
		}
	}
	for _, raw := range []string{"", "NaN", "-1", "216", "not-a-number"} {
		if weight, ok := parseFontconfigWeight(raw); ok {
			t.Fatalf("invalid fontconfig weight %q accepted as %d", raw, weight)
		}
	}
}

func TestFontProbeWeightUsesOpenTypeScale(t *testing.T) {
	probeDir := t.TempDir()
	for name, content := range map[string]string{
		"file":    "#!/bin/sh\nprintf 'font/woff\\n'\n",
		"fc-scan": "#!/bin/sh\nprintf 'Example\\nRegular\\n80\\n'\n",
	} {
		if err := os.WriteFile(filepath.Join(probeDir, name), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", probeDir)

	result := inspectBytes(t, "example.woff", []byte("wOFF\x00\x01\x00\x00"), ModeStandard)
	if result.Status != "ok" || result.Font == nil || result.Font.Weight != 400 {
		t.Fatalf("fontconfig weight was not normalized: %#v", result)
	}
}

func TestUnsupportedWebFontProbeDoesNotClaimCorruption(t *testing.T) {
	probeDir := t.TempDir()
	for name, content := range map[string]string{
		"file":    "#!/bin/sh\nprintf 'application/octet-stream\\n'\n",
		"fc-scan": "#!/bin/sh\nexit 1\n",
	} {
		if err := os.WriteFile(filepath.Join(probeDir, name), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", probeDir)

	result := inspectBytes(t, "example.woff2", []byte("wOF2\x00\x01\x00\x00"), ModeStandard)
	if result.Status != "partial" || result.Identity.MediaType != "font/woff2" || result.Font != nil || result.Integrity.Parseable != nil {
		t.Fatalf("probe inability was reported as file corruption: %#v", result)
	}
	if !hasDiagnostic(result, "FONT_PROBE_FAILED") {
		t.Fatalf("web-font probe failure diagnostic missing: %#v", result.Diagnostics)
	}
}
