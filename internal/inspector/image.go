package inspector

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// maxSVGDimension bounds SVG dimensions below 2^53, where integral float64
// values remain exact and conversion to int64 is deterministic.
const maxSVGDimension = float64(1 << 53)

func inspectImage(data []byte, mediaType string) (*ImageInfo, error) {
	switch mediaType {
	case "image/webp":
		return inspectWebP(data)
	case "image/svg+xml":
		return inspectSVG(data)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	info := &ImageInfo{Width: config.Width, Height: config.Height, ColorModel: colorModelName(config.ColorModel)}
	if alpha, known := imageAlpha(data, mediaType, config.ColorModel); known {
		info.HasAlpha = &alpha
	}
	if mediaType == "image/png" && len(data) >= 26 {
		info.BitDepth = int(data[24])
	}
	return info, nil
}

func colorModelName(model color.Model) string {
	switch model {
	case color.RGBAModel:
		return "rgba"
	case color.NRGBAModel:
		return "nrgba"
	case color.RGBA64Model:
		return "rgba64"
	case color.NRGBA64Model:
		return "nrgba64"
	case color.GrayModel:
		return "gray"
	case color.Gray16Model:
		return "gray16"
	case color.CMYKModel:
		return "cmyk"
	case color.YCbCrModel:
		return "ycbcr"
	default:
		return "palette"
	}
}

func imageAlpha(data []byte, mediaType string, model color.Model) (bool, bool) {
	switch mediaType {
	case "image/jpeg":
		return false, true
	case "image/png":
		return pngAlpha(data)
	case "image/gif":
		return gifAlpha(data)
	default:
		if model == color.RGBAModel || model == color.NRGBAModel || model == color.RGBA64Model || model == color.NRGBA64Model {
			return true, true
		}
		return false, false
	}
}

func pngAlpha(data []byte) (bool, bool) {
	if len(data) < 33 || !bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return false, false
	}
	alphaFromColorType := data[25] == 4 || data[25] == 6
	for offset := 8; offset+12 <= len(data); {
		length := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
		end := uint64(offset) + 12 + length
		if end > uint64(len(data)) {
			return false, false
		}
		chunkType := data[offset+4 : offset+8]
		if chunkType[0] == 't' && chunkType[1] == 'R' && chunkType[2] == 'N' && chunkType[3] == 'S' {
			return true, true
		}
		if chunkType[0] == 'I' && chunkType[1] == 'E' && chunkType[2] == 'N' && chunkType[3] == 'D' {
			return alphaFromColorType, true
		}
		offset = int(end)
	}
	return false, false
}

func gifAlpha(data []byte) (bool, bool) {
	if len(data) < 13 || string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a" {
		return false, false
	}
	offset := 13
	if data[10]&0x80 != 0 {
		offset += 3 * (1 << (int(data[10]&0x07) + 1))
	}
	if offset > len(data) {
		return false, false
	}
	for offset < len(data) {
		switch data[offset] {
		case 0x3b:
			return false, true
		case 0x21:
			if offset+2 > len(data) {
				return false, false
			}
			label := data[offset+1]
			offset += 2
			if label == 0xf9 {
				if offset+6 > len(data) || data[offset] != 4 || data[offset+5] != 0 {
					return false, false
				}
				if data[offset+1]&0x01 != 0 {
					return true, true
				}
				offset += 6
				continue
			}
			var ok bool
			offset, ok = skipGIFSubBlocks(data, offset)
			if !ok {
				return false, false
			}
		case 0x2c:
			if offset+10 > len(data) {
				return false, false
			}
			packed := data[offset+9]
			offset += 10
			if packed&0x80 != 0 {
				offset += 3 * (1 << (int(packed&0x07) + 1))
			}
			if offset >= len(data) {
				return false, false
			}
			offset++
			var ok bool
			offset, ok = skipGIFSubBlocks(data, offset)
			if !ok {
				return false, false
			}
		default:
			return false, false
		}
	}
	return false, false
}

func skipGIFSubBlocks(data []byte, offset int) (int, bool) {
	for offset < len(data) {
		length := int(data[offset])
		offset++
		if length == 0 {
			return offset, true
		}
		if offset+length > len(data) {
			return 0, false
		}
		offset += length
	}
	return 0, false
}

func inspectWebP(data []byte) (*ImageInfo, error) {
	if len(data) < 20 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return nil, errors.New("invalid WebP header")
	}
	riffSize := uint64(binary.LittleEndian.Uint32(data[4:8]))
	riffEnd := riffSize + 8
	if riffSize < 12 || riffSize&1 != 0 || riffEnd > uint64(len(data)) {
		return nil, errors.New("invalid WebP RIFF size")
	}
	chunkSize := uint64(binary.LittleEndian.Uint32(data[16:20]))
	chunkEnd := uint64(20) + chunkSize
	paddedEnd := chunkEnd + chunkSize&1
	if chunkEnd < 20 || paddedEnd > riffEnd || paddedEnd > uint64(len(data)) {
		return nil, errors.New("invalid WebP chunk size")
	}
	if chunkSize&1 != 0 && data[chunkEnd] != 0 {
		return nil, errors.New("invalid WebP chunk padding")
	}
	chunk := string(data[12:16])
	switch chunk {
	case "VP8X":
		if chunkSize != 10 || len(data) < 30 {
			return nil, errors.New("truncated VP8X chunk")
		}
		if !webPHasImageChunk(data, paddedEnd, riffEnd) {
			return nil, errors.New("VP8X container has no image data")
		}
		width := 1 + int(data[24]) + int(data[25])<<8 + int(data[26])<<16
		height := 1 + int(data[27]) + int(data[28])<<8 + int(data[29])<<16
		alpha := data[20]&0x10 != 0
		return &ImageInfo{Width: width, Height: height, HasAlpha: &alpha, ColorModel: "yuv"}, nil
	case "VP8 ":
		if chunkSize <= 10 || len(data) < 30 || data[23] != 0x9d || data[24] != 0x01 || data[25] != 0x2a {
			return nil, errors.New("invalid VP8 frame")
		}
		width := int(binary.LittleEndian.Uint16(data[26:28]) & 0x3fff)
		height := int(binary.LittleEndian.Uint16(data[28:30]) & 0x3fff)
		alpha := false
		return &ImageInfo{Width: width, Height: height, HasAlpha: &alpha, ColorModel: "yuv"}, nil
	case "VP8L":
		if chunkSize <= 5 || len(data) < 25 || data[20] != 0x2f {
			return nil, errors.New("invalid VP8L frame")
		}
		bits := binary.LittleEndian.Uint32(data[21:25])
		if bits>>29 != 0 {
			return nil, errors.New("unsupported VP8L version")
		}
		width := int(bits&0x3fff) + 1
		height := int((bits>>14)&0x3fff) + 1
		// alpha_is_used is only an encoder hint in the VP8L bitstream, not
		// evidence that all decoded pixels are opaque or translucent.
		return &ImageInfo{Width: width, Height: height, ColorModel: "rgba"}, nil
	default:
		return nil, errors.New("unsupported WebP chunk")
	}
}

