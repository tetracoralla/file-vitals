package inspector

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/tetracoralla/file-vitals/internal/version"
)

func inspect(ctx context.Context, source Source, options Options) Result {
	result := baseResult(source, options)
	if source.File == nil {
		return errorResult(source, options, "E_INVALID_SOURCE", "No open file was provided.")
	}
	stat, err := source.File.Stat()
	if err != nil {
		return errorResult(source, options, "E_FILE_STAT", "The file could not be inspected.")
	}
	if !stat.Mode().IsRegular() {
		return errorResult(source, options, "E_NOT_REGULAR_FILE", "Only regular files can be inspected.")
	}
	// The already-open descriptor is the authority for file facts. Callers may
	// provide a stale or fabricated Source.Size; trusting it can suppress reads,
	// misreport the file, or drive parser bounds from non-file input.
	source.Size = stat.Size()
	result.File.SizeBytes = source.Size
	if modified := stat.ModTime(); !modified.IsZero() {
		result.File.ModifiedUTC = modified.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	} else {
		result.File.ModifiedUTC = ""
	}

	header, _, err := readAtMost(source.File, source.Size, 64*1024)
	if err != nil {
		return errorResult(source, options, "E_FILE_READ", "The file header could not be read.")
	}
	result.Identity = detectIdentityFromFile(source.File, header, source.Size, source.Name)
	addProvenance(&result, "internal-signatures", version.Version, "used")
	if result.Identity.Kind != "unknown" {
		result.Status = "ok"
	}
	if indirection := inspectIndirection(header, source.Size); indirection != nil {
		result.Indirection = indirection
		result.Identity.Kind = "text"
		result.Identity.MediaType = "text/plain"
		result.Identity.Format = "Git LFS pointer"
		result.Identity.Confidence = "exact"
		result.Identity.Candidates = append(result.Identity.Candidates, Candidate{Source: "pointer-structure", MediaType: "text/plain", Format: "Git LFS pointer", Confidence: "exact"})
		result.Status = "ok"
		reconcileIdentityExtension(&result)
	}
	if result.Identity.ExtensionMatch != nil && !*result.Identity.ExtensionMatch {
		addDiagnostic(&result, "EXTENSION_MISMATCH", "warning", "The filename extension does not match the detected byte signature.")
	}

	if options.Mode != ModeQuick {
		addFileProbe(ctx, &result, source.File)
		inspectFamily(ctx, &result, source, header, options)
	} else {
		addProvenance(&result, "family-probe", "", "skipped")
	}

	if options.ExpectedSHA256 != "" {
		expected, err := NormalizeExpectedSHA256(options.ExpectedSHA256)
		if err != nil {
			return errorResult(source, options, "E_INVALID_OPTIONS", "Expected SHA-256 must contain exactly 64 hexadecimal characters.")
		}
		options.ExpectedSHA256 = expected
		result.Integrity.ExpectedSHA256 = expected
	}
	if options.Hash == HashSHA256 || options.ExpectedSHA256 != "" {
		if source.Size > MaxHashBytes {
			makePartial(&result)
			addDiagnostic(&result, "HASH_SIZE_LIMIT", "warning", "SHA-256 was not computed because the file exceeds the fixed hash budget.")
		} else if digest, err := hashFile(ctx, source.File, source.Size); err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return errorResult(source, options, "E_TIMEOUT", "The inspection deadline was exceeded.")
			}
			makePartial(&result)
			addDiagnostic(&result, "HASH_FAILED", "warning", "SHA-256 could not be completed.")
		} else {
			result.Integrity.SHA256 = digest
			addProvenance(&result, "sha256", "", "used")
			if options.ExpectedSHA256 != "" {
				matches := digest == options.ExpectedSHA256
				result.Integrity.SHA256Matches = &matches
				if !matches {
					addDiagnostic(&result, "SHA256_MISMATCH", "warning", "The computed SHA-256 does not match the supplied expected digest.")
				}
			}
		}
	}
	result.Traits = deriveTraits(result)
	result.Constraints = deriveConstraints(result)
	if ctx.Err() != nil {
		return errorResult(source, options, "E_TIMEOUT", "The inspection deadline was exceeded.")
	}
	if err := fitResponse(&result); err != nil {
		return errorResult(source, options, "E_RESPONSE_TOO_LARGE", "The bounded result could not fit the response budget.")
	}
	return result
}

