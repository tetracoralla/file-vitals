package inspector

import (
	"testing"
)

// FuzzByteParsers feeds arbitrary bytes to the bounded byte-oriented parsers
// used by identity, text, image, and structured inspection. A panic here would
// become an internal worker failure in production.
func FuzzByteParsers(f *testing.F) {
	seeds := [][]byte{
		{},                                 // empty
		[]byte("true story about fonts\n"), // legacy sfnt magic hijack
		append([]byte("true"), 0, 1, 0, 0, 0, 0, 0, 0), // minimal legacy sfnt header
		{0, 1, 0, 0}, // sfnt version magic alone
		{'R', 'I', 'F', 'F', 13, 0, 0, 0, 'W', 'E', 'B', 'P', 'V', 'P', '8', 'L', 5, 0, 0, 0, 0x2f, 0, 0, 0, 0}, // inconsistent VP8L container
		{'R', 'I', 'F', 'F', 13, 0, 0, 0, 'W', 'E', 'B', 'P', 'V', 'P', '8', 'X', 5, 0, 0, 0, 0, 0, 0, 0, 0},    // truncated VP8X
		{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},                                                           // bare PNG magic
		[]byte("GIF89a\x01\x00\x01\x00\x00"),
		{0x1a, 0x45, 0xdf, 0xa3, 0x9f},                   // EBML header prefix
		{0xca, 0xfe, 0xba, 0xbe, 0, 0, 0x00, 0x34, 0, 1}, // fat Mach-O vs Java class
		[]byte("<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"9223372036854775808\" height=\"1\"/>"),
		[]byte("<?xml version=\"1.0\"?><svg/>"),
		[]byte("{\"broken\":"),
		[]byte("{} {}"),
		{0xef, 0xbb, 0xbf, 'h', 'i'},   // UTF-8 BOM
		{0xff, 0xfe, 0, 0, 1, 0, 0, 0}, // UTF-32LE BOM
		{0xfe, 0xff, 0, 0x41},          // UTF-16BE BOM
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			data = data[:1<<20]
		}
		size := int64(len(data))
		signature := sniffSignature(data, size)
		if signature.confidence == "" && signature.mediaType != "" {
			t.Fatalf("signature without confidence: %#v", signature)
		}
		detectIdentity(data, size, "fuzz.bin")
		decodeText(data)
		inspectTextBytes(data, false)
		pngAlpha(data)
		gifAlpha(data)
		for _, mediaType := range []string{"image/webp", "image/svg+xml"} {
			if info, err := inspectImage(data, mediaType); err == nil && info != nil {
				if info.Width < 0 || info.Height < 0 {
					t.Fatalf("negative dimensions published for %s: %#v", mediaType, info)
				}
			}
		}
		for _, format := range []string{"json", "jsonl", "yaml", "toml", "csv", "tsv", "xml", "svg"} {
			// Limit outcomes are fine; only panics and invalid states fail.
			_ = validateStructured(format, data)
		}
	})
}
