package inspector

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

type signature struct {
	mediaType  string
	format     string
	kind       string
	version    string
	confidence string
}

func detectIdentity(header []byte, size int64, name string) Identity {
	sig := sniffSignature(header, size)
	identity := Identity{
		Kind: sig.kind, MediaType: sig.mediaType, Format: sig.format,
		FormatVersion: sig.version, Confidence: sig.confidence,
		Candidates: []Candidate{}, Conflicts: []string{},
	}
	if identity.MediaType == "" {
		identity = Identity{Kind: "unknown", MediaType: "application/octet-stream", Format: "Unknown", Confidence: "unknown", Candidates: []Candidate{}, Conflicts: []string{}}
	} else {
		identity.Candidates = append(identity.Candidates, Candidate{Source: "signature", MediaType: sig.mediaType, Format: sig.format, Confidence: sig.confidence})
	}

	ext := strings.ToLower(extensionOf(name))
	extMedia := extensionMediaType(ext)
	if extMedia != "" {
		identity.Candidates = append(identity.Candidates, Candidate{Source: "extension", MediaType: extMedia, Format: formatForMediaType(extMedia), Confidence: "probable"})
		if sig.mediaType != "" {
			if (sig.mediaType == "application/zip" && isOOXMLMediaType(extMedia)) || (sig.mediaType == "text/plain" && structuredFormat("", ext) != "") {
				return identity
			}
			matched := mediaCompatible(sig.mediaType, extMedia)
			identity.ExtensionMatch = boolPointer(matched)
			if !matched {
				identity.Conflicts = append(identity.Conflicts, bounded("Extension suggests "+bounded(extMedia, 128)+" but signature indicates "+bounded(sig.mediaType, 128)+".", 256))
			}
		}
	}
	return identity
}

func extensionOf(name string) string {
	index := strings.LastIndexByte(name, '.')
	if index < 0 || strings.ContainsAny(name[index:], `/\\`) {
		return ""
	}
	return name[index:]
}

func sniffSignature(data []byte, size int64) signature {
	prefix := func(value []byte) bool { return len(data) >= len(value) && bytes.Equal(data[:len(value)], value) }
	switch {
	case prefix([]byte("\x89PNG\r\n\x1a\n")):
		return signature{"image/png", "PNG", "image", "", "exact"}
	case prefix([]byte("\xff\xd8\xff")):
		return signature{"image/jpeg", "JPEG", "image", "", "exact"}
	case prefix([]byte("GIF87a")), prefix([]byte("GIF89a")):
		return signature{"image/gif", "GIF", "image", string(data[3:6]), "exact"}
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return signature{"image/webp", "WebP", "image", "", "exact"}
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE":
		return signature{"audio/wav", "WAVE", "audio", "", "exact"}
	case prefix([]byte("fLaC")):
		return signature{"audio/flac", "FLAC", "audio", "", "exact"}
	case prefix([]byte("OggS")):
		return signature{"application/ogg", "Ogg", "media", "", "exact"}
	case prefix([]byte("ID3")) || looksLikeMP3Frame(data):
		return signature{"audio/mpeg", "MP3", "audio", "", "high"}
	case prefix([]byte("%PDF-")):
		version := ""
		if len(data) >= 8 {
			version = bounded(string(data[5:8]), 16)
		}
		return signature{"application/pdf", "PDF", "document", version, "exact"}
	case prefix([]byte("PK\x03\x04")), prefix([]byte("PK\x05\x06")), prefix([]byte("PK\x07\x08")):
		return signature{"application/zip", "ZIP", "archive", "", "exact"}
	case prefix([]byte("\x1f\x8b")):
		return signature{"application/gzip", "Gzip", "archive", "", "exact"}
	case prefix([]byte("BZh")):
		return signature{"application/x-bzip2", "Bzip2", "archive", "", "exact"}
	case prefix([]byte("\xfd7zXZ\x00")):
		return signature{"application/x-xz", "XZ", "archive", "", "exact"}
	case prefix([]byte("7z\xbc\xaf\x27\x1c")):
		return signature{"application/x-7z-compressed", "7-Zip", "archive", "", "exact"}
	case prefix([]byte("Rar!\x1a\x07")):
		return signature{"application/vnd.rar", "RAR", "archive", "", "exact"}
	case len(data) >= 262 && string(data[257:262]) == "ustar":
		return signature{"application/x-tar", "Tar", "archive", "", "exact"}
	case prefix([]byte("\x7fELF")):
		return signature{"application/x-elf", "ELF", "binary", "", "exact"}
	case isMachO(data):
		return signature{"application/x-mach-binary", "Mach-O", "binary", "", "exact"}
	case isPE(data):
		return signature{"application/vnd.microsoft.portable-executable", "PE", "binary", "", "exact"}
	case prefix([]byte("wOFF")):
		return signature{"font/woff", "WOFF", "font", "", "exact"}
	case prefix([]byte("wOF2")):
		return signature{"font/woff2", "WOFF2", "font", "", "exact"}
	case prefix([]byte("OTTO")):
		return signature{"font/otf", "OpenType", "font", "", "exact"}
	case (prefix([]byte{0, 1, 0, 0}) || prefix([]byte("true"))) && looksLikeSFNTDirectory(data, size):
		return signature{"font/ttf", "TrueType", "font", "", "exact"}
	case prefix([]byte("SQLite format 3\x00")):
		return signature{"application/vnd.sqlite3", "SQLite 3", "data", "3", "exact"}
	case len(data) >= 12 && string(data[4:8]) == "ftyp":
		return sniffISOBMFF(data)
	case prefix([]byte("\x1aE\xdf\xa3")):
		return sniffEBML(data)
	case isJavaClass(data):
		return signature{"application/java-vm", "Java class", "binary", "", "exact"}
	}

	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && size <= int64(len(data)) && (trimmed[0] == '{' || trimmed[0] == '[') && json.Valid(trimmed) {
		return signature{"application/json", "JSON", "data", "", "high"}
	}
	if bytes.HasPrefix(trimmed, []byte("<?xml")) {
		if bytes.Contains(bytes.ToLower(trimmed[:min(len(trimmed), 4096)]), []byte("<svg")) {
			return signature{"image/svg+xml", "SVG", "image", "", "high"}
		}
		return signature{"application/xml", "XML", "data", "", "high"}
	}
	if bytes.HasPrefix(bytes.ToLower(trimmed), []byte("<svg")) {
		return signature{"image/svg+xml", "SVG", "image", "", "high"}
	}
	if probablyText(data) {
		return signature{"text/plain", "Plain text", "text", "", "probable"}
	}
	return signature{}
}