// NormalizeExpectedSHA256 is shared by public adapters so an invalid digest is
// rejected before a worker starts and normalized identically in every carrier.
func NormalizeExpectedSHA256(value string) (string, error) {
	expected := strings.ToLower(value)
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != sha256.Size || len(expected) != sha256.Size*2 {
		return "", errors.New("invalid SHA-256")
	}
	return expected, nil
}

func inspectFamily(ctx context.Context, result *Result, source Source, header []byte, options Options) {
	mediaType := result.Identity.MediaType
	extension := result.File.Extension
	structuredExtension := ""
	if result.Identity.Kind == "text" && source.Size > 0 {
		structuredExtension = extension
	}
	structured := structuredFormat(mediaType, structuredExtension)
	// Data containers are not necessarily text. Structured text formats such as
	// JSON and CSV are selected explicitly above, while binary data such as
	// SQLite must not receive text metadata or text-routing traits.
	needsText := result.Identity.Kind == "text" || structured != "" || mediaType == "image/svg+xml"
	var content []byte
	var truncated bool
	if needsText || result.Identity.Kind == "image" {
		content, truncated, _ = readAtMost(source.File, source.Size, MaxTextBytes)
	}
	if needsText {
		info := inspectTextBytes(content, truncated)
		result.Text = &info
		addProvenance(result, "text", version.Version, "used")
	}
	if structured != "" {
		validationConclusive := false
		if truncated {
			makePartial(result)
			addDiagnostic(result, "STRUCTURED_SIZE_LIMIT", "warning", "Structured syntax was not validated because the file exceeds the parse window.")
		} else {
			err := validateStructured(structured, content)
			if errors.Is(err, errStructuredLimit) {
				// The parse stopped at an internal budget, not at invalid
				// syntax: claim neither parseability nor corruption.
				result.Structured = &Structured{Format: structured}
				makePartial(result)
				addProvenance(result, "structured-"+structured, "", "used")
				addDiagnostic(result, "STRUCTURED_PARSE_LIMIT", "warning", "Structured syntax validation stopped at the parse limit; the file is not shown as invalid.")
			} else {
				validationConclusive = true
				result.Structured = &Structured{Format: structured, Parseable: boolPointer(err == nil)}
				setParseable(result, err == nil)
				addProvenance(result, "structured-"+structured, "", "used")
				if err != nil {
					makeCorrupt(result)
					addDiagnostic(result, "STRUCTURED_PARSE_FAILED", "error", "The recognized structured-text syntax is invalid.")
				} else {
					promoteStructuredIdentity(result, structured)
				}
			}
		}
		if structuredExtension != "" && validationConclusive {
			reconcileIdentityExtension(result)
		}
	}
	if mediaType == "image/svg+xml" {
		if truncated {
			result.SVG = &SVGInfo{}
		} else if result.Structured != nil && result.Structured.Parseable != nil && *result.Structured.Parseable {
			if info, err := inspectSVGStructure(content); err == nil {
				result.SVG = info
			} else {
				result.SVG = &SVGInfo{}
				makePartial(result)
				addDiagnostic(result, "SVG_STRUCTURE_LIMIT", "warning", "SVG active-content and external-reference inspection did not complete within the structural limit.")
			}
		}
	}
	mediaType = result.Identity.MediaType

	switch result.Identity.Kind {
	case "image":
		if mediaType == "image/avif" || mediaType == "image/heic" {
			handleMediaProbe(ctx, source.File, result)
		} else if imageInfo, err := inspectImage(content, mediaType); err != nil {
			if truncated {
				makePartial(result)
				addDiagnostic(result, "IMAGE_PARSE_WINDOW", "warning", "Image dimensions were not found within the parse window.")
			} else if mediaType == "image/svg+xml" && result.Structured != nil && result.Structured.Parseable != nil && *result.Structured.Parseable {
				makePartial(result)
				addDiagnostic(result, "IMAGE_PROPERTIES_UNAVAILABLE", "warning", "The SVG is valid, but absolute dimensions could not be derived.")
			} else {
				makeCorrupt(result)
				addDiagnostic(result, "IMAGE_PARSE_FAILED", "error", "The recognized image header is invalid.")
			}
		} else {
			result.Image = imageInfo
			setParseable(result, true)
			addProvenance(result, "image", version.Version, "used")
		}
	case "media", "audio", "video":
		handleMediaProbe(ctx, source.File, result)
	case "archive":
		archive, ooxml, err := inspectArchive(ctx, source.File, source.Size, mediaType, options.Mode)
		if errors.Is(err, errProbeUnavailable) {
			makePartial(result)
			addProvenance(result, "archive", "", "unavailable")
			addDiagnostic(result, "ARCHIVE_PROBE_UNAVAILABLE", "warning", "This archive format can be identified but is not enumerated by the built-in probe.")
		} else if err != nil {
			makeCorrupt(result)
			setParseable(result, false)
			addProvenance(result, "archive", "", "failed")
			addDiagnostic(result, "ARCHIVE_PARSE_FAILED", "error", "The recognized archive could not be enumerated.")
		} else {
			result.Archive = archive
			setParseable(result, true)
			addProvenance(result, "archive", version.Version, "used")
			if archive.ScanTruncated {
				makePartial(result)
				addDiagnostic(result, "ARCHIVE_SCAN_LIMIT", "warning", "Archive totals cover only the bounded scanned prefix.")
			}
			if ooxml != nil {
				result.OOXML = ooxml
				promoteOOXML(result, ooxml.Kind)
				reconcileIdentityExtension(result)
			} else if !archive.ScanTruncated && isOOXMLMediaType(extensionMediaType(result.File.Extension)) {
				reconcileIdentityExtension(result)
			}
		}
	case "document":
		if mediaType == "application/pdf" {
			if err := inspectPDF(ctx, source.File, result); errors.Is(err, errProbeUnavailable) {
				result.PDF = &PDFInfo{Version: result.Identity.FormatVersion, TextLayer: "unknown"}
				makePartial(result)
				addProvenance(result, "pdfinfo", "", "unavailable")
				addDiagnostic(result, "PDF_PROBE_UNAVAILABLE", "warning", "PDF identity is known, but page and encryption properties are unavailable.")
			} else if err != nil {
				makePartial(result)
				addProvenance(result, "pdfinfo", "", "failed")
				addDiagnostic(result, "PDF_PROBE_FAILED", "warning", "PDF properties could not be read by the optional probe.")
			} else {
				setParseable(result, true)
				addProvenance(result, "pdfinfo", "", "used")
			}
		}
	case "font":
		font, err := inspectFont(ctx, source.File, source.Size, result.Identity.Format)
		if errors.Is(err, errProbeUnavailable) {
			makePartial(result)
			addProvenance(result, "font", "", "unavailable")
			addDiagnostic(result, "FONT_PROBE_UNAVAILABLE", "warning", "Font identity is known, but detailed metadata is unavailable.")
		} else if err != nil && (result.Identity.Format == "WOFF" || result.Identity.Format == "WOFF2") {
			makePartial(result)
			addProvenance(result, "font", "", "failed")
			addDiagnostic(result, "FONT_PROBE_FAILED", "warning", "The web-font container is recognized, but the optional metadata probe could not read its properties.")
		} else if err != nil {
			makeCorrupt(result)
			addProvenance(result, "font", "", "failed")
			addDiagnostic(result, "FONT_PARSE_FAILED", "error", "The recognized font structure is invalid.")
		} else {
			result.Font = font
			setParseable(result, true)
			addProvenance(result, "font", version.Version, "used")
		}
	case "binary":
		binaryInfo, err := inspectBinary(source.File, header, result.Identity.Format)
		if err != nil {
			makeCorrupt(result)
			addProvenance(result, "executable", "", "failed")
			addDiagnostic(result, "BINARY_PARSE_FAILED", "error", "The recognized executable header is invalid.")
		} else {
			result.Binary = binaryInfo
			setParseable(result, true)
			addProvenance(result, "executable", version.Version, "used")
		}
	}
}