func webPHasImageChunk(data []byte, offset, riffEnd uint64) bool {
	for offset+8 <= riffEnd {
		chunkSize := uint64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		chunkEnd := offset + 8 + chunkSize
		paddedEnd := chunkEnd + chunkSize&1
		if chunkEnd < offset || paddedEnd > riffEnd || paddedEnd > uint64(len(data)) {
			return false
		}
		if chunkSize&1 != 0 && data[chunkEnd] != 0 {
			return false
		}
		switch string(data[offset : offset+4]) {
		case "VP8 ":
			return validVP8Header(data, offset+8, chunkSize)
		case "VP8L":
			return validVP8LHeader(data, offset+8, chunkSize)
		case "ANMF":
			if chunkSize > 16 && webPHasImageChunk(data, offset+8+16, chunkEnd) {
				return true
			}
		}
		offset = paddedEnd
	}
	return false
}

func validVP8Header(data []byte, payload, size uint64) bool {
	return size > 10 && payload+10 <= uint64(len(data)) &&
		data[payload+3] == 0x9d && data[payload+4] == 0x01 && data[payload+5] == 0x2a
}

func validVP8LHeader(data []byte, payload, size uint64) bool {
	if size <= 5 || payload+5 > uint64(len(data)) || data[payload] != 0x2f {
		return false
	}
	bits := binary.LittleEndian.Uint32(data[payload+1 : payload+5])
	return bits>>29 == 0
}

func inspectSVG(data []byte) (*ImageInfo, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("svg root not found")
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if strings.ToLower(start.Name.Local) != "svg" {
			return nil, errors.New("root element is not svg")
		}
		var width, height float64
		var viewBox []float64
		for _, attr := range start.Attr {
			switch strings.ToLower(attr.Name.Local) {
			case "width":
				if value, err := parseSVGNumber(attr.Value); err == nil {
					width = value
				}
			case "height":
				if value, err := parseSVGNumber(attr.Value); err == nil {
					height = value
				}
			case "viewbox":
				fields := strings.FieldsFunc(attr.Value, func(r rune) bool { return unicode.IsSpace(r) || r == ',' })
				for _, field := range fields {
					value, err := strconv.ParseFloat(field, 64)
					if err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
						viewBox = append(viewBox, value)
					} else {
						viewBox = nil
						break
					}
				}
			}
		}
		if (width <= 0 || height <= 0) && len(viewBox) == 4 {
			width, height = viewBox[2], viewBox[3]
		}
		maximumDimension := math.Nextafter(maxSVGDimension, 0)
		if platformMaximum := float64(int(^uint(0) >> 1)); platformMaximum < maximumDimension {
			maximumDimension = platformMaximum
		}
		if width <= 0 || height <= 0 || math.IsNaN(width) || math.IsNaN(height) || math.IsInf(width, 0) || math.IsInf(height, 0) || width > maximumDimension || height > maximumDimension {
			return nil, errors.New("svg has no absolute dimensions or viewBox")
		}
		roundedWidth, roundedHeight := int(math.Round(width)), int(math.Round(height))
		if roundedWidth <= 0 || roundedHeight <= 0 {
			return nil, errors.New("svg dimensions round below one pixel")
		}
		alpha := true
		return &ImageInfo{Width: roundedWidth, Height: roundedHeight, HasAlpha: &alpha, ColorModel: "vector"}, nil
	}
}

func parseSVGNumber(value string) (float64, error) {
	value = strings.TrimSpace(value)
	end := 0
	for end < len(value) && (value[end] == '-' || value[end] == '+' || value[end] == '.' || value[end] >= '0' && value[end] <= '9' || value[end] == 'e' || value[end] == 'E') {
		end++
	}
	if end == 0 {
		return 0, errors.New("not a number")
	}
	number, err := strconv.ParseFloat(value[:end], 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, errors.New("invalid number")
	}
	unit := strings.ToLower(strings.TrimSpace(value[end:]))
	scale := 0.0
	switch unit {
	case "", "px":
		scale = 1
	case "pt":
		scale = 96.0 / 72.0
	case "pc":
		scale = 16
	case "in":
		scale = 96
	case "cm":
		scale = 96.0 / 2.54
	case "mm":
		scale = 96.0 / 25.4
	case "q":
		scale = 96.0 / 101.6
	default:
		return 0, errors.New("relative or unsupported SVG length")
	}
	return number * scale, nil
}
