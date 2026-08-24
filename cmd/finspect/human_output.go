package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/tetracoralla/file-vitals/internal/inspector"
)

func printHuman(output io.Writer, result inspector.Result) {
	fmt.Fprintf(output, "%s · %s · %s · %s\n", result.Identity.Format, result.Identity.MediaType, result.Identity.Confidence, result.Status)
	fmt.Fprintf(output, "%s · %s\n", result.File.Name, formatBytes(result.File.SizeBytes))
	if result.Image != nil {
		fmt.Fprintf(output, "%d × %d", result.Image.Width, result.Image.Height)
		if result.Image.ColorModel != "" {
			fmt.Fprintf(output, " · %s", result.Image.ColorModel)
		}
		fmt.Fprintln(output)
	}
	if result.Text != nil {
		fmt.Fprintf(output, "%s %s", result.Text.Encoding.Value, result.Text.Encoding.Certainty)
		if result.Text.LineCount != nil {
			fmt.Fprintf(output, " · %d lines", *result.Text.LineCount)
		}
		if result.Text.LineEnding != "" {
			fmt.Fprintf(output, " · %s", result.Text.LineEnding)
		}
		fmt.Fprintln(output)
	}
	if result.Structured != nil {
		if result.Structured.Parseable == nil {
			fmt.Fprintf(output, "%s syntax · validation incomplete\n", result.Structured.Format)
		} else {
			fmt.Fprintf(output, "%s syntax · parseable=%t\n", result.Structured.Format, *result.Structured.Parseable)
		}
	}
	if result.Media != nil && result.Media.DurationMS != nil {
		fmt.Fprintf(output, "%d ms", *result.Media.DurationMS)
		if result.Media.Container != "" {
			fmt.Fprintf(output, " · %s", result.Media.Container)
		}
		fmt.Fprintln(output)
	}
	for _, stream := range result.VideoStreams {
		fmt.Fprintf(output, "Video %d: %s", stream.Index, stream.Codec)
		if stream.Width > 0 && stream.Height > 0 {
			fmt.Fprintf(output, " · %d × %d", stream.Width, stream.Height)
		}
		if stream.FPS != nil {
			fmt.Fprintf(output, " · %d/%d fps", stream.FPS.Num, stream.FPS.Den)
		}
		fmt.Fprintln(output)
	}
	for _, stream := range result.AudioStreams {
		fmt.Fprintf(output, "Audio %d: %s", stream.Index, stream.Codec)
		if stream.SampleRateHz > 0 {
			fmt.Fprintf(output, " · %d Hz", stream.SampleRateHz)
		}
		if stream.Channels > 0 {
			fmt.Fprintf(output, " · %d channels", stream.Channels)
		}
		fmt.Fprintln(output)
	}
	if result.Archive != nil {
		fmt.Fprintf(output, "%d entries scanned", result.Archive.EntriesScanned)
		if result.Archive.EntryCount != nil {
			fmt.Fprintf(output, " / %d total", *result.Archive.EntryCount)
		}
		fmt.Fprintln(output)
		for _, entry := range result.Archive.Entries {
			name := entry.Name
			if entry.Directory && !strings.HasSuffix(name, "/") {
				name += "/"
			}
			fmt.Fprintf(output, "  %s · %s", name, formatBytes(entry.SizeBytes))
			if entry.Encrypted {
				fmt.Fprint(output, " · encrypted")
			}
			fmt.Fprintln(output)
		}
		if result.Archive.EntriesTruncated {
			fmt.Fprintln(output, "  … additional entries omitted")
		}
	}
	if result.PDF != nil {
		fmt.Fprintf(output, "PDF %s", result.PDF.Version)
		if result.PDF.PageCount > 0 {
			fmt.Fprintf(output, " · %d pages", result.PDF.PageCount)
		}
		if result.PDF.Encrypted != nil {
			fmt.Fprintf(output, " · encrypted=%t", *result.PDF.Encrypted)
		}
		fmt.Fprintln(output)
	}
	if result.Font != nil {
		fmt.Fprintf(output, "%s", result.Font.Family)
		if result.Font.Subfamily != "" {
			fmt.Fprintf(output, " · %s", result.Font.Subfamily)
		}
		if result.Font.GlyphCount > 0 {
			fmt.Fprintf(output, " · %d glyphs", result.Font.GlyphCount)
		}
		fmt.Fprintln(output)
	}
	if result.Binary != nil {
		fmt.Fprintf(output, "%s", result.Binary.Format)
		if len(result.Binary.Architectures) > 0 {
			fmt.Fprintf(output, " · %s", strings.Join(result.Binary.Architectures, ", "))
		}
		if result.Binary.Bits > 0 {
			fmt.Fprintf(output, " · %d-bit", result.Binary.Bits)
		}
		fmt.Fprintln(output)
	}
	if result.Integrity.SHA256 != "" {
		fmt.Fprintf(output, "SHA-256: %s\n", result.Integrity.SHA256)
	}
	if result.Integrity.SHA256Matches != nil {
		fmt.Fprintf(output, "SHA-256 matches expected: %t\n", *result.Integrity.SHA256Matches)
	}
	if len(result.Traits) > 0 {
		fmt.Fprintf(output, "Traits: %s\n", strings.Join(result.Traits, ", "))
	}
	if len(result.Constraints) > 0 {
		fmt.Fprintf(output, "Action blockers: %s\n", strings.Join(result.Constraints, ", "))
	}
	for _, diagnostic := range result.Diagnostics {
		fmt.Fprintf(output, "%s %s: %s\n", strings.ToUpper(diagnostic.Severity), diagnostic.Code, diagnostic.Message)
	}
	if result.Error != nil {
		fmt.Fprintf(output, "%s: %s\n", result.Error.Code, result.Error.Message)
	}
}

func printHumanBatch(output io.Writer, result inspector.BatchResult) {
	fmt.Fprintf(output, "%s · %d files\n", result.Status, len(result.Items))
	for _, item := range result.Items {
		if item.Result.Error != nil {
			fmt.Fprintf(output, "[%d] %s · %s · %s\n", item.Index, item.Path, item.Result.Status, item.Result.Error.Code)
			continue
		}
		fmt.Fprintf(output, "[%d] %s · %s · %s · %s\n", item.Index, item.Path, item.Result.Status, item.Result.Identity.Format, formatBytes(item.Result.File.SizeBytes))
	}
	if result.Error != nil {
		fmt.Fprintf(output, "%s: %s\n", result.Error.Code, result.Error.Message)
	}
}

func printHumanInventory(output io.Writer, result inspector.InventoryResult) {
	fmt.Fprintf(output, "%s · %d files · %d directories · %s\n", result.Status, result.FilesScanned, result.DirectoriesScanned, formatBytes(result.TotalSizeBytes))
	for _, format := range result.Formats {
		fmt.Fprintf(output, "%s · %d files · %s\n", format.Format, format.FileCount, formatBytes(format.TotalSizeBytes))
	}
	for _, item := range result.Items {
		fmt.Fprintf(output, "%s · %s · %s", item.Path, item.Status, item.Identity.Format)
		if len(item.Constraints) > 0 {
			fmt.Fprintf(output, " · blockers=%s", strings.Join(item.Constraints, ","))
		}
		fmt.Fprintln(output)
	}
	if result.Limits.Truncated {
		fmt.Fprintln(output, "Inventory truncated at the declared limits.")
	}
	if result.Error != nil {
		fmt.Fprintf(output, "%s: %s\n", result.Error.Code, result.Error.Message)
	}
}
