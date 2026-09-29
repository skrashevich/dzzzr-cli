// Statistics formatting and export layout adapted from dozor_stats.html.
// Original algorithm author: Sergey <sergey@luberg.me> Luberg.
package gamestats

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

func FormatDuration(sec float64, short bool) string {
	neg := ""
	if sec < 0 {
		neg = "-"
	}
	n := int64(math.Round(math.Abs(sec)))
	h, m, s := n/3600, n%3600/60, n%60
	if short {
		if s != 0 {
			return fmt.Sprintf("%s%d:%02d:%02d", neg, h, m, s)
		}
		return fmt.Sprintf("%s%d:%02d", neg, h, m)
	}
	return fmt.Sprintf("%s%02d:%02d:%02d", neg, h, m, s)
}
func dateString(t *int64) string {
	if t == nil {
		return ""
	}
	return time.UnixMilli(*t).UTC().Format("02.01 15:04:05")
}
func hms(t *float64) string {
	if t == nil {
		return ""
	}
	return FormatDuration(*t, false)
}

// ExportXLSX emits the reference's two sheets. Text cells are always inline
// strings, so team names and codes cannot become spreadsheet formulas.
func ExportXLSX(report *Report) ([]byte, error) {
	r := report.Result
	head := []string{"Команда"}
	var levels []Level
	for _, l := range r.R.Levels {
		if l.Kind == "hidden" {
			continue
		}
		levels = append(levels, l)
		name := l.Name
		if l.Kind == "bonus" {
			name += " (бонусное)"
		}
		if l.Kind == "through" {
			name += " (бонусное сквозное)"
		}
		head = append(head, name)
	}
	head = append(head, "Штраф", "Бонусы", "Чистое время", "Общее время", "Место", "Место по чистому", "Отставание от лидера", "Отставание от предыдущего")
	board := [][]string{head}
	details := [][]string{{"Команда", "Уровень", "Тип", "Выдан", "Закрыт", "Время", "В зачёт, сек", "Добавочное", "Статус", "Как закрыт", "Спойлер", "Коды спойлера", "Кодов", "Бонусных кодов", "Бонус"}}
	states := map[string]string{"h0": "без подсказок", "h1": "после первой подсказки", "h2": "после второй подсказки", "fail": "не выполнено", "none": "не получено", "stop": "не выполнено до стоп-игры"}
	methods := map[string]string{"code": "выполнен — введены все коды", "credit": "засчитан организатором", "avg": "засчитано среднее время", "timeout": "не выполнен — вышел лимит времени", "refuse": "не выполнен — отказ от уровня", "stop": "не выполнено до стоп-игры", "none": "не получен"}
	for _, row := range r.Rows {
		line := []string{row.Team}
		for _, l := range levels {
			c := row.Cells[l.Name]
			value := ""
			if c.Show {
				if l.Kind == "through" {
					if c.BonusSec > 0 {
						value = FormatDuration(c.BonusSec, true)
					} else if c.NC > 0 || c.NB > 0 {
						value = fmt.Sprintf("кодов %d", c.NC)
						if c.NB > 0 {
							value += fmt.Sprintf(", бонусных %d", c.NB)
						}
					}
				} else {
					value = hms(c.Sec)
				}
			}
			line = append(line, value)
			d := make([]string, 15)
			d[0] = row.Team
			d[1] = l.Name
			d[2] = map[string]string{"main": "зачётный", "bonus": "заглушка", "through": "сквозной"}[l.Kind]
			d[3] = dateString(c.Issued)
			d[4] = dateString(c.End)
			if l.Kind != "through" {
				if c.Show {
					d[5] = hms(c.Sec)
				}
				d[8] = states[c.St]
				d[9] = methods[c.How]
			}
			if l.Kind == "main" {
				d[6] = strconv.FormatFloat(math.Round(c.CountSec), 'f', 0, 64)
			}
			if c.AddSec != 0 {
				d[7] = FormatDuration(c.AddSec, true)
			}
			d[12] = "0"
			d[13] = "0"
			if rec := c.Rec; rec != nil {
				if c.Issued == nil {
					d[3] = dateString(rec.Issued)
				}
				d[10] = dateString(rec.SpoilerAt)
				codes := []*Code{}
				for _, code := range rec.Spoilers {
					codes = append(codes, code)
				}
				slices.SortFunc(codes, func(a, b *Code) int {
					if a.T < b.T {
						return -1
					}
					if a.T > b.T {
						return 1
					}
					return strings.Compare(a.Raw, b.Raw)
				})
				names := []string{}
				for _, code := range codes {
					names = append(names, code.Raw)
				}
				d[11] = strings.Join(names, ", ")
				d[12] = strconv.Itoa(len(rec.Codes))
				d[13] = strconv.Itoa(len(rec.Bonus))
			}
			if c.BonusSec != 0 {
				d[14] = FormatDuration(c.BonusSec, true)
			}
			details = append(details, d)
		}
		pen, bon := FormatDuration(row.Add, true), FormatDuration(row.Thr, true)
		if row.Penalty != 0 {
			pen += "+" + FormatDuration(row.Penalty, true)
		}
		if row.BonusOther != 0 {
			bon += "+" + FormatDuration(row.BonusOther, true)
		}
		leader, prev := "-", "-"
		if row.GapLeader != nil {
			leader = hms(row.GapLeader)
		}
		if row.GapPrev != nil {
			prev = hms(row.GapPrev)
		}
		line = append(line, pen, bon, FormatDuration(row.Clean, false), FormatDuration(row.Total, false), strconv.Itoa(row.Place), strconv.Itoa(row.PlaceClean), leader, prev)
		board = append(board, line)
	}
	return writeXLSX([]string{"Статистика", "Детали"}, [][][]string{board, details})
}

