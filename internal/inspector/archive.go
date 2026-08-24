package inspector

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"os"
	"path"
	"strings"
)

func inspectArchive(ctx context.Context, file *os.File, size int64, mediaType string, mode Mode) (*ArchiveInfo, string, error) {
	switch mediaType {
	case "application/zip":
		return inspectZIP(ctx, file, size, mode)
	case "application/x-tar":
		info, err := inspectTAR(ctx, io.NewSectionReader(file, 0, size), "Tar", mode)
		return info, "", err
	case "application/gzip":
		reader, err := gzip.NewReader(io.NewSectionReader(file, 0, size))
		if err != nil {
			return nil, "", err
		}
		defer reader.Close()
		limited := &countingReader{reader: reader, remaining: MaxArchiveScanBytes}
		buffer := make([]byte, 512)
		n, readErr := io.ReadFull(limited, buffer)
		if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
			return nil, "", readErr
		}
		if n >= 262 && string(buffer[257:262]) == "ustar" {
			combined := io.MultiReader(strings.NewReader(string(buffer[:n])), limited)
			info, err := inspectTAR(ctx, combined, "Tar+Gzip", mode)
			return info, "", err
		}
		copied, copyErr := io.Copy(io.Discard, limited)
		total := int64(n) + copied
		if copyErr != nil {
			return nil, "", copyErr
		}
		info := &ArchiveInfo{Format: "Gzip", EntriesScanned: 0, UncompressedBytesScanned: total, ScanTruncated: limited.hitLimit}
		if !info.ScanTruncated {
			info.TotalUncompressedBytes = &total
		}
		return info, "", nil
	default:
		return nil, "", errProbeUnavailable
	}
}

func inspectZIP(ctx context.Context, file *os.File, size int64, mode Mode) (*ArchiveInfo, string, error) {
	reader, err := zip.NewReader(file, size)
	if err != nil {
		return nil, "", err
	}
	entryCount := len(reader.File)
	info := &ArchiveInfo{Format: "ZIP", EntryCount: &entryCount, EntriesScanned: 0, Encrypted: false}
	var total int64
	entries := make(map[string]*zip.File)
	duplicates := make(map[string]bool)
	limit := len(reader.File)
	if limit > MaxArchiveHeaders {
		limit = MaxArchiveHeaders
		info.ScanTruncated = true
	}
	for index := 0; index < limit; index++ {
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		default:
		}
		entry := reader.File[index]
		name := strings.TrimPrefix(strings.ReplaceAll(entry.Name, "\\", "/"), "./")
		sizeValue, sizeOK := archiveSize(entry.UncompressedSize64)
		compressed, compressedOK := archiveSize(entry.CompressedSize64)
		if !sizeOK || !compressedOK || sizeValue > math.MaxInt64-total {
			info.ScanTruncated = true
			break
		}
		if packageName, valid := ooxmlPackageName(entry.Name); valid {
			if _, exists := entries[packageName]; exists {
				duplicates[packageName] = true
			} else {
				entries[packageName] = entry
			}
		}
		total += sizeValue
		encrypted := entry.Flags&1 != 0
		info.Encrypted = info.Encrypted || encrypted
		info.EntriesScanned++
		info.UncompressedBytesScanned = total
		if mode == ModeDeep && len(info.Entries) < MaxArchiveEntryNames {
			info.Entries = append(info.Entries, ArchiveEntry{Name: bounded(name, 256), SizeBytes: sizeValue, CompressedBytes: &compressed, Directory: entry.FileInfo().IsDir(), Encrypted: encrypted})
		}
	}
	if !info.ScanTruncated {
		info.TotalUncompressedBytes = &total
	}
	info.EntriesTruncated = mode == ModeDeep && len(reader.File) > len(info.Entries)
	ooxml := ""
	if !info.ScanTruncated {
		ooxml, _ = detectOOXML(ctx, entries, duplicates)
	}
	return info, ooxml, nil
}

type contentTypesDocument struct {
	XMLName   xml.Name              `xml:"Types"`
	Overrides []contentTypeOverride `xml:"Override"`
}

type contentTypeOverride struct {
	PartName    string `xml:"PartName,attr"`
	ContentType string `xml:"ContentType,attr"`
}

type relationshipsDocument struct {
	XMLName       xml.Name          `xml:"Relationships"`
	Relationships []packageRelation `xml:"Relationship"`
}

type packageRelation struct {
	Type       string `xml:"Type,attr"`
	Target     string `xml:"Target,attr"`
	TargetMode string `xml:"TargetMode,attr"`
}

type ooxmlKind struct {
	kind        string
	mainPart    string
	contentType string
}

