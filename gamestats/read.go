// Log normalization and date parsing ported from dozor_stats.html.
// Original algorithm author: Sergey <sergey@luberg.me> Luberg.
package gamestats

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"encoding/xml"
	"fmt"
	"io"
	"maps"
	"math"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// Dates are wall-clock times from the engine. UTC keeps calculations and the
// wire representation independent of the server's and browser's time zones.
func ParseTime(s string) *int64 {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	for _, layout := range []string{"2006-1-2 15:04:05.999999999", "2006-1-2T15:04:05.999999999", "2.1.2006 15:04:05.999999999", "2006-1-2 15:04", "2006-1-2T15:04", "2.1.2006 15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return new(t.UnixMilli())
		}
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil && n >= 20000 && n <= 80000 {
		return new(time.UnixMilli(int64(math.Round((n - 25569) * 86400000))).Truncate(time.Second).UnixMilli())
	}
	return nil
}

func norm(s string) string     { return strings.Join(strings.Fields(s), " ") }
func normCode(s string) string { return strings.ToUpper(strings.Join(strings.Fields(s), "")) }

var integerLevel = regexp.MustCompile(`^\d+$`)

// Read supports XLSX, JSON row exports and UTF-8/Windows-1251 CSV/TSV files.
func Read(name string, data []byte) ([]Event, error) {
	var table [][]string
	var err error
	switch strings.ToLower(path.Ext(name)) {
	case ".xlsx", ".xlsm":
		table, err = readXLSX(data)
	case ".json":
		table, err = readJSON(data)
	case ".csv", ".tsv", ".txt":
		if !utf8.Valid(data) {
			data, err = charmap.Windows1251.NewDecoder().Bytes(data)
		}
		if err == nil {
			text := strings.TrimPrefix(string(data), "\ufeff")
			head, _, _ := strings.Cut(text, "\n")
			delim := ';'
			if strings.Contains(head, "\t") {
				delim = '\t'
			} else if strings.Count(head, ",") > strings.Count(head, ";") {
				delim = ','
			}
			r := csv.NewReader(strings.NewReader(text))
			r.Comma = delim
			r.FieldsPerRecord = -1
			table, err = r.ReadAll()
		}
	default:
		return nil, fmt.Errorf("поддерживаются XLSX, XLSM, JSON, CSV и TSV; сохраните журнал в одном из этих форматов")
	}
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать журнал: %w", err)
	}
	return rowsFromTable(table)
}

// JSON exports contain table rows keyed by their spreadsheet row number.
// Sort these keys numerically so simultaneous events retain their log order.
// An array of rows is accepted as the equivalent unnumbered representation.
func readJSON(data []byte) ([][]string, error) {
	data = bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	var rows [][]jsontext.Value
	if len(data) > 0 && data[0] == '{' {
		var numbered map[string][]jsontext.Value
		if err := json.Unmarshal(data, &numbered); err != nil {
			return nil, err
		}
		byNumber := make(map[uint64][]jsontext.Value, len(numbered))
		for key, row := range numbered {
			n, err := strconv.ParseUint(key, 10, 64)
			if err != nil || strconv.FormatUint(n, 10) != key {
				return nil, fmt.Errorf("JSON: ключ %q должен быть номером строки", key)
			}
			byNumber[n] = row
		}
		for _, n := range slices.Sorted(maps.Keys(byNumber)) {
			rows = append(rows, byNumber[n])
		}
	} else {
		if err := json.Unmarshal(data, &rows); err != nil {
			return nil, fmt.Errorf("JSON: нужен объект с номерами строк или массив строк: %w", err)
		}
	}
	table := make([][]string, len(rows))
	for i, row := range rows {
		table[i] = make([]string, len(row))
		for j, raw := range row {
			s := strings.TrimSpace(string(raw))
			if s == "null" {
				continue
			}
			if strings.HasPrefix(s, `"`) {
				if err := json.Unmarshal(raw, &table[i][j]); err != nil {
					return nil, err
				}
			} else {
				n, err := strconv.ParseFloat(s, 64)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
					return nil, fmt.Errorf("JSON: строка %d, столбец %d — ожидается строка, число или null", i+1, j+1)
				}
				table[i][j] = strconv.FormatFloat(n, 'f', -1, 64)
			}
		}
	}
	return table, nil
}

