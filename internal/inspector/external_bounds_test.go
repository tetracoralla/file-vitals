package inspector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tetracoralla/file-vitals/schemas"
)

func TestExternalNumericValuesStayInsideResultContract(t *testing.T) {
	for _, value := range []string{"NaN", "Inf", "-1", "1e30"} {
		if milliseconds, ok := secondsToMilliseconds(value); ok {
			t.Fatalf("duration %q accepted as %d", value, milliseconds)
		}
	}
	if milliseconds, ok := secondsToMilliseconds("1.2345"); !ok || milliseconds != 1235 {
		t.Fatalf("valid duration: milliseconds=%d ok=%t", milliseconds, ok)
	}
	for _, value := range []string{"-1/1", "1/-1", "0/1", "1/0"} {
		if rational := parseRational(value); rational != nil {
			t.Fatalf("invalid rate %q accepted as %#v", value, rational)
		}
	}
	for _, raw := range []string{"-1", "999999999999999999999999999999999999"} {
		if value, ok := nonNegativeDecimal(raw); ok || value != 0 {
			t.Fatalf("invalid non-negative decimal %q accepted: value=%d ok=%t", raw, value, ok)
		}
	}
}

func TestFileProbeDereferencesInheritedDescriptor(t *testing.T) {
	probeDir := t.TempDir()
	probe := `#!/bin/sh
case " $* " in
  *" --dereference "*) printf 'application/octet-stream\n' ;;
  *) printf 'inode/symlink\n' ;;
esac
`
	if err := os.WriteFile(filepath.Join(probeDir, "file"), []byte(probe), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", probeDir)

	result := inspectBytes(t, "unknown.bin", []byte{0, 1, 2, 3}, ModeStandard)
	if result.Status != "unsupported" || result.Identity.Kind != "unknown" || result.Identity.MediaType != "application/octet-stream" {
		t.Fatalf("descriptor path became probe identity: %#v", result)
	}
	if hasDiagnostic(result, "IDENTITY_CONFLICT") {
		t.Fatalf("descriptor path produced an identity conflict: %#v", result.Diagnostics)
	}
}

func TestMalformedMediaProbeNumbersAreOmitted(t *testing.T) {
	probeDir := t.TempDir()
	writeProbe := func(name, output string) {
		t.Helper()
		content := "#!/bin/sh\nprintf '%s\\n' '" + output + "'\n"
		if err := os.WriteFile(filepath.Join(probeDir, name), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeProbe("file", "video/mp4")
	writeProbe("ffprobe", `{"format":{"format_name":"mov","duration":"Inf","bit_rate":"-1"},"streams":[{"index":0,"codec_type":"video","codec_name":"h264","width":-1,"height":-2,"avg_frame_rate":"-1/1"},{"index":-1,"codec_type":"audio","codec_name":"aac","sample_rate":"-1","channels":-1}]}`)
	t.Setenv("PATH", probeDir)

	result := inspectBytes(t, "crafted.mp4", []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, ModeStandard)
	if result.Status != "ok" || result.Media == nil || result.Media.DurationMS != nil || result.Media.BitrateBPS != nil {
		t.Fatalf("malformed media facts were not omitted: %#v", result)
	}
	if len(result.VideoStreams) != 1 || result.VideoStreams[0].Width != 0 || result.VideoStreams[0].Height != 0 || result.VideoStreams[0].FPS != nil || len(result.AudioStreams) != 0 {
		t.Fatalf("malformed stream facts were not bounded: %#v %#v", result.VideoStreams, result.AudioStreams)
	}
	if err := schemas.ValidateInspectionResult(result); err != nil {
		t.Fatalf("malformed probe output escaped the published schema: %v", err)
	}
}

func TestStillImageProbeDoesNotBecomeVideo(t *testing.T) {
	probeDir := t.TempDir()
	writeProbe := func(name, output string) {
		t.Helper()
		content := "#!/bin/sh\nprintf '%s\\n' '" + output + "'\n"
		if err := os.WriteFile(filepath.Join(probeDir, name), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeProbe("file", "image/avif")
	writeProbe("ffprobe", `{"format":{"format_name":"avif"},"streams":[{"index":0,"codec_type":"video","codec_name":"av1","width":320,"height":240,"pix_fmt":"yuva420p","avg_frame_rate":"1/1"}]}`)
	t.Setenv("PATH", probeDir)

	result := inspectBytes(t, "still.avif", []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'a', 'v', 'i', 'f'}, ModeStandard)
	if result.Status != "ok" || result.Identity.Kind != "image" || result.Image == nil || result.Image.Width != 320 || result.Image.Height != 240 {
		t.Fatalf("still-image identity was not preserved: %#v", result)
	}
	if result.Integrity.Parseable == nil || !*result.Integrity.Parseable {
		t.Fatalf("valid still-image probe was not parseable: %#v", result.Integrity)
	}

	writeProbe("ffprobe", `{"format":{"format_name":"avif"},"streams":[{"index":0,"codec_type":"video","codec_name":"av1","width":-1,"height":240,"pix_fmt":"yuv420p"}]}`)
	invalid := inspectBytes(t, "invalid.avif", []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'a', 'v', 'i', 'f'}, ModeStandard)
	if invalid.Status != "partial" || invalid.Identity.Kind != "image" || invalid.Image != nil || !hasDiagnostic(invalid, "MEDIA_PROBE_FAILED") {
		t.Fatalf("invalid still-image dimensions were accepted: %#v", invalid)
	}
}

func TestIdentityConflictsStayInsideSchemaBudget(t *testing.T) {
	result := baseResult(Source{Name: "mystery.json"}, Options{Mode: ModeStandard, Timeout: 5 * time.Second})
	result.Identity.MediaType = strings.Repeat("x", 128)
	reconcileIdentityExtension(&result)
	if len(result.Identity.Conflicts) != 1 {
		t.Fatalf("expected one conflict: %#v", result.Identity.Conflicts)
	}
	if len(result.Identity.Conflicts[0]) > 256 {
		t.Fatalf("conflict exceeds schema maxLength 256: %d", len(result.Identity.Conflicts[0]))
	}
	if err := schemas.ValidateInspectionResult(result); err != nil {
		t.Fatalf("result left the published schema: %v", err)
	}
}
