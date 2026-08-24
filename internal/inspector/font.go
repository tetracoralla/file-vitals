package inspector

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode/utf16"
)

type sfntTable struct{ offset, length uint32 }

func inspectFont(ctx context.Context, file *os.File, size int64, format string) (*FontInfo, error) {
	if format == "TrueType" || format == "OpenType" {
		if info, err := inspectSFNT(file, size, format); err == nil {
			return info, nil
		}
	}
	stdout, _, err := runProbe(ctx, file, "fc-scan", "--format", "%{family}\n%{style}\n%{weight}\n", "{file}")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(stdout), "\n")
	info := &FontInfo{Format: format}
	if len(lines) > 0 {
		info.Family = bounded(firstComma(lines[0]), 256)
	}
	if len(lines) > 1 {
		info.Subfamily = bounded(firstComma(lines[1]), 128)
	}
	if len(lines) > 2 {
		if weight, ok := parseFontconfigWeight(lines[2]); ok {
			info.Weight = weight
		}
	}
	return info, nil
}

// Fontconfig reports FC_WEIGHT values, not the 1..1000 OpenType/CSS scale used
// by the public result. This is the inverse mapping published by fontconfig's
// FcWeightFromOpenTypeDouble/FcWeightToOpenTypeDouble API.
func parseFontconfigWeight(value string) (int, bool) {
	fontconfigWeight, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsNaN(fontconfigWeight) || math.IsInf(fontconfigWeight, 0) {
		return 0, false
	}
	points := [...]struct {
		fontconfig float64
		opentype   float64
	}{
		{0, 100}, {40, 200}, {50, 300}, {55, 350}, {75, 380}, {80, 400},
		{100, 500}, {180, 600}, {200, 700}, {205, 800}, {210, 900}, {215, 1000},
	}
	if fontconfigWeight < points[0].fontconfig || fontconfigWeight > points[len(points)-1].fontconfig {
		return 0, false
	}
	for index := 1; index < len(points); index++ {
		upper := points[index]
		if fontconfigWeight > upper.fontconfig {
			continue
		}
		lower := points[index-1]
		ratio := (fontconfigWeight - lower.fontconfig) / (upper.fontconfig - lower.fontconfig)
		weight := int(math.Round(lower.opentype + ratio*(upper.opentype-lower.opentype)))
		return weight, weight >= 1 && weight <= 1000
	}
	return 0, false
}

func inspectSFNT(file *os.File, size int64, format string) (*FontInfo, error) {
	header := make([]byte, 12)
	if _, err := file.ReadAt(header, 0); err != nil {
		return nil, err
	}
	tableCount := int(binary.BigEndian.Uint16(header[4:6]))
	if tableCount <= 0 || tableCount > 256 {
		return nil, errors.New("invalid SFNT table count")
	}
	directory := make([]byte, tableCount*16)
	if _, err := file.ReadAt(directory, 12); err != nil {
		return nil, err
	}
	tables := map[string]sfntTable{}
	for index := 0; index < tableCount; index++ {
		record := directory[index*16 : index*16+16]
		tag := string(record[:4])
		offset := binary.BigEndian.Uint32(record[8:12])
		length := binary.BigEndian.Uint32(record[12:16])
		if int64(offset)+int64(length) > size {
			return nil, errors.New("SFNT table exceeds file")
		}
		tables[tag] = sfntTable{offset: offset, length: length}
	}
	info := &FontInfo{Format: format, Variable: false}
	if table, ok := tables["name"]; ok {
		info.Family, info.Subfamily = readFontNames(file, table)
	}
	if table, ok := tables["maxp"]; ok && table.length >= 6 {
		data := make([]byte, 6)
		if _, err := file.ReadAt(data, int64(table.offset)); err == nil {
			info.GlyphCount = int(binary.BigEndian.Uint16(data[4:6]))
		}
	}
	if table, ok := tables["OS/2"]; ok && table.length >= 6 {
		data := make([]byte, 6)
		if _, err := file.ReadAt(data, int64(table.offset)); err == nil {
			if weight := int(binary.BigEndian.Uint16(data[4:6])); weight >= 1 && weight <= 1000 {
				info.Weight = weight
			}
		}
	}
	if table, ok := tables["fvar"]; ok {
		info.Variable = true
		info.Axes = readFontAxes(file, table)
	}
	return info, nil
}

func readFontNames(file *os.File, table sfntTable) (string, string) {
	if table.length < 6 || table.length > 1024*1024 {
		return "", ""
	}
	data := make([]byte, int(table.length))
	if _, err := file.ReadAt(data, int64(table.offset)); err != nil {
		return "", ""
	}
	count := int(binary.BigEndian.Uint16(data[2:4]))
	storage := int(binary.BigEndian.Uint16(data[4:6]))
	if count > 1024 || 6+count*12 > len(data) || storage > len(data) {
		return "", ""
	}
	family, subfamily := "", ""
	for index := 0; index < count; index++ {
		record := data[6+index*12 : 18+index*12]
		platform := binary.BigEndian.Uint16(record[0:2])
		nameID := binary.BigEndian.Uint16(record[6:8])
		length := int(binary.BigEndian.Uint16(record[8:10]))
		offset := storage + int(binary.BigEndian.Uint16(record[10:12]))
		if (nameID != 1 && nameID != 2) || offset < 0 || offset+length > len(data) {
			continue
		}
		value := decodeFontString(data[offset:offset+length], platform)
		if nameID == 1 && family == "" {
			family = bounded(value, 256)
		}
		if nameID == 2 && subfamily == "" {
			subfamily = bounded(value, 128)
		}
	}
	return family, subfamily
}

func decodeFontString(data []byte, platform uint16) string {
	if platform == 0 || platform == 3 {
		values := make([]uint16, 0, len(data)/2)
		for len(data) >= 2 {
			values = append(values, binary.BigEndian.Uint16(data[:2]))
			data = data[2:]
		}
		return strings.TrimSpace(string(utf16.Decode(values)))
	}
	return strings.TrimSpace(string(data))
}

func readFontAxes(file *os.File, table sfntTable) []FontAxis {
	if table.length < 16 || table.length > 1024*1024 {
		return nil
	}
	data := make([]byte, int(table.length))
	if _, err := file.ReadAt(data, int64(table.offset)); err != nil {
		return nil
	}
	offset := int(binary.BigEndian.Uint16(data[4:6]))
	count := int(binary.BigEndian.Uint16(data[8:10]))
	size := int(binary.BigEndian.Uint16(data[10:12]))
	if count > 64 || size < 20 || offset < 0 || offset+count*size > len(data) {
		return nil
	}
	axes := make([]FontAxis, 0, count)
	for index := 0; index < count; index++ {
		record := data[offset+index*size:]
		axes = append(axes, FontAxis{Tag: string(record[:4]), Minimum: fixed1616(binary.BigEndian.Uint32(record[4:8])), Default: fixed1616(binary.BigEndian.Uint32(record[8:12])), Maximum: fixed1616(binary.BigEndian.Uint32(record[12:16]))})
	}
	return axes
}

func fixed1616(value uint32) float64 { return math.Round((float64(int32(value))/65536)*1e6) / 1e6 }
func firstComma(value string) string {
	if index := strings.IndexByte(value, ','); index >= 0 {
		return value[:index]
	}
	return value
}
