package main

import (
	"strings"
	"unicode/utf8"
)

// formatMarkdownForTerminal makes a model's answer readable on a plain
// terminal. Only pipe tables are rewritten — they are unreadable as written,
// while headings, lists and code fences already read fine — so the text a
// model produced is otherwise passed through untouched.
func formatMarkdownForTerminal(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	var out []string
	for i := 0; i < len(lines); {
		if !isTableRow(lines[i]) {
			out = append(out, lines[i])
			i++
			continue
		}
		rows, n := collectTable(lines[i:])
		out = append(out, alignTable(rows)...)
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
		i += n
	}
	return strings.Join(out, "\n")
}

func isTableRow(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "|") && strings.Contains(t, "|")
}

// isTableSeparator recognizes the "|---|:--:|" line under a table's header.
func isTableSeparator(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.Contains(t, "-") {
		return false
	}
	for _, part := range strings.Split(strings.Trim(t, "|"), "|") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Trim(part, "-:") != "" {
			return false
		}
	}
	return true
}

func splitRow(line string) []string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

// collectTable takes the rows of one table and reports how many lines it used.
func collectTable(lines []string) ([][]string, int) {
	rows := [][]string{splitRow(lines[0])}
	i := 1
	if i < len(lines) && isTableSeparator(lines[i]) {
		i++
	}
	for i < len(lines) && isTableRow(lines[i]) {
		rows = append(rows, splitRow(lines[i]))
		i++
	}
	return rows, i
}

// alignTable pads every column to its widest cell.
func alignTable(rows [][]string) []string {
	if len(rows) == 0 {
		return nil
	}
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	widths := make([]int, cols)
	for _, r := range rows {
		for c, cell := range r {
			widths[c] = max(widths[c], utf8.RuneCountInString(cell))
		}
	}

	out := make([]string, 0, len(rows)+1)
	for i, r := range rows {
		parts := make([]string, cols)
		for c := range cols {
			cell := ""
			if c < len(r) {
				cell = r[c]
			}
			parts[c] = pad(cell, widths[c])
		}
		out = append(out, "  "+strings.TrimRight(strings.Join(parts, "  "), " "))
		if i == 0 && len(rows) > 1 {
			rule := make([]string, cols)
			for c := range cols {
				rule[c] = strings.Repeat("-", widths[c])
			}
			out = append(out, "  "+strings.Join(rule, "  "))
		}
	}
	return out
}

func pad(s string, width int) string {
	if n := utf8.RuneCountInString(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}
