package inspector

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v3"
)

func structuredFormat(mediaType, extension string) string {
	switch mediaType {
	case "application/json":
		return "json"
	case "application/x-ndjson":
		return "jsonl"
	case "application/yaml":
		return "yaml"
	case "application/toml":
		return "toml"
	case "application/xml", "text/xml":
		return "xml"
	case "image/svg+xml":
		return "svg"
	case "text/csv":
		return "csv"
	case "text/tab-separated-values":
		return "tsv"
	}
	switch extension {
	case ".json":
		return "json"
	case ".jsonl", ".ndjson":
		return "jsonl"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".xml":
		return "xml"
	case ".svg":
		return "svg"
	case ".csv":
		return "csv"
	case ".tsv":
		return "tsv"
	}
	return ""
}

// errStructuredLimit marks a parse that stopped at an internal budget (line,
// record, document, token, or depth cap) rather than at invalid syntax. The
// caller reports the file as partial instead of corrupt: a valid file must not
// be shown as broken because it crossed a scan limit.
var errStructuredLimit = errors.New("structured parse limit reached")

func validateStructured(format string, data []byte) error {
	switch format {
	case "json":
		if json.Valid(data) {
			return nil
		}
		return errors.New("invalid JSON document")
	case "jsonl":
		scanner := bufio.NewScanner(bytes.NewReader(data))
		scanner.Buffer(make([]byte, 64*1024), MaxTextBytes)
		count := 0
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			if !json.Valid(line) {
				return errors.New("invalid JSON line")
			}
			count++
			if count > MaxArchiveHeaders {
				return errStructuredLimit
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		if count == 0 {
			return errors.New("no JSON values")
		}
		return nil
	case "yaml":
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		for count := 0; ; count++ {
			if count > 1000 {
				return errStructuredLimit
			}
			var value any
			err := decoder.Decode(&value)
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
		}
	case "toml":
		var value map[string]any
		_, err := toml.Decode(string(data), &value)
		return err
	case "csv", "tsv":
		reader := csv.NewReader(bytes.NewReader(data))
		if format == "tsv" {
			reader.Comma = '\t'
		}
		reader.ReuseRecord = true
		for count := 0; ; count++ {
			if count > MaxArchiveHeaders {
				return errStructuredLimit
			}
			_, err := reader.Read()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
		}
	case "xml", "svg":
		decoder := xml.NewDecoder(bytes.NewReader(data))
		depth := 0
		tokens := 0
		root := ""
		for {
			token, err := decoder.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			tokens++
			if tokens > 100_000 {
				return errStructuredLimit
			}
			switch typed := token.(type) {
			case xml.StartElement:
				depth++
				if root == "" {
					root = strings.ToLower(typed.Name.Local)
				}
				if depth > 256 {
					return errStructuredLimit
				}
			case xml.EndElement:
				depth--
			}
		}
		if format == "svg" && root != "svg" {
			return errors.New("root element is not svg")
		}
		if root == "" {
			return errors.New("empty XML document")
		}
		return nil
	default:
		return errors.New("unsupported structured format")
	}
}