func handleMediaProbe(ctx context.Context, file *os.File, result *Result) {
	err := inspectMedia(ctx, file, result)
	if errors.Is(err, errProbeUnavailable) {
		makePartial(result)
		addProvenance(result, "ffprobe", "", "unavailable")
		addDiagnostic(result, "MEDIA_PROBE_UNAVAILABLE", "warning", "Media identity is known, but stream properties require ffprobe.")
	} else if err != nil {
		makePartial(result)
		setParseable(result, false)
		addProvenance(result, "ffprobe", "", "failed")
		addDiagnostic(result, "MEDIA_PROBE_FAILED", "warning", "Media stream properties could not be read.")
	} else {
		setParseable(result, true)
		addProvenance(result, "ffprobe", "", "used")
		if result.Identity.Kind != "image" {
			if len(result.VideoStreams) > 0 {
				result.Identity.Kind = "video"
			} else if len(result.AudioStreams) > 0 {
				result.Identity.Kind = "audio"
			}
		}
	}
}

var structuredMediaTypes = map[string]string{"json": "application/json", "jsonl": "application/x-ndjson", "yaml": "application/yaml", "toml": "application/toml", "xml": "application/xml", "svg": "image/svg+xml", "csv": "text/csv", "tsv": "text/tab-separated-values"}