func looksLikeMP3Frame(data []byte) bool {
	return len(data) >= 2 && data[0] == 0xff && data[1]&0xe0 == 0xe0 && data[1]&0x18 != 0x08
}

// looksLikeSFNTDirectory requires the minimal sfnt table-directory shape
// (numTables in 1..256 with the directory inside the file) before the 0x00010000
// and legacy "true" magic values may claim an exact TrueType identity. Without
// it, any text file beginning with the word "true" is hijacked as a corrupt
// font before text detection can run.
func looksLikeSFNTDirectory(data []byte, size int64) bool {
	if len(data) < 12 {
		return false
	}
	tableCount := int(binary.BigEndian.Uint16(data[4:6]))
	return tableCount >= 1 && tableCount <= 256 && int64(12+tableCount*16) <= size
}

func probablyText(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	if !utf8.Valid(data) {
		return false
	}
	bad := 0
	count := 0
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		data = data[size:]
		count++
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' && r != '\f' {
			bad++
		}
	}
	return count > 0 && bad*100/count < 2
}

func sniffISOBMFF(data []byte) signature {
	brand := ""
	if len(data) >= 12 {
		brand = string(data[8:12])
	}
	switch brand {
	case "avif", "avis":
		return signature{"image/avif", "AVIF", "image", "", "exact"}
	case "heic", "heix", "hevc", "hevx", "mif1", "msf1":
		return signature{"image/heic", "HEIF", "image", "", "exact"}
	case "qt  ":
		return signature{"video/quicktime", "QuickTime", "video", "", "exact"}
	case "M4A ", "M4B ":
		return signature{"audio/mp4", "MPEG-4 Audio", "audio", "", "exact"}
	default:
		return signature{"video/mp4", "ISO Base Media", "video", "", "exact"}
	}
}

func isMachO(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	magic := binary.BigEndian.Uint32(data[:4])
	if magic == 0xfeedface || magic == 0xcefaedfe || magic == 0xfeedfacf || magic == 0xcffaedfe {
		return true
	}
	if magic != 0xcafebabe && magic != 0xbebafeca || len(data) < 8 {
		return false
	}
	var order binary.ByteOrder = binary.BigEndian
	if magic == 0xbebafeca {
		order = binary.LittleEndian
	}
	architectures := order.Uint32(data[4:8])
	return architectures > 0 && architectures <= 32 && uint64(len(data)) >= 8+uint64(architectures)*20
}

func isJavaClass(data []byte) bool {
	if len(data) < 10 || binary.BigEndian.Uint32(data[:4]) != 0xcafebabe {
		return false
	}
	major := binary.BigEndian.Uint16(data[6:8])
	constantPoolCount := binary.BigEndian.Uint16(data[8:10])
	return major >= 45 && major <= 100 && constantPoolCount > 0
}