func rowsFromTable(table [][]string) ([]Event, error) {
	hi := -1
	indexes := []int{0, 1, 2, 3, 4, 5}
	for i, row := range table[:min(30, len(table))] {
		ti, ai, tm, lv, player := -1, -1, -1, -1, -1
		var ds []int
		for j, value := range row {
			s := strings.ToLower(strings.TrimSpace(value))
			switch {
			case strings.HasPrefix(s, "время"):
				ti = j
			case strings.HasPrefix(s, "действ"):
				ai = j
			case strings.HasPrefix(s, "команд"):
				tm = j
			case strings.HasPrefix(s, "уровень"), strings.HasPrefix(s, "задани"):
				lv = j
			case strings.HasPrefix(s, "данн"):
				ds = append(ds, j)
			case strings.Contains(s, "игрок"), strings.Contains(s, "участник"), strings.Contains(s, "пользовател"), strings.Contains(s, "логин"), strings.Contains(s, "ник"):
				player = j
			}
		}
		if ti < 0 || ai < 0 {
			continue
		}
		hi = i
		indexes[0] = ti
		indexes[1] = ai
		if tm >= 0 {
			indexes[2] = tm
		}
		if lv >= 0 {
			indexes[3] = lv
		}
		if len(ds) > 0 {
			indexes[4] = ds[0]
		}
		if len(ds) > 1 {
			indexes[5] = ds[1]
		}
		if player >= 0 {
			indexes[5] = player
		}
		break
	}
	var events []Event
	for _, row := range table[hi+1:] {
		get := func(i int) string {
			if indexes[i] < len(row) {
				return row[indexes[i]]
			}
			return ""
		}
		t := ParseTime(get(0))
		if t == nil || get(1) == "" {
			continue
		}
		level := norm(get(3))
		if integerLevel.MatchString(level) {
			level += ".0"
		}
		events = append(events, Event{T: *t, A: get(1), Team: norm(get(2)), Level: level, Data: get(4), Player: get(5)})
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("нет строк журнала: нужны столбцы «Время» и «Действие»")
	}
	return events, nil
}

type richText struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (r richText) value() string {
	s := r.Text
	for _, run := range r.Runs {
		s += run.Text
	}
	return s
}

func readXLSX(data []byte) ([][]string, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := map[string]*zip.File{}
	var total uint64
	for _, f := range z.File {
		total += f.UncompressedSize64
		if total > 128<<20 {
			return nil, fmt.Errorf("распакованный XLSX превышает 128 МБ")
		}
		files[f.Name] = f
	}
	decode := func(name string, out any) error {
		f := files[name]
		if f == nil {
			return fmt.Errorf("XLSX: отсутствует %s", name)
		}
		r, e := f.Open()
		if e != nil {
			return e
		}
		defer func() { _ = r.Close() }()
		return xml.NewDecoder(io.LimitReader(r, 128<<20)).Decode(out)
	}
	var book struct {
		Props struct {
			Date1904 bool `xml:"date1904,attr"`
		} `xml:"workbookPr"`
		Sheets []struct {
			ID string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := decode("xl/workbook.xml", &book); err != nil {
		return nil, err
	}
	if len(book.Sheets) == 0 {
		return nil, fmt.Errorf("XLSX не содержит листов")
	}
	if book.Props.Date1904 {
		return nil, fmt.Errorf("XLSX с календарём 1904: сохраните журнал как CSV")
	}
	var rels struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := decode("xl/_rels/workbook.xml.rels", &rels); err != nil {
		return nil, err
	}
	sheet := ""
	for _, rel := range rels.Items {
		if rel.ID == book.Sheets[0].ID {
			sheet = path.Clean(path.Join("xl", rel.Target))
			if strings.HasPrefix(rel.Target, "/") {
				sheet = strings.TrimPrefix(rel.Target, "/")
			}
			break
		}
	}
	var shared struct {
		Items []richText `xml:"si"`
	}
	if files["xl/sharedStrings.xml"] != nil {
		if err := decode("xl/sharedStrings.xml", &shared); err != nil {
			return nil, err
		}
	}
	var ws struct {
		Rows []struct {
			Cells []struct {
				Ref    string   `xml:"r,attr"`
				Type   string   `xml:"t,attr"`
				Value  string   `xml:"v"`
				Inline richText `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := decode(sheet, &ws); err != nil {
		return nil, err
	}
	rows := make([][]string, 0, len(ws.Rows))
	for _, row := range ws.Rows {
		var values []string
		for _, c := range row.Cells {
			col := 0
			for _, ch := range c.Ref {
				if ch < 'A' || ch > 'Z' {
					break
				}
				col = col*26 + int(ch-'A'+1)
			}
			if col == 0 {
				col = len(values) + 1
			}
			if col > 16384 {
				return nil, fmt.Errorf("XLSX: неверный столбец")
			}
			for len(values) < col {
				values = append(values, "")
			}
			v := c.Value
			switch c.Type {
			case "s":
				i, e := strconv.Atoi(v)
				if e != nil || i < 0 || i >= len(shared.Items) {
					return nil, fmt.Errorf("XLSX: неверный индекс строки")
				}
				v = shared.Items[i].value()
			case "inlineStr":
				v = c.Inline.value()
			}
			values[col-1] = v
		}
		rows = append(rows, values)
	}
	return rows, nil
}
