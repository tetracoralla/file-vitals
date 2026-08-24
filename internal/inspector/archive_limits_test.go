package inspector

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"fmt"
	"math"
	"testing"
)

func TestOOXMLIsNotPromotedFromTruncatedHeaderScan(t *testing.T) {
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   "<document/>",
	}
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = entry.Write([]byte(content))
	}
	for index := len(entries); index <= MaxArchiveHeaders; index++ {
		entry, err := writer.Create(fmt.Sprintf("extra/%05d", index))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = entry.Write(nil)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	result := inspectBytes(t, "oversized.docx", data.Bytes(), ModeStandard)
	if result.Identity.MediaType != "application/zip" || result.Archive == nil || !result.Archive.ScanTruncated {
		t.Fatalf("truncated package was promoted: %#v", result)
	}
	if !hasDiagnostic(result, "ARCHIVE_SCAN_LIMIT") {
		t.Fatalf("truncated package diagnostics missing: %#v", result.Diagnostics)
	}
}

func TestTarScanLimitIsMarkedTruncated(t *testing.T) {
	var data bytes.Buffer
	writer := tar.NewWriter(&data)
	payload := make([]byte, 67107840) // entry 1 (header + payload) plus the zero-size marker header consume MaxArchiveScanBytes exactly
	if err := writer.WriteHeader(&tar.Header{Name: "large.bin", Mode: 0o600, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteHeader(&tar.Header{Name: "marker.bin", Mode: 0o600, Size: 0}); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteHeader(&tar.Header{Name: "after.bin", Mode: 0o600, Size: 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	result := inspectBytes(t, "large.tar", data.Bytes(), ModeStandard)
	if result.Archive == nil {
		t.Fatalf("archive facts missing: %#v", result)
	}
	if !result.Archive.ScanTruncated {
		t.Fatalf("scan limit was not marked truncated: %#v", result.Archive)
	}
	if result.Archive.EntryCount != nil || result.Archive.TotalUncompressedBytes != nil {
		t.Fatalf("truncated scan reported complete totals: %#v", result.Archive)
	}
	if result.Archive.EntriesScanned != 2 {
		t.Fatalf("entries scanned: %#v", result.Archive)
	}
	if !hasDiagnostic(result, "ARCHIVE_SCAN_LIMIT") || result.Status != "partial" {
		t.Fatalf("truncation diagnostics missing: %#v", result)
	}
}

func TestTarExactlyAtHeaderCapReportsCompleteTotals(t *testing.T) {
	buildTar := func(entries int) []byte {
		t.Helper()
		var data bytes.Buffer
		writer := tar.NewWriter(&data)
		for index := 0; index < entries; index++ {
			if err := writer.WriteHeader(&tar.Header{Name: fmt.Sprintf("e/%05d", index), Mode: 0o600, Size: 0}); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return data.Bytes()
	}
	exact := inspectBytes(t, "exact.tar", buildTar(MaxArchiveHeaders), ModeStandard)
	if exact.Archive == nil {
		t.Fatalf("archive facts missing: %#v", exact)
	}
	if exact.Archive.ScanTruncated || exact.Status != "ok" {
		t.Fatalf("fully scanned archive at the header cap was claimed truncated: %#v", exact.Archive)
	}
	if exact.Archive.EntryCount == nil || *exact.Archive.EntryCount != MaxArchiveHeaders {
		t.Fatalf("exact entry count missing: %#v", exact.Archive)
	}
	if exact.Archive.TotalUncompressedBytes == nil {
		t.Fatalf("exact totals missing: %#v", exact.Archive)
	}
	beyond := inspectBytes(t, "beyond.tar", buildTar(MaxArchiveHeaders+1), ModeStandard)
	if !beyond.Archive.ScanTruncated || beyond.Archive.EntryCount != nil {
		t.Fatalf("archive past the header cap lost its truncation truth: %#v", beyond.Archive)
	}
}

func TestArchiveSizesRejectPublishedIntegerOverflow(t *testing.T) {
	if size, ok := archiveSize(uint64(math.MaxInt64)); !ok || size != math.MaxInt64 {
		t.Fatalf("maximum archive size rejected: size=%d ok=%t", size, ok)
	}
	if size, ok := archiveSize(uint64(math.MaxInt64) + 1); ok || size != 0 {
		t.Fatalf("overflowing archive size accepted: size=%d ok=%t", size, ok)
	}
}