func sniffEBML(data []byte) signature {
	if len(data) < 6 || !bytes.Equal(data[:4], []byte("\x1aE\xdf\xa3")) {
		return signature{}
	}
	size, width, ok := readEBMLVINT(data[4:])
	if !ok {
		return signature{"application/x-ebml", "EBML", "media", "", "probable"}
	}
	end := 4 + width + int(size)
	if end > len(data) {
		end = len(data)
	}
	header := data[4+width : end]
	for offset := 0; offset+3 <= len(header); offset++ {
		if header[offset] != 0x42 || header[offset+1] != 0x82 {
			continue
		}
		length, lengthWidth, valid := readEBMLVINT(header[offset+2:])
		start := offset + 2 + lengthWidth
		if !valid || length > 32 || start+int(length) > len(header) {
			continue
		}
		docType := strings.ToLower(string(header[start : start+int(length)]))
		switch docType {
		case "webm":
			return signature{"video/webm", "WebM", "video", "", "exact"}
		case "matroska":
			return signature{"video/x-matroska", "Matroska", "video", "", "exact"}
		}
	}
	return signature{"application/x-ebml", "EBML", "media", "", "probable"}
}

func readEBMLVINT(data []byte) (uint64, int, bool) {
	if len(data) == 0 || data[0] == 0 {
		return 0, 0, false
	}
	width := 1
	mask := byte(0x80)
	for width <= 8 && data[0]&mask == 0 {
		width++
		mask >>= 1
	}
	if width > 8 || len(data) < width {
		return 0, 0, false
	}
	value := uint64(data[0] & (mask - 1))
	for index := 1; index < width; index++ {
		value = value<<8 | uint64(data[index])
	}
	unknown := uint64(1)<<(7*width) - 1
	return value, width, value != unknown
}

func isPE(data []byte) bool {
	if len(data) < 64 || string(data[:2]) != "MZ" {
		return false
	}
	offset := int(binary.LittleEndian.Uint32(data[0x3c:0x40]))
	return offset >= 0 && offset+4 <= len(data) && string(data[offset:offset+4]) == "PE\x00\x00"
}

func kindForMediaType(mediaType string) string {
	switch {
	case strings.HasPrefix(mediaType, "text/"):
		return "text"
	case strings.HasPrefix(mediaType, "image/"):
		return "image"
	case strings.HasPrefix(mediaType, "audio/"):
		return "audio"
	case strings.HasPrefix(mediaType, "video/"):
		return "video"
	case strings.HasPrefix(mediaType, "font/"):
		return "font"
	case mediaType == "application/pdf":
		return "document"
	case mediaType == "application/zip" || mediaType == "application/gzip" || strings.Contains(mediaType, "compressed") || strings.Contains(mediaType, "rar") || strings.Contains(mediaType, "tar"):
		return "archive"
	case mediaType == "application/json" || mediaType == "application/yaml" || mediaType == "application/toml" || mediaType == "application/xml" || mediaType == "application/x-ndjson" || mediaType == "application/vnd.sqlite3":
		return "data"
	case strings.Contains(mediaType, "executable") || strings.Contains(mediaType, "mach") || strings.Contains(mediaType, "elf"):
		return "binary"
	default:
		return "unknown"
	}
}

var mediaTypeFormats = map[string]string{
	"application/json": "JSON", "application/x-ndjson": "JSON Lines", "application/yaml": "YAML", "application/toml": "TOML", "application/xml": "XML",
	"text/csv": "CSV", "text/tab-separated-values": "TSV", "text/markdown": "Markdown", "text/plain": "Plain text",
	"image/png": "PNG", "image/jpeg": "JPEG", "image/gif": "GIF", "image/webp": "WebP", "image/svg+xml": "SVG",
	"application/pdf": "PDF", "application/zip": "ZIP", "application/gzip": "Gzip",
	"font/woff": "WOFF", "font/woff2": "WOFF2", "font/otf": "OpenType", "font/ttf": "TrueType",
}

func formatForMediaType(mediaType string) string {
	if value := mediaTypeFormats[mediaType]; value != "" {
		return value
	}
	return mediaType
}

func parseELFHeader(data []byte) *BinaryInfo {
	if len(data) < 20 || !bytes.Equal(data[:4], []byte("\x7fELF")) {
		return nil
	}
	info := &BinaryInfo{Format: "ELF"}
	if elf.Class(data[4]) == elf.ELFCLASS32 {
		info.Bits = 32
	} else if elf.Class(data[4]) == elf.ELFCLASS64 {
		info.Bits = 64
	}
	var order binary.ByteOrder = binary.LittleEndian
	if elf.Data(data[5]) == elf.ELFDATA2MSB {
		info.Endianness = "big"
		order = binary.BigEndian
	} else {
		info.Endianness = "little"
	}
	machine := order.Uint16(data[18:20])
	info.Architectures = []string{elfMachineName(machine)}
	return info
}

func elfMachineName(machine uint16) string {
	switch elf.Machine(machine) {
	case elf.EM_X86_64:
		return "x86_64"
	case elf.EM_386:
		return "x86"
	case elf.EM_ARM:
		return "arm"
	case elf.EM_AARCH64:
		return "arm64"
	case elf.EM_RISCV:
		return "riscv"
	default:
		return "machine-" + strings.TrimPrefix(strings.ToLower(elf.Machine(machine).String()), "em_")
	}
}
