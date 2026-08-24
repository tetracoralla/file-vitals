package inspector

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strconv"
	"strings"
)

func addFileProbe(ctx context.Context, result *Result, file *os.File) {
	stdout, _, err := runProbe(ctx, file, "file", "--dereference", "--brief", "--mime-type", "{file}")
	if errors.Is(err, errProbeUnavailable) {
		addProvenance(result, "file", "", "unavailable")
		return
	}
	if err != nil {
		addProvenance(result, "file", "", "failed")
		addDiagnostic(result, "IDENTITY_PROBE_FAILED", "warning", "The optional system identity probe did not complete.")
		return
	}
	mediaType := parseFileMediaType(stdout)
	if mediaType == "" {
		addProvenance(result, "file", "", "failed")
		return
	}
	addProvenance(result, "file", "", "used")
	result.Identity.Candidates = append(result.Identity.Candidates, Candidate{Source: "file", MediaType: bounded(mediaType, 128), Format: formatForMediaType(mediaType), Confidence: "high"})
	if result.Identity.Kind == "unknown" && mediaType != "application/octet-stream" {
		result.Identity.Kind = kindForMediaType(mediaType)
		result.Identity.MediaType = mediaType
		result.Identity.Format = formatForMediaType(mediaType)
		result.Identity.Confidence = "high"
		if result.Identity.Kind != "unknown" {
			result.Status = "ok"
		}
	} else if mediaType != "application/octet-stream" && !mediaCompatible(result.Identity.MediaType, mediaType) && !textAndStructuredEvidence(result.Identity.MediaType, mediaType) {
		result.Identity.Conflicts = append(result.Identity.Conflicts, bounded("System probe reports "+bounded(mediaType, 128)+" while canonical identity remains "+bounded(result.Identity.MediaType, 128)+".", 256))
		addDiagnostic(result, "IDENTITY_CONFLICT", "warning", "Independent identity probes disagree; signature evidence remains canonical.")
	}
}

func textAndStructuredEvidence(first, second string) bool {
	if first == "text/plain" {
		return structuredFormat(second, "") != ""
	}
	if second == "text/plain" {
		return structuredFormat(first, "") != ""
	}
	return false
}

func parseFileMediaType(stdout []byte) string {
	line := string(stdout)
	if index := strings.IndexByte(line, '\n'); index >= 0 {
		line = line[:index]
	}
	line = strings.TrimSpace(strings.SplitN(line, ";", 2)[0])
	if index := strings.LastIndex(line, "\t"); index >= 0 {
		line = strings.TrimSpace(line[index+1:])
	}
	return bounded(line, 128)
}

type ffprobeEnvelope struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
	Streams []struct {
		Index         int    `json:"index"`
		CodecType     string `json:"codec_type"`
		CodecName     string `json:"codec_name"`
		Profile       string `json:"profile"`
		Width         int    `json:"width"`
		Height        int    `json:"height"`
		RFrameRate    string `json:"r_frame_rate"`
		AvgFrameRate  string `json:"avg_frame_rate"`
		BitRate       string `json:"bit_rate"`
		PixelFormat   string `json:"pix_fmt"`
		SampleRate    string `json:"sample_rate"`
		Channels      int    `json:"channels"`
		ChannelLayout string `json:"channel_layout"`
	} `json:"streams"`
}

