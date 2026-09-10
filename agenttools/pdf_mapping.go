package agenttools

import (
	"context"
	"fmt"

	"github.com/skrashevich/dzzzr-cli/pdfsource"
)

const pdfMappingInstructions = `
PDF STRUCTURAL EXTRACTION (mandatory for scenario exports/imports):
- The LLM identifies STRUCTURE ONLY. NEVER transcribe, paraphrase or generate scenario text, codes, hints, spoilers, URLs or numbers in an output document. Process pages progressively using index_pdf; addresses are 1-based page/line numbers tied to source_sha256. read_pdf is only for additional visual-placement information.
- Build a fresh mapping for EACH PDF. Layouts and headings can differ. Mapping JSON is {"version":1,"source_sha256":"<hash from index_pdf>","output":<tree>,"ignored":[<ranges>]}.
- The output tree contains objects/arrays, null for unresolved values, and $source leaves ONLY. Literal scalar values are rejected. A leaf example: {"$source":{"ranges":[{"page":15,"start_line":20,"end_line":22}],"format":"text"}}. Never put the text itself in the mapping. For a multi-page field provide multiple ranges in order. Ranges have inclusive start_line/end_line; optional start_column/end_column are inclusive 1-based Unicode character positions.
- Formats: text copies exactly including whitespace/newlines; trim removes only outer whitespace; integer parses the selected source; boolean parses yes/no/да/нет/true/false/1/0; html escapes plain text and renders source newlines as br. Use text for existing HTML. No translation, correction, invented defaults or replacement templates.
- A source selector can additionally use pattern (Go RE2) and group (0=whole match) to select a table cell or value after a label. Pattern must match exactly once; it extracts an existing substring, never generates content. Code values use trim, not integer, so leading zeros remain. Map every code row, alias, danger/sector and every division-specific count separately; don't replace alternatives with one invented value.
- Stable PDF annotation URL: {"$source":{"page":15,"link":1}} (1-based index into links). Embedded image: {"$source":{"page":15,"image_id":"<id from index>"}} produces a stable pdf-image source reference; it does not upload an image. Never rewrite URLs by hand. Keep unsupported/unresolved media references explicit and do not upload a placeholder as an engine URL.
- For engine HTML, upload selected embedded images using admin_upload_pdf_image, then put its exact returned URL into the mapping's optional image_urls:{image_id:URL}. Use format:image_html on that image selector. Combine source text and image selectors with {"$concat":[<source selector>,<image selector>,...]}; literal text in concat is forbidden. Engine validation rejects unresolved pdf-image references. For archival export references may remain unresolved without uploading anything.
- Every nonempty source line must be selected or explicitly listed in ignored (same range format). Mark labels, examples and unused template sections there, not actual scenario fields. Coverage is checked by line, not semantic meaning; inspect the mapping for complete fields. Map missing fields to null, rather than silently omitting them.
- INCREMENTAL WORKFLOW: index a few pages, identify a complete level/section, immediately save its version:1 mapping via save_local_json as part-001.json, then continue reading. Use ONE small part per response. Never read the entire long document before saving parts and never emit the full mapping in one response. Each part has the same source_sha256, output:{levels:[<one level>]} and its own ignored ranges. Intro/template-only parts may have output:{} with explicit ignored ranges. Include shared metadata in separate parts. All pages must eventually be covered; a section crossing a batch boundary requires the next pages before completing that section.
- After all parts are saved, save a SMALL manifest {"version":2,"source_sha256":"<hash>","parts":["part-001.json","part-002.json"]} and call extract_pdf(path,mapping_path:<manifest path>,output_path). Part paths are relative to the manifest directory. Go recursively merges output objects, appends arrays in listed order, combines ignored ranges, and rejects conflicting leaves. Never repeat a level in two parts: arrays append, not merge by position. Then Go checks hash, selectors and coverage of the WHOLE PDF before writing final JSON. It returns a compact report, not document text. Single version:1 mappings remain supported for short PDFs.
- Resume after interruption by listing and inspecting existing mapping parts; reuse completed parts and process only unfinished sections. Never overwrite existing parts; save corrections under new names and reference only the corrected version in the manifest. Never use assemble_local_json for PDF mapping assembly or generate final scenario text via save_local_json. Report success only after extract_pdf succeeds.
- For a local archival export preserve all fields/variants in the output tree. For an engine plan use {levels:[{params:{title,question,hint1,hint2,clue_min,clue_min2,clue_min3,codes,spoilers,code_count,...}}]} with the engine schema, and resolve division/ambiguous values with the user before import. game_id/mode need not be in the PDF: pass them explicitly to admin_validate_source/admin_upload_source together with path to the extracted file. Those tools load the file directly; never re-emit its contents through the model. Technical levels remain a separate explicit action.
`

func pdfMappingTools(g *gate, root string) []*Tool {
	return []*Tool{
		{name: "index_pdf", gate: g, noCache: true,
			description: "Индекс PDF для построения LLM схемы: SHA-256 файла, нумерованные строки, ссылки и ID картинок. Смысловой анализ определяет только адреса полей; содержимое затем копирует Go через extract_pdf. До 10 страниц за вызов. Для каждого PDF нужна своя схема.",
			parameters:  schema(map[string]any{"path": strProp("PDF внутри DZZR_FILES_ROOT"), "start_page": intProp("Первая страница с 1"), "end_page": intProp("Последняя включительно, максимум 10 страниц")}, "path"),
			run: func(ctx context.Context, a arguments) (any, error) {
				path, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				var opts pdfsource.Options
				for key, target := range map[string]*int{"start_page": &opts.StartPage, "end_page": &opts.EndPage} {
					if _, ok := a[key]; ok {
						v, err := requireAdminInt(a, key)
						if err != nil {
							return nil, err
						}
						if v < 1 {
							return nil, fmt.Errorf("%s must be positive", key)
						}
						*target = v
					}
				}
				return pdfsource.IndexFile(ctx, root, path, opts)
			}},
		{name: "extract_pdf", gate: g, noCache: true, writesLocal: true,
			description: "Алгоритмически извлечь данные PDF по сохранённой структурной схеме и создать НОВЫЙ JSON. LLM задаёт только $source ссылки на строки, без текстов. Проверяет SHA-256, диапазоны, типы и непокрытые строки. Локальная запись разрешена при readonly движка. Не перезаписывает файлы и ничего не загружает в движок.",
			parameters:  schema(map[string]any{"path": strProp("Исходный PDF"), "mapping_path": strProp("Схема version:1 либо манифест version:2 с source_sha256 и упорядоченным parts:[имена файлов схем]"), "output_path": strProp("Новый итоговый .json внутри DZZR_FILES_ROOT")}, "path", "mapping_path", "output_path"),
			run: func(ctx context.Context, a arguments) (any, error) {
				path, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				mapping, err := a.requireString("mapping_path")
				if err != nil {
					return nil, err
				}
				output, err := a.requireString("output_path")
				if err != nil {
					return nil, err
				}
				return pdfsource.ExtractFiles(ctx, root, path, mapping, output)
			}},
	}
}
