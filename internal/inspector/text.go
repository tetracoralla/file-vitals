package inspector

import (
	"bytes"
	"encoding/binary"
	"unicode/utf16"
	"unicode/utf8"
)

func inspectTextBytes(data []byte, truncated bool) TextInfo {
	decoded, encoding, bom := decodeText(data)
	info := TextInfo{
		Encoding:    encoding,
		BOM:         bom,
		Sampled:     truncated,
		SampleBytes: len(data),
	}
	if encoding.Certainty == "unknown" {
		return info
	}
	lf, crlf, cr := countLineEndings(decoded)
	standaloneLF := lf - crlf
	standaloneCR := cr - crlf
	kinds := 0
	for _, count := range []int{crlf, standaloneLF, standaloneCR} {
		if count > 0 {
			kinds++
		}
	}
	switch {
	case kinds > 1:
		info.LineEnding = "mixed"
	case crlf > 0:
		info.LineEnding = "crlf"
	case lf > 0:
		info.LineEnding = "lf"
	case cr > 0:
		info.LineEnding = "cr"
	default:
		info.LineEnding = "none"
	}
	if !truncated {
		lines := int64(0)
		if len(decoded) > 0 {
			lines = int64(lf + cr - crlf + 1)
			if decoded[len(decoded)-1] == '\n' || decoded[len(decoded)-1] == '\r' {
				lines--
			}
		}
		final := len(decoded) > 0 && (decoded[len(decoded)-1] == '\n' || decoded[len(decoded)-1] == '\r')
		info.LineCount = &lines
		info.FinalNewline = &final
	}
	return info
}

func decodeText(data []byte) ([]byte, EncodingInfo, string) {
	switch {
	case bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}):
		return data[3:], EncodingInfo{Value: "utf-8", Certainty: "exact", Evidence: []string{"bom"}}, "utf-8"
	case bytes.HasPrefix(data, []byte{0xff, 0xfe, 0, 0}):
		return decodeUTF32(data[4:], binary.LittleEndian), EncodingInfo{Value: "utf-32le", Certainty: "exact", Evidence: []string{"bom"}}, "utf-32le"
	case bytes.HasPrefix(data, []byte{0, 0, 0xfe, 0xff}):
		return decodeUTF32(data[4:], binary.BigEndian), EncodingInfo{Value: "utf-32be", Certainty: "exact", Evidence: []string{"bom"}}, "utf-32be"
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		return decodeUTF16(data[2:], binary.LittleEndian), EncodingInfo{Value: "utf-16le", Certainty: "exact", Evidence: []string{"bom"}}, "utf-16le"
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		return decodeUTF16(data[2:], binary.BigEndian), EncodingInfo{Value: "utf-16be", Certainty: "exact", Evidence: []string{"bom"}}, "utf-16be"
	case utf8.Valid(data) && bytes.IndexByte(data, 0) < 0:
		return data, EncodingInfo{Value: "utf-8", Certainty: "probable", Evidence: []string{"valid_utf8"}}, ""
	default:
		return nil, EncodingInfo{Value: "unknown", Certainty: "unknown", Evidence: []string{}}, ""
	}
}

func decodeUTF16(data []byte, order binary.ByteOrder) []byte {
	values := make([]uint16, 0, len(data)/2)
	for len(data) >= 2 {
		values = append(values, order.Uint16(data[:2]))
		data = data[2:]
	}
	return []byte(string(utf16.Decode(values)))
}

func decodeUTF32(data []byte, order binary.ByteOrder) []byte {
	runes := make([]rune, 0, len(data)/4)
	for len(data) >= 4 {
		value := rune(order.Uint32(data[:4]))
		if utf8.ValidRune(value) {
			runes = append(runes, value)
		} else {
			runes = append(runes, utf8.RuneError)
		}
		data = data[4:]
	}
	return []byte(string(runes))
}

func countLineEndings(data []byte) (lf, crlf, cr int) {
	for index := 0; index < len(data); index++ {
		switch data[index] {
		case '\n':
			lf++
		case '\r':
			cr++
			if index+1 < len(data) && data[index+1] == '\n' {
				crlf++
			}
		}
	}
	return lf, crlf, cr
}
