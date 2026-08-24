package inspector

import (
	"context"
	"os"
	"time"

	"github.com/tetracoralla/file-vitals/schemas"
)

const (
	MaxResponseBytes      = 256 * 1024
	MaxProbeStdoutBytes   = 1024 * 1024
	MaxProbeStderrBytes   = 16 * 1024
	MaxMemoryBytes        = 384 * 1024 * 1024
	GoMemoryLimitBytes    = 256 * 1024 * 1024
	MaxTextBytes          = 8 * 1024 * 1024
	MaxHashBytes          = int64(1024 * 1024 * 1024)
	MaxArchiveHeaders     = 10_000
	MaxArchiveScanBytes   = 64 * 1024 * 1024
	MaxArchiveEntryNames  = 200
	MaxOOXMLMetadataBytes = 1024 * 1024
	MaxDiagnosticCount    = 64
	MaxStreamCount        = 32
)

type Mode string

const (
	ModeQuick    Mode = "quick"
	ModeStandard Mode = "standard"
	ModeDeep     Mode = "deep"
)

type HashMode string

const (
	HashNone   HashMode = "none"
	HashSHA256 HashMode = "sha256"
)

type Options struct {
	Mode      Mode
	Hash      HashMode
	Timeout   time.Duration
	ProbePath string
}

func (o Options) Normalized() Options {
	if o.Mode == "" {
		o.Mode = ModeStandard
	}
	if o.Hash == "" {
		o.Hash = HashNone
	}
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	return o
}

type Source struct {
	File    *os.File
	Name    string
	Size    int64
	ModTime time.Time
}

type Inspector struct{}

func New() *Inspector { return &Inspector{} }

func (i *Inspector) Inspect(ctx context.Context, source Source, options Options) Result {
	options = options.Normalized()
	if options.Mode != ModeQuick && options.Mode != ModeStandard && options.Mode != ModeDeep {
		return PublicError(source.Name, ModeStandard, options.Timeout.Milliseconds(), "E_INVALID_OPTIONS", "Inspection mode must be quick, standard, or deep.")
	}
	if options.Hash != HashNone && options.Hash != HashSHA256 {
		return PublicError(source.Name, options.Mode, options.Timeout.Milliseconds(), "E_INVALID_OPTIONS", "Hash mode must be none or sha256.")
	}
	result := inspect(ctx, source, options)
	if err := schemas.ValidateInspectionResult(result); err != nil {
		return PublicError(source.Name, options.Mode, options.Timeout.Milliseconds(), "E_RESULT_SCHEMA", "The inspection result did not satisfy its published schema.")
	}
	return result
}

type Result struct {
	SchemaVersion string        `json:"schema_version"`
	Status        string        `json:"status"`
	File          FileInfo      `json:"file"`
	Identity      Identity      `json:"identity"`
	Traits        []string      `json:"traits"`
	Integrity     Integrity     `json:"integrity"`
	Text          *TextInfo     `json:"text,omitempty"`
	Structured    *Structured   `json:"structured,omitempty"`
	Image         *ImageInfo    `json:"image,omitempty"`
	Media         *MediaInfo    `json:"media,omitempty"`
	VideoStreams  []VideoStream `json:"video_streams,omitempty"`
	AudioStreams  []AudioStream `json:"audio_streams,omitempty"`
	Archive       *ArchiveInfo  `json:"archive,omitempty"`
	PDF           *PDFInfo      `json:"pdf,omitempty"`
	Font          *FontInfo     `json:"font,omitempty"`
	Binary        *BinaryInfo   `json:"binary,omitempty"`
	Diagnostics   []Diagnostic  `json:"diagnostics"`
	Provenance    []Provenance  `json:"provenance"`
	Limits        AppliedLimits `json:"limits"`
	Error         *ErrorInfo    `json:"error,omitempty"`
}

type FileInfo struct {
	Name        string `json:"name"`
	SizeBytes   int64  `json:"size_bytes"`
	Extension   string `json:"extension"`
	ModifiedUTC string `json:"modified_utc,omitempty"`
}

type Candidate struct {
	Source     string `json:"source"`
	MediaType  string `json:"media_type"`
	Format     string `json:"format,omitempty"`
	Confidence string `json:"confidence"`
}

type Identity struct {
	Kind           string      `json:"kind"`
	MediaType      string      `json:"media_type"`
	Format         string      `json:"format"`
	FormatVersion  string      `json:"format_version,omitempty"`
	Confidence     string      `json:"confidence"`
	ExtensionMatch *bool       `json:"extension_match,omitempty"`
	Candidates     []Candidate `json:"candidates"`
	Conflicts      []string    `json:"conflicts"`
}