func writeXLSX(names []string, tables [][][]string) ([]byte, error) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	put := func(name, body string) error {
		w, err := z.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(body))
		return err
	}
	esc := func(s string) string { var b bytes.Buffer; _ = xml.EscapeText(&b, []byte(s)); return b.String() }
	content := `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`
	book := `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`
	rels := `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	for i, name := range names {
		n := strconv.Itoa(i + 1)
		content += `<Override PartName="/xl/worksheets/sheet` + n + `.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`
		book += `<sheet name="` + esc(name) + `" sheetId="` + n + `" r:id="rId` + n + `"/>`
		rels += `<Relationship Id="rId` + n + `" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet` + n + `.xml"/>`
		var sheet strings.Builder
		sheet.WriteString(`<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><cols><col min="1" max="1" width="20" customWidth="1"/><col min="2" max="16384" width="16" customWidth="1"/></cols><sheetData>`)
		for ri, row := range tables[i] {
			_, _ = fmt.Fprintf(&sheet, `<row r="%d">`, ri+1)
			for ci, value := range row {
				col := ""
				for x := ci + 1; x > 0; x = (x - 1) / 26 {
					col = string(rune('A'+(x-1)%26)) + col
				}
				numeric := ri > 0 && value != "" && ((i == 0 && (ci == len(row)-4 || ci == len(row)-3)) || (i == 1 && (ci == 6 || ci == 12 || ci == 13)))
				if numeric {
					_, _ = fmt.Fprintf(&sheet, `<c r="%s%d"><v>%s</v></c>`, col, ri+1, esc(value))
				} else {
					_, _ = fmt.Fprintf(&sheet, `<c r="%s%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, col, ri+1, esc(value))
				}
			}
			sheet.WriteString(`</row>`)
		}
		sheet.WriteString(`</sheetData></worksheet>`)
		if err := put("xl/worksheets/sheet"+n+".xml", sheet.String()); err != nil {
			return nil, err
		}
	}
	for _, f := range [][2]string{{"[Content_Types].xml", content + `</Types>`}, {"xl/workbook.xml", book + `</sheets></workbook>`}, {"xl/_rels/workbook.xml.rels", rels + `</Relationships>`}, {"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`}} {
		if err := put(f[0], f[1]); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