func promoteStructuredIdentity(result *Result, format string) {
	media := structuredMediaTypes[format]
	if media == "" {
		return
	}
	result.Identity.MediaType = media
	result.Identity.Format = formatForMediaType(media)
	result.Identity.Kind = kindForMediaType(media)
	if format == "csv" || format == "tsv" {
		result.Identity.Kind = "data"
	}
	result.Identity.Confidence = "high"
	result.Identity.Candidates = append(result.Identity.Candidates, Candidate{Source: "parser", MediaType: media, Format: result.Identity.Format, Confidence: "high"})
	if result.Status == "unsupported" {
		result.Status = "ok"
	}
}

var ooxmlPromotions = map[string][3]string{
	"docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "Word document", "document"},
	"docm": {"application/vnd.ms-word.document.macroEnabled.12", "Macro-enabled Word document", "document"},
	"xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "Excel workbook", "document"},
	"xlsm": {"application/vnd.ms-excel.sheet.macroEnabled.12", "Macro-enabled Excel workbook", "document"},
	"pptx": {"application/vnd.openxmlformats-officedocument.presentationml.presentation", "PowerPoint presentation", "document"},
	"pptm": {"application/vnd.ms-powerpoint.presentation.macroEnabled.12", "Macro-enabled PowerPoint presentation", "document"},
}

func promoteOOXML(result *Result, kind string) {
	value := ooxmlPromotions[kind]
	result.Identity.MediaType = value[0]
	result.Identity.Format = value[1]
	result.Identity.Kind = value[2]
	result.Identity.Confidence = "exact"
	result.Identity.Candidates = append(result.Identity.Candidates, Candidate{Source: "container-structure", MediaType: value[0], Format: value[1], Confidence: "exact"})
}

func reconcileIdentityExtension(result *Result) {
	extensionMedia := extensionMediaType(result.File.Extension)
	if extensionMedia == "" {
		return
	}
	matched := mediaCompatible(result.Identity.MediaType, extensionMedia)
	result.Identity.ExtensionMatch = boolPointer(matched)
	if matched {
		return
	}
	conflict := bounded("Extension suggests "+bounded(extensionMedia, 128)+" but verified content indicates "+bounded(result.Identity.MediaType, 128)+".", 256)
	result.Identity.Conflicts = append(result.Identity.Conflicts, conflict)
	if !hasDiagnosticCode(*result, "EXTENSION_MISMATCH") {
		addDiagnostic(result, "EXTENSION_MISMATCH", "warning", "The filename extension does not match the verified file content.")
	}
}

