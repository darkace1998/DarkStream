package formatter

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected Format
	}{
		{"json", FormatJSON},
		{"JSON", FormatJSON},
		{"csv", FormatCSV},
		{"CSV", FormatCSV},
		{"table", FormatTable},
		{"TABLE", FormatTable},
		{"", FormatTable},
		{"unknown", FormatTable},
	}

	for _, tt := range tests {
		result := ParseFormat(tt.input)
		if result != tt.expected {
			t.Errorf("ParseFormat(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestOutput_PrintJSON(t *testing.T) {
	var buf bytes.Buffer
	out := New(&buf, FormatJSON)

	data := map[string]string{"key": "value"}
	err := out.PrintJSON(data)
	if err != nil {
		t.Fatalf("PrintJSON() error = %v", err)
	}

	var result map[string]string
	err = json.Unmarshal(buf.Bytes(), &result)
	if err != nil {
		t.Fatalf("Failed to unmarshal output: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("key = %q, want \"value\"", result["key"])
	}
}

func TestOutput_PrintTable(t *testing.T) {
	var buf bytes.Buffer
	out := New(&buf, FormatTable)

	headers := []string{"Name", "Status"}
	rows := [][]string{
		{"job-1", "pending"},
		{"job-2", "completed"},
	}
	out.PrintTable(headers, rows)

	output := buf.String()
	if !strings.Contains(output, "Name") {
		t.Error("Table output missing header 'Name'")
	}
	if !strings.Contains(output, "job-1") {
		t.Error("Table output missing row 'job-1'")
	}
	if !strings.Contains(output, "completed") {
		t.Error("Table output missing row 'completed'")
	}
	// Should contain separator line
	if !strings.Contains(output, "---") {
		t.Error("Table output missing separator")
	}
}

func TestOutput_PrintTable_EmptyHeaders(t *testing.T) {
	var buf bytes.Buffer
	out := New(&buf, FormatTable)

	out.PrintTable([]string{}, nil)

	if buf.Len() != 0 {
		t.Errorf("PrintTable with empty headers produced output: %q", buf.String())
	}
}

func TestOutput_PrintCSV(t *testing.T) {
	var buf bytes.Buffer
	out := New(&buf, FormatCSV)

	headers := []string{"Name", "Status"}
	rows := [][]string{
		{"job-1", "pending"},
		{"job-2", "completed"},
	}
	err := out.PrintCSV(headers, rows)
	if err != nil {
		t.Fatalf("PrintCSV() error = %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 3 {
		t.Errorf("Expected 3 CSV lines (header + 2 rows), got %d", len(lines))
	}
	if lines[0] != "Name,Status" {
		t.Errorf("CSV header = %q, want \"Name,Status\"", lines[0])
	}
	if lines[1] != "job-1,pending" {
		t.Errorf("CSV row 1 = %q, want \"job-1,pending\"", lines[1])
	}
}

func TestOutput_Print_AllFormats(t *testing.T) {
	headers := []string{"Name", "Status"}
	rows := [][]string{{"job-1", "pending"}}
	jsonData := map[string]string{"name": "job-1", "status": "pending"}

	tests := []struct {
		name   string
		format Format
	}{
		{"table", FormatTable},
		{"json", FormatJSON},
		{"csv", FormatCSV},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			out := New(&buf, tt.format)
			err := out.Print(headers, rows, jsonData)
			if err != nil {
				t.Fatalf("Print() error = %v", err)
			}
			if buf.Len() == 0 {
				t.Error("Print() produced no output")
			}
		})
	}
}
