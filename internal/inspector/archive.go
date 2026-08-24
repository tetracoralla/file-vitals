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
	"sort"
	"strings"
)

func inspectArchive(ctx context.Context, file *os.File, size int64, mediaType string, mode Mode) (*ArchiveInfo, *OOXMLInfo, error) {
	switch mediaType {
	case "application/zip":
		return inspectZIP(ctx, file, size, mode)
	case "application/x-tar":
		info, err := inspectTAR(ctx, io.NewSectionReader(file, 0, size), "Tar", mode)
		return info, nil, err
	case "application/gzip":
		reader, err := gzip.NewReader(io.NewSectionReader(file, 0, size))
		if err != nil {
			return nil, nil, err
		}
		defer reader.Close()
		limited := &countingReader{reader: reader, remaining: MaxArchiveScanBytes}
		buffer := make([]byte, 512)
		n, readErr := io.ReadFull(limited, buffer)
		if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
			return nil, nil, readErr
		}
		if n >= 262 && string(buffer[257:262]) == "ustar" {
			combined := io.MultiReader(strings.NewReader(string(buffer[:n])), limited)
			info, err := inspectTAR(ctx, combined, "Tar+Gzip", mode)
			return info, nil, err
		}
		copied, copyErr := io.Copy(io.Discard, limited)
		total := int64(n) + copied
		if copyErr != nil {
			return nil, nil, copyErr
		}
		info := &ArchiveInfo{Format: "Gzip", EntriesScanned: 0, UncompressedBytesScanned: total, ScanTruncated: limited.hitLimit}
		info.PathFacts.InspectionComplete = !info.ScanTruncated
		if !info.ScanTruncated {
			info.TotalUncompressedBytes = &total
		}
		return info, nil, nil
	default:
		return nil, nil, errProbeUnavailable
	}
}

func inspectZIP(ctx context.Context, file *os.File, size int64, mode Mode) (*ArchiveInfo, *OOXMLInfo, error) {
	reader, err := zip.NewReader(file, size)
	if err != nil {
		return nil, nil, err
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
			return nil, nil, ctx.Err()
		default:
		}
		entry := reader.File[index]
		name := strings.TrimPrefix(strings.ReplaceAll(entry.Name, "\\", "/"), "./")
		absolutePath, parentPath := archivePathFlags(entry.Name)
		if absolutePath {
			info.PathFacts.AbsolutePaths++
		}
		if parentPath {
			info.PathFacts.ParentPaths++
		}
		kind := zipEntryKind(entry)
		if kind == "symlink" || kind == "hardlink" {
			info.PathFacts.LinkEntries++
		}
		if kind == "character_device" || kind == "block_device" || kind == "fifo" {
			info.PathFacts.DeviceEntries++
		}
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
			info.Entries = append(info.Entries, ArchiveEntry{Name: bounded(name, 256), SizeBytes: sizeValue, CompressedBytes: &compressed, Directory: entry.FileInfo().IsDir(), Encrypted: encrypted, Kind: kind})
		}
	}
	info.PathFacts.InspectionComplete = !info.ScanTruncated
	if !info.ScanTruncated {
		info.TotalUncompressedBytes = &total
	}
	info.EntriesTruncated = mode == ModeDeep && len(reader.File) > len(info.Entries)
	var ooxml *OOXMLInfo
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
	kind         string
	mainPart     string
	contentTypes []string
	macroEnabled bool
}