func hasDiagnosticCode(result Result, code string) bool {
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func deriveTraits(result Result) []string {
	traits := []string{}
	if result.Identity.Kind != "unknown" {
		traits = append(traits, "metadata_readable")
	}
	switch result.Identity.Kind {
	case "text":
		// The text routing trait claims the bounded text probe could classify the
		// bytes. An unclassifiable encoding keeps the identity evidence but must
		// not claim extractability; quick mode has no Text node yet, so the
		// identity alone carries the trait there.
		if result.Text == nil || result.Text.Encoding.Certainty != "unknown" {
			traits = append(traits, "text_extractable")
		}
	case "data":
		if result.Text != nil {
			traits = append(traits, "text_extractable")
		}
	case "image":
		traits = append(traits, "previewable", "transcodable")
	case "audio":
		traits = append(traits, "playable", "transcodable")
	case "video":
		traits = append(traits, "container", "playable", "previewable", "transcodable")
	case "media":
		traits = append(traits, "container", "playable", "transcodable")
	case "document":
		traits = append(traits, "previewable")
		if result.Identity.MediaType == "application/pdf" {
			traits = append(traits, "page_addressable")
			if result.PDF != nil && result.PDF.Encrypted != nil && !*result.PDF.Encrypted && result.PDF.TextLayer == "present" {
				traits = append(traits, "text_extractable")
			}
		}
	case "archive":
		traits = append(traits, "container", "enumerable", "extractable")
	case "font":
		traits = append(traits, "previewable")
	case "binary":
		traits = append(traits, "executable")
	}
	sort.Strings(traits)
	return traits
}

func deriveConstraints(result Result) []string {
	constraints := []string{}
	if result.Integrity.SHA256Matches != nil && !*result.Integrity.SHA256Matches {
		constraints = append(constraints, "integrity_mismatch")
	}
	if result.Indirection != nil {
		constraints = append(constraints, "indirect_content")
	}
	if result.Archive != nil {
		if result.Archive.Encrypted {
			constraints = append(constraints, "encrypted")
		}
		if result.Archive.PathFacts.AbsolutePaths > 0 || result.Archive.PathFacts.ParentPaths > 0 {
			constraints = append(constraints, "archive_unsafe_paths")
		}
		if result.Archive.PathFacts.LinkEntries > 0 {
			constraints = append(constraints, "archive_links")
		}
		if result.Archive.PathFacts.DeviceEntries > 0 {
			constraints = append(constraints, "archive_devices")
		}
	}
	if result.PDF != nil && result.PDF.Encrypted != nil && *result.PDF.Encrypted {
		constraints = append(constraints, "encrypted")
	}
	if result.OOXML != nil {
		if result.OOXML.MacroEnabled {
			constraints = append(constraints, "active_content")
		}
		if result.OOXML.ExternalRelationships > 0 {
			constraints = append(constraints, "external_references")
		}
		if result.OOXML.EmbeddedObjects > 0 {
			constraints = append(constraints, "embedded_objects")
		}
	}
	if result.SVG != nil {
		if result.SVG.ScriptCount > 0 {
			constraints = append(constraints, "active_content")
		}
		if result.SVG.ExternalHrefCount > 0 {
			constraints = append(constraints, "external_references")
		}
	}
	sort.Strings(constraints)
	return compactSortedStrings(constraints)
}

func compactSortedStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	write := 1
	for read := 1; read < len(values); read++ {
		if values[read] == values[write-1] {
			continue
		}
		values[write] = values[read]
		write++
	}
	return values[:write]
}

func hashFile(ctx context.Context, file *os.File, size int64) (string, error) {
	hash := sha256.New()
	reader := &contextReader{ctx: ctx, reader: io.NewSectionReader(file, 0, size)}
	buffer := make([]byte, 128*1024)
	if _, err := io.CopyBuffer(hash, reader, buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(p)
	}
}

func openSource(file *os.File, name string) (Source, error) {
	stat, err := file.Stat()
	if err != nil {
		return Source{}, err
	}
	return Source{File: file, Name: strings.ToValidUTF8(name, "�"), Size: stat.Size(), ModTime: stat.ModTime()}, nil
}

func SourceFromFile(file *os.File, name string) (Source, error) { return openSource(file, name) }