func detectOOXML(ctx context.Context, entries map[string]*zip.File, duplicates map[string]bool) (string, error) {
	if entries["[Content_Types].xml"] == nil || entries["_rels/.rels"] == nil || duplicates["[Content_Types].xml"] || duplicates["_rels/.rels"] {
		return "", errors.New("required OOXML package metadata is absent or ambiguous")
	}
	remaining := int64(2 * MaxOOXMLMetadataBytes)
	contentBytes, err := readZIPMetadata(ctx, entries["[Content_Types].xml"], &remaining)
	if err != nil {
		return "", err
	}
	relationshipBytes, err := readZIPMetadata(ctx, entries["_rels/.rels"], &remaining)
	if err != nil {
		return "", err
	}
	var contentTypes contentTypesDocument
	if err := xml.Unmarshal(contentBytes, &contentTypes); err != nil || contentTypes.XMLName.Space != "http://schemas.openxmlformats.org/package/2006/content-types" {
		return "", errors.New("invalid OOXML content types metadata")
	}
	var relationships relationshipsDocument
	if err := xml.Unmarshal(relationshipBytes, &relationships); err != nil || relationships.XMLName.Space != "http://schemas.openxmlformats.org/package/2006/relationships" {
		return "", errors.New("invalid OOXML relationship metadata")
	}
	kinds := []ooxmlKind{
		{kind: "docx", mainPart: "word/document.xml", contentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"},
		{kind: "xlsx", mainPart: "xl/workbook.xml", contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"},
		{kind: "pptx", mainPart: "ppt/presentation.xml", contentType: "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"},
	}
	matched := ""
	for _, candidate := range kinds {
		if entries[candidate.mainPart] == nil || duplicates[candidate.mainPart] || !hasOOXMLContentType(contentTypes, candidate) || !hasOOXMLRelationship(relationships, candidate.mainPart) {
			continue
		}
		if matched != "" {
			return "", errors.New("multiple OOXML main document kinds are present")
		}
		matched = candidate.kind
	}
	if matched != "" {
		return matched, nil
	}
	return "", errors.New("no verified OOXML main part")
}

func ooxmlPackageName(value string) (string, bool) {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "./") {
		return "", false
	}
	cleaned := path.Clean(value)
	if cleaned != value || cleaned == "." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

func readZIPMetadata(ctx context.Context, entry *zip.File, remaining *int64) ([]byte, error) {
	if entry.UncompressedSize64 > MaxOOXMLMetadataBytes || int64(entry.UncompressedSize64) > *remaining {
		return nil, errors.New("OOXML metadata exceeds inspection limit")
	}
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	limit := int64(entry.UncompressedSize64) + 1
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: reader}, limit))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != int64(entry.UncompressedSize64) {
		return nil, errors.New("OOXML metadata size is inconsistent")
	}
	*remaining -= int64(len(data))
	return data, nil
}

func hasOOXMLContentType(document contentTypesDocument, candidate ooxmlKind) bool {
	for _, override := range document.Overrides {
		if strings.TrimPrefix(override.PartName, "/") == candidate.mainPart && override.ContentType == candidate.contentType {
			return true
		}
	}
	return false
}

func hasOOXMLRelationship(document relationshipsDocument, mainPart string) bool {
	for _, relationship := range document.Relationships {
		if relationship.TargetMode != "" && !strings.EqualFold(relationship.TargetMode, "Internal") {
			continue
		}
		if !strings.HasSuffix(relationship.Type, "/officeDocument") {
			continue
		}
		target := strings.TrimPrefix(relationship.Target, "/")
		if strings.ContainsAny(target, "\\?#") || strings.Contains(target, ":") {
			continue
		}
		cleaned := path.Clean(target)
		if cleaned == mainPart && cleaned != "." && !strings.HasPrefix(cleaned, "../") {
			return true
		}
	}
	return false
}

func inspectTAR(ctx context.Context, source io.Reader, format string, mode Mode) (*ArchiveInfo, error) {
	limited := &countingReader{reader: source, remaining: MaxArchiveScanBytes}
	reader := tar.NewReader(limited)
	info := &ArchiveInfo{Format: format}
	var total int64
	complete := false
	for info.EntriesScanned < MaxArchiveHeaders {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			if !limited.hitLimit {
				complete = true
			}
			break
		}
		if err != nil {
			if limited.hitLimit || limited.remaining <= 0 {
				info.ScanTruncated = true
				break
			}
			return nil, err
		}
		if header.Size < 0 || header.Size > math.MaxInt64-total {
			info.ScanTruncated = true
			break
		}
		info.EntriesScanned++
		total += header.Size
		info.UncompressedBytesScanned = total
		if mode == ModeDeep && len(info.Entries) < MaxArchiveEntryNames {
			entry := ArchiveEntry{Name: bounded(header.Name, 256), SizeBytes: header.Size, Directory: header.FileInfo().IsDir(), Encrypted: false}
			if format == "Tar" {
				compressed := header.Size
				entry.CompressedBytes = &compressed
			}
			info.Entries = append(info.Entries, entry)
		}
	}
	// Leaving the loop at the header cap is indistinguishable from truncation;
	// one bounded extra read proves whether the archive actually ended there.
	if !complete && !limited.hitLimit && info.EntriesScanned == MaxArchiveHeaders {
		if _, err := reader.Next(); errors.Is(err, io.EOF) && !limited.hitLimit {
			complete = true
		}
	}
	if !complete {
		info.ScanTruncated = true
	}
	if complete {
		count := info.EntriesScanned
		info.EntryCount = &count
		info.TotalUncompressedBytes = &total
	}
	info.EntriesTruncated = mode == ModeDeep && info.EntriesScanned > len(info.Entries)
	return info, nil
}

func archiveSize(value uint64) (int64, bool) {
	if value > math.MaxInt64 {
		return 0, false
	}
	return int64(value), true
}

type countingReader struct {
	reader    io.Reader
	remaining int64
	hitLimit  bool
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		r.hitLimit = true
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	return n, err
}