func detectOOXML(ctx context.Context, entries map[string]*zip.File, duplicates map[string]bool) (*OOXMLInfo, error) {
	if entries["[Content_Types].xml"] == nil || entries["_rels/.rels"] == nil || duplicates["[Content_Types].xml"] || duplicates["_rels/.rels"] {
		return nil, errors.New("required OOXML package metadata is absent or ambiguous")
	}
	remaining := int64(MaxOOXMLMetadataTotal)
	contentBytes, err := readZIPMetadata(ctx, entries["[Content_Types].xml"], &remaining)
	if err != nil {
		return nil, err
	}
	relationshipBytes, err := readZIPMetadata(ctx, entries["_rels/.rels"], &remaining)
	if err != nil {
		return nil, err
	}
	var contentTypes contentTypesDocument
	if err := xml.Unmarshal(contentBytes, &contentTypes); err != nil || contentTypes.XMLName.Space != "http://schemas.openxmlformats.org/package/2006/content-types" {
		return nil, errors.New("invalid OOXML content types metadata")
	}
	var relationships relationshipsDocument
	if err := xml.Unmarshal(relationshipBytes, &relationships); err != nil || relationships.XMLName.Space != "http://schemas.openxmlformats.org/package/2006/relationships" {
		return nil, errors.New("invalid OOXML relationship metadata")
	}
	kinds := []ooxmlKind{
		{kind: "docx", mainPart: "word/document.xml", contentTypes: []string{"application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"}},
		{kind: "docm", mainPart: "word/document.xml", contentTypes: []string{"application/vnd.ms-word.document.macroEnabled.main+xml"}, macroEnabled: true},
		{kind: "xlsx", mainPart: "xl/workbook.xml", contentTypes: []string{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"}},
		{kind: "xlsm", mainPart: "xl/workbook.xml", contentTypes: []string{"application/vnd.ms-excel.sheet.macroEnabled.main+xml"}, macroEnabled: true},
		{kind: "pptx", mainPart: "ppt/presentation.xml", contentTypes: []string{"application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"}},
		{kind: "pptm", mainPart: "ppt/presentation.xml", contentTypes: []string{"application/vnd.ms-powerpoint.presentation.macroEnabled.main+xml"}, macroEnabled: true},
	}
	var matched *ooxmlKind
	for _, candidate := range kinds {
		if entries[candidate.mainPart] == nil || duplicates[candidate.mainPart] || !hasOOXMLContentType(contentTypes, candidate) || !hasOOXMLRelationship(relationships, candidate.mainPart) {
			continue
		}
		if matched != nil {
			return nil, errors.New("multiple OOXML main document kinds are present")
		}
		copy := candidate
		matched = &copy
	}
	if matched == nil {
		return nil, errors.New("no verified OOXML main part")
	}
	info := &OOXMLInfo{Kind: matched.kind, MacroEnabled: matched.macroEnabled, InspectionComplete: true}
	for name, entry := range entries {
		lowerName := strings.ToLower(name)
		if strings.Contains(lowerName, "/embeddings/") && !entry.FileInfo().IsDir() {
			info.EmbeddedObjects++
		}
		if strings.HasSuffix(lowerName, "vbaproject.bin") {
			info.MacroEnabled = true
		}
	}
	for _, override := range contentTypes.Overrides {
		lowerType := strings.ToLower(override.ContentType)
		if strings.Contains(lowerType, "macroenabled") || strings.Contains(lowerType, "vbaproject") {
			info.MacroEnabled = true
		}
	}
	if err := inspectOOXMLMainFacts(ctx, entries[matched.mainPart], matched.kind, &remaining, info); err != nil {
		info.InspectionComplete = false
	}
	info.ExternalRelationships += externalRelationshipCount(relationships)
	relationNames := make([]string, 0)
	for name := range entries {
		if name != "_rels/.rels" && strings.HasSuffix(strings.ToLower(name), ".rels") {
			relationNames = append(relationNames, name)
		}
	}
	sort.Strings(relationNames)
	if len(relationNames) > MaxOOXMLRelationships {
		relationNames = relationNames[:MaxOOXMLRelationships]
		info.InspectionComplete = false
	}
	for _, name := range relationNames {
		if duplicates[name] {
			info.InspectionComplete = false
			continue
		}
		data, readErr := readZIPMetadata(ctx, entries[name], &remaining)
		if readErr != nil {
			info.InspectionComplete = false
			break
		}
		var document relationshipsDocument
		if xml.Unmarshal(data, &document) != nil || document.XMLName.Local != "Relationships" {
			info.InspectionComplete = false
			continue
		}
		info.ExternalRelationships += externalRelationshipCount(document)
	}
	return info, nil
}

func inspectOOXMLMainFacts(ctx context.Context, entry *zip.File, kind string, remaining *int64, info *OOXMLInfo) error {
	if kind != "xlsx" && kind != "xlsm" && kind != "pptx" && kind != "pptm" {
		return nil
	}
	data, err := readZIPMetadata(ctx, entry, remaining)
	if err != nil {
		return err
	}
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	count := 0
	target := "sheet"
	if kind == "pptx" || kind == "pptm" {
		target = "sldId"
	}
	for {
		token, tokenErr := decoder.Token()
		if errors.Is(tokenErr, io.EOF) {
			break
		}
		if tokenErr != nil {
			return tokenErr
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == target {
			count++
		}
	}
	if kind == "xlsx" || kind == "xlsm" {
		info.SheetCount = &count
	} else {
		info.SlideCount = &count
	}
	return nil
}

func externalRelationshipCount(document relationshipsDocument) int {
	count := 0
	for _, relationship := range document.Relationships {
		if strings.EqualFold(relationship.TargetMode, "External") {
			count++
		}
	}
	return count
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
		if strings.TrimPrefix(override.PartName, "/") != candidate.mainPart {
			continue
		}
		for _, contentType := range candidate.contentTypes {
			if override.ContentType == contentType {
				return true
			}
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
		absolutePath, parentPath := archivePathFlags(header.Name)
		if absolutePath {
			info.PathFacts.AbsolutePaths++
		}
		if parentPath {
			info.PathFacts.ParentPaths++
		}
		kind := tarEntryKind(header)
		if kind == "symlink" || kind == "hardlink" {
			info.PathFacts.LinkEntries++
		}
		if kind == "character_device" || kind == "block_device" || kind == "fifo" {
			info.PathFacts.DeviceEntries++
		}
		info.EntriesScanned++
		total += header.Size
		info.UncompressedBytesScanned = total
		if mode == ModeDeep && len(info.Entries) < MaxArchiveEntryNames {
			entry := ArchiveEntry{Name: bounded(header.Name, 256), SizeBytes: header.Size, Directory: header.FileInfo().IsDir(), Encrypted: false, Kind: kind}
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
	info.PathFacts.InspectionComplete = complete
	if complete {
		count := info.EntriesScanned
		info.EntryCount = &count
		info.TotalUncompressedBytes = &total
	}
	info.EntriesTruncated = mode == ModeDeep && info.EntriesScanned > len(info.Entries)
	return info, nil
}

func archivePathFlags(value string) (absolute bool, parent bool) {
	normalized := strings.ReplaceAll(value, "\\", "/")
	absolute = strings.HasPrefix(normalized, "/") || len(normalized) >= 3 && ((normalized[0] >= 'A' && normalized[0] <= 'Z') || (normalized[0] >= 'a' && normalized[0] <= 'z')) && normalized[1] == ':' && normalized[2] == '/'
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			parent = true
			break
		}
	}
	return absolute, parent
}

func zipEntryKind(entry *zip.File) string {
	mode := entry.Mode()
	switch {
	case mode.IsDir():
		return "directory"
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode&os.ModeCharDevice != 0:
		return "character_device"
	case mode&os.ModeDevice != 0:
		return "block_device"
	case mode&os.ModeNamedPipe != 0:
		return "fifo"
	case mode.IsRegular():
		return "file"
	default:
		return "other"
	}
}

func tarEntryKind(header *tar.Header) string {
	switch header.Typeflag {
	case tar.TypeDir:
		return "directory"
	case tar.TypeSymlink:
		return "symlink"
	case tar.TypeLink:
		return "hardlink"
	case tar.TypeChar:
		return "character_device"
	case tar.TypeBlock:
		return "block_device"
	case tar.TypeFifo:
		return "fifo"
	case tar.TypeReg, tar.TypeRegA:
		return "file"
	default:
		return "other"
	}
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
