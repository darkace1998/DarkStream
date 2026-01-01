// Package formatter provides output formatting utilities for the CLI.
package formatter

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Format represents an output format type
type Format string

const (
	// FormatTable is the default table format
	FormatTable Format = "table"
	// FormatJSON outputs data as JSON
	FormatJSON Format = "json"
	// FormatCSV outputs data as CSV
	FormatCSV Format = "csv"
)

// ParseFormat parses a format string into a Format type
func ParseFormat(s string) Format {
	switch strings.ToLower(s) {
	case "json":
		return FormatJSON
	case "csv":
		return FormatCSV
	case "table", "":
		return FormatTable
	default:
		return FormatTable
	}
}

// Output handles formatted output for the CLI
type Output struct {
	format Format
	writer io.Writer
}

// New creates a new Output formatter
func New(w io.Writer, format Format) *Output {
	return &Output{
		format: format,
		writer: w,
	}
}

// PrintJSON outputs data as formatted JSON
func (o *Output) PrintJSON(data any) error {
	encoder := json.NewEncoder(o.writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}
	return nil
}

// PrintTable outputs data as a formatted table
func (o *Output) PrintTable(headers []string, rows [][]string) {
	if len(headers) == 0 {
		return
	}

	// Calculate column widths
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Print header
	printRow(o.writer, headers, widths)
	// Print separator
	printSeparator(o.writer, widths)
	// Print rows
	for _, row := range rows {
		printRow(o.writer, row, widths)
	}
}

// PrintCSV outputs data as CSV
func (o *Output) PrintCSV(headers []string, rows [][]string) error {
	w := csv.NewWriter(o.writer)
	if err := w.Write(headers); err != nil {
		return fmt.Errorf("failed to write CSV headers: %w", err)
	}
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return fmt.Errorf("failed to write CSV row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("failed to flush CSV: %w", err)
	}
	return nil
}

// Print outputs data in the configured format
func (o *Output) Print(headers []string, rows [][]string, jsonData any) error {
	switch o.format {
	case FormatJSON:
		return o.PrintJSON(jsonData)
	case FormatCSV:
		return o.PrintCSV(headers, rows)
	default:
		o.PrintTable(headers, rows)
		return nil
	}
}

func printRow(w io.Writer, cells []string, widths []int) {
	for i, cell := range cells {
		if i > 0 {
			_, _ = fmt.Fprintf(w, " | ")
		}
		width := 10
		if i < len(widths) {
			width = widths[i]
		}
		_, _ = fmt.Fprintf(w, "%-*s", width, cell)
	}
	_, _ = fmt.Fprintln(w)
}

func printSeparator(w io.Writer, widths []int) {
	for i, width := range widths {
		if i > 0 {
			_, _ = fmt.Fprintf(w, "-+-")
		}
		_, _ = fmt.Fprintf(w, "%s", strings.Repeat("-", width))
	}
	_, _ = fmt.Fprintln(w)
}