func inspectMedia(ctx context.Context, file *os.File, result *Result) error {
	stdout, _, err := runProbe(ctx, file, "ffprobe", "-v", "error", "-show_format", "-show_streams", "-of", "json", "{file}")
	if err != nil {
		return err
	}
	var envelope ffprobeEnvelope
	decoder := json.NewDecoder(strings.NewReader(string(stdout)))
	if err := decoder.Decode(&envelope); err != nil {
		return err
	}
	media := &MediaInfo{Container: bounded(envelope.Format.FormatName, 128)}
	if duration, ok := secondsToMilliseconds(envelope.Format.Duration); ok {
		media.DurationMS = &duration
	}
	if bitrate, ok := positiveInt64(envelope.Format.BitRate); ok {
		media.BitrateBPS = &bitrate
	}
	result.Media = media
	for _, stream := range envelope.Streams {
		if stream.Index < 0 {
			continue
		}
		switch stream.CodecType {
		case "video":
			if len(result.VideoStreams) >= MaxStreamCount {
				continue
			}
			fps := parseRational(stream.AvgFrameRate)
			if fps == nil {
				fps = parseRational(stream.RFrameRate)
			}
			bitrate, _ := positiveInt64(stream.BitRate)
			result.VideoStreams = append(result.VideoStreams, VideoStream{Index: stream.Index, Codec: bounded(stream.CodecName, 64), Profile: bounded(stream.Profile, 64), Width: nonNegative(stream.Width), Height: nonNegative(stream.Height), FPS: fps, BitrateBPS: bitrate, PixelFormat: bounded(stream.PixelFormat, 64)})
		case "audio":
			if len(result.AudioStreams) >= MaxStreamCount {
				continue
			}
			rate, _ := nonNegativeDecimal(stream.SampleRate)
			bitrate, _ := positiveInt64(stream.BitRate)
			result.AudioStreams = append(result.AudioStreams, AudioStream{Index: stream.Index, Codec: bounded(stream.CodecName, 64), SampleRateHz: rate, Channels: nonNegative(stream.Channels), ChannelLayout: bounded(stream.ChannelLayout, 64), BitrateBPS: bitrate})
		}
	}
	if result.Identity.Kind == "image" && result.Image == nil && len(result.VideoStreams) > 0 {
		stream := result.VideoStreams[0]
		if stream.Width <= 0 || stream.Height <= 0 {
			return errors.New("image stream has invalid dimensions")
		}
		alpha := pixelFormatHasAlpha(stream.PixelFormat)
		result.Image = &ImageInfo{Width: stream.Width, Height: stream.Height, ColorModel: bounded(stream.PixelFormat, 64), HasAlpha: &alpha}
	}
	if result.Identity.Kind == "image" && result.Image == nil {
		return errors.New("no usable image stream")
	}
	if len(result.VideoStreams) == 0 && len(result.AudioStreams) == 0 && result.Image == nil {
		return errors.New("no media streams")
	}
	return nil
}

func pixelFormatHasAlpha(value string) bool {
	value = strings.ToLower(value)
	return strings.HasPrefix(value, "rgba") || strings.HasPrefix(value, "argb") || strings.HasPrefix(value, "bgra") || strings.HasPrefix(value, "abgr") || strings.HasPrefix(value, "yuva") || strings.HasPrefix(value, "gbrap")
}

func positiveInt64(value string) (int64, bool) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	return parsed, err == nil && parsed >= 0
}

func secondsToMilliseconds(value string) (int64, bool) {
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0, false
	}
	milliseconds := math.Round(seconds * 1000)
	maximumSafe := math.Nextafter(float64(math.MaxInt64), 0)
	if math.IsInf(milliseconds, 0) || milliseconds > maximumSafe {
		return 0, false
	}
	return int64(milliseconds), true
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func nonNegativeDecimal(value string) (int, bool) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, false
	}
	return parsed, true
}

func parseRational(value string) *Rational {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return nil
	}
	num, err1 := strconv.ParseInt(parts[0], 10, 64)
	den, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil || den <= 0 || num <= 0 {
		return nil
	}
	return &Rational{Num: num, Den: den}
}

func inspectPDF(ctx context.Context, file *os.File, result *Result) error {
	stdout, _, err := runProbe(ctx, file, "pdfinfo", "{file}")
	if err != nil {
		return err
	}
	info := &PDFInfo{Version: result.Identity.FormatVersion}
	for _, line := range strings.Split(string(stdout), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "pages":
			info.PageCount, _ = nonNegativeDecimal(value)
		case "encrypted":
			flag := strings.HasPrefix(strings.ToLower(value), "yes")
			info.Encrypted = &flag
		case "pdf version":
			info.Version = bounded(value, 32)
		case "title":
			info.Title = bounded(value, 256)
		case "author":
			info.Author = bounded(value, 256)
		}
	}
	result.PDF = info
	return nil
}