type Integrity struct {
	Readable  bool   `json:"readable"`
	Parseable *bool  `json:"parseable,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
}

type EncodingInfo struct {
	Value     string   `json:"value"`
	Certainty string   `json:"certainty"`
	Evidence  []string `json:"evidence"`
}

type TextInfo struct {
	Encoding     EncodingInfo `json:"encoding"`
	BOM          string       `json:"bom,omitempty"`
	LineCount    *int64       `json:"line_count,omitempty"`
	LineEnding   string       `json:"line_ending,omitempty"`
	FinalNewline *bool        `json:"final_newline,omitempty"`
	Sampled      bool         `json:"sampled"`
	SampleBytes  int          `json:"sample_bytes"`
}

type Structured struct {
	Format    string `json:"format"`
	Parseable *bool  `json:"parseable,omitempty"`
}

type ImageInfo struct {
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	BitDepth   int    `json:"bit_depth,omitempty"`
	ColorModel string `json:"color_model,omitempty"`
	HasAlpha   *bool  `json:"has_alpha,omitempty"`
	FrameCount int    `json:"frame_count,omitempty"`
}

type Rational struct {
	Num int64 `json:"num"`
	Den int64 `json:"den"`
}

type MediaInfo struct {
	DurationMS *int64 `json:"duration_ms,omitempty"`
	BitrateBPS *int64 `json:"bitrate_bps,omitempty"`
	Container  string `json:"container,omitempty"`
}

type VideoStream struct {
	Index       int       `json:"index"`
	Codec       string    `json:"codec"`
	Profile     string    `json:"profile,omitempty"`
	Width       int       `json:"width,omitempty"`
	Height      int       `json:"height,omitempty"`
	FPS         *Rational `json:"fps,omitempty"`
	BitrateBPS  int64     `json:"bitrate_bps,omitempty"`
	PixelFormat string    `json:"pixel_format,omitempty"`
}

type AudioStream struct {
	Index         int    `json:"index"`
	Codec         string `json:"codec"`
	SampleRateHz  int    `json:"sample_rate_hz,omitempty"`
	Channels      int    `json:"channels,omitempty"`
	ChannelLayout string `json:"channel_layout,omitempty"`
	BitrateBPS    int64  `json:"bitrate_bps,omitempty"`
}

type ArchiveEntry struct {
	Name            string `json:"name"`
	SizeBytes       int64  `json:"size_bytes"`
	CompressedBytes *int64 `json:"compressed_bytes,omitempty"`
	Directory       bool   `json:"directory"`
	Encrypted       bool   `json:"encrypted"`
}

type ArchiveInfo struct {
	Format                   string         `json:"format"`
	EntryCount               *int           `json:"entry_count,omitempty"`
	EntriesScanned           int            `json:"entries_scanned"`
	TotalUncompressedBytes   *int64         `json:"total_uncompressed_bytes,omitempty"`
	UncompressedBytesScanned int64          `json:"uncompressed_bytes_scanned"`
	Encrypted                bool           `json:"encrypted"`
	Entries                  []ArchiveEntry `json:"entries,omitempty"`
	EntriesTruncated         bool           `json:"entries_truncated"`
	ScanTruncated            bool           `json:"scan_truncated"`
}

type PDFInfo struct {
	Version   string `json:"version,omitempty"`
	PageCount int    `json:"page_count,omitempty"`
	Encrypted *bool  `json:"encrypted,omitempty"`
	Title     string `json:"title,omitempty"`
	Author    string `json:"author,omitempty"`
}

type FontAxis struct {
	Tag     string  `json:"tag"`
	Minimum float64 `json:"minimum"`
	Default float64 `json:"default"`
	Maximum float64 `json:"maximum"`
}

type FontInfo struct {
	Format     string     `json:"format"`
	Family     string     `json:"family,omitempty"`
	Subfamily  string     `json:"subfamily,omitempty"`
	Weight     int        `json:"weight,omitempty"`
	Variable   bool       `json:"variable"`
	GlyphCount int        `json:"glyph_count,omitempty"`
	Axes       []FontAxis `json:"axes,omitempty"`
}

type BinaryInfo struct {
	Format        string   `json:"format"`
	Architectures []string `json:"architectures,omitempty"`
	Bits          int      `json:"bits,omitempty"`
	Endianness    string   `json:"endianness,omitempty"`
}

type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type Provenance struct {
	Probe   string `json:"probe"`
	Version string `json:"version,omitempty"`
	Status  string `json:"status"`
}

type AppliedLimits struct {
	Mode             Mode  `json:"mode"`
	ResponseBytesMax int   `json:"response_bytes_max"`
	TimeoutMS        int64 `json:"timeout_ms"`
	MemoryBytesMax   int64 `json:"memory_bytes_max"`
	Truncated        bool  `json:"truncated,omitempty"`
}

type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
