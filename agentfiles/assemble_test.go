package agentfiles

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSaveLocalJSONStructuredData(t *testing.T) {
	root := t.TempDir()
	tool := toolsFor(t, root)["save_local_json"]
	for i, data := range []any{map[string]any{"text": "слово \"кино\" и Unicode 😀", "nested": map[string]any{"zero": 0, "enabled": false}}, []any{"a", "b"}} {
		name := fmt.Sprintf("data%d.json", i)
		call(t, tool, map[string]any{"path": name, "data": data})
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		var gotValue, wantValue any
		if err := json.Unmarshal(got, &gotValue); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(want, &wantValue); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(gotValue, wantValue) {
			t.Fatalf("data changed: %s", got)
		}
	}
	for _, args := range []map[string]any{{}, {"data": nil}, {"data": "not JSON"}, {"data": `{"duplicate":1,"duplicate":2}`}, {"data": `"scalar"`}, {"data": false}, {"data": map[string]any{}, "content": "{}"}, {"data": map[string]any{"large": strings.Repeat("x", MaxJSONBytes)}}} {
		args["path"] = "invalid.json"
		if res := tool.Execute(t.Context(), args); !res.IsError {
			t.Errorf("accepted invalid data/content combination")
		}
		if _, err := os.Stat(filepath.Join(root, "invalid.json")); !os.IsNotExist(err) {
			t.Fatalf("invalid call left file: %v", err)
		}
	}
	r := tool.Execute(t.Context(), map[string]any{"path": "bad.json", "content": `{"spoiler":"слово "кино""}`})
	if !r.IsError || !strings.Contains(r.Content, "byte_offset=") || !strings.Contains(r.Content, "JSON_pointer=") {
		t.Fatalf("missing precise parse error: %+v", r)
	}
}

func TestSaveLocalJSONStringifiedDataFromModel(t *testing.T) {
	root := t.TempDir()
	tool := toolsFor(t, root)["save_local_json"]
	content := `{"key":"level","text":"слово \"кино\"","codes":["001"]}`
	call(t, tool, map[string]any{"path": "part.json", "data": content})
	got, err := os.ReadFile(filepath.Join(root, "part.json"))
	if err != nil || string(got) != content {
		t.Fatalf("stringified object changed: %s, %v", got, err)
	}
}

func assemblyFixture(t *testing.T) (string, *Tool) {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "manifest.json"), `{"source":"scenario.pdf","metadata":{"name":"Игра","number":9007199254740993},"levels":[{"key":"one","title":"First"},{"key":"two","title":"Second"}]}`)
	mustWrite(t, filepath.Join(root, "one.json"), `{"key":"one","title":"Первый","codes":["A"],"precise":9007199254740993}`)
	mustWrite(t, filepath.Join(root, "two.json"), `{"key":"two","title":"Второй","text":"слово \"кино\""}`)
	return root, toolsFor(t, root)["assemble_local_json"]
}
func assemblyArgs() map[string]any {
	return map[string]any{"path": "output.json", "manifest": "manifest.json", "parts": []any{"two.json", "one.json"}}
}

func TestAssembleLocalJSONCompleteOrderedAndImmutable(t *testing.T) {
	root, tool := assemblyFixture(t)
	result := call(t, tool, assemblyArgs())
	if result["level_count"] != float64(2) || result["saved"] != true {
		t.Fatalf("result: %v", result)
	}
	data, err := os.ReadFile(filepath.Join(root, "output.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Source   string         `json:"source"`
		Metadata map[string]any `json:"metadata"`
		Levels   []struct {
			Key   string `json:"key"`
			Title string `json:"title"`
		} `json:"levels"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Source != "scenario.pdf" || out.Metadata["name"] != "Игра" || len(out.Levels) != 2 || out.Levels[0].Key != "one" || out.Levels[1].Key != "two" || out.Levels[1].Title != "Второй" {
		t.Fatalf("bad assembly: %+v", out)
	}
	if strings.Count(string(data), "9007199254740993") != 2 {
		t.Fatalf("numeric precision lost: %s", data)
	}
	if res := tool.Execute(t.Context(), assemblyArgs()); !res.IsError {
		t.Fatal("overwrote existing output")
	}
	after, err := os.ReadFile(filepath.Join(root, "output.json"))
	if err != nil || string(after) != string(data) {
		t.Fatal("existing output changed")
	}
}

func TestAssembleLocalJSONRefusesIncompleteInventory(t *testing.T) {
	root, tool := assemblyFixture(t)
	items := []string{}
	for i := range 16 {
		key := fmt.Sprintf("level-%02d", i)
		items = append(items, fmt.Sprintf(`{"key":%q}`, key))
		if i < 2 {
			mustWrite(t, filepath.Join(root, fmt.Sprintf("part%d.json", i)), fmt.Sprintf(`{"key":%q,"title":"Ready"}`, key))
		}
	}
	mustWrite(t, filepath.Join(root, "manifest.json"), `{"levels":[`+strings.Join(items, ",")+`]}`)
	result := tool.Execute(t.Context(), map[string]any{"path": "output.json", "manifest": "manifest.json", "parts": []any{"part0.json", "part1.json"}})
	if !result.IsError || !strings.Contains(result.Content, "14 из 16") {
		t.Fatalf("partial scenario accepted: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "output.json")); !os.IsNotExist(err) {
		t.Fatalf("partial output exists: %v", err)
	}
}

func TestAssembleLocalJSONRejectsMalformedInputs(t *testing.T) {
	for _, tc := range []struct {
		name, file, content string
		parts               any
	}{
		{"bad manifest", "manifest.json", "{", nil},
		{"array manifest", "manifest.json", "[]", nil},
		{"empty inventory", "manifest.json", `{"levels":[]}`, nil},
		{"missing inventory", "manifest.json", `{}`, nil},
		{"wrong metadata", "manifest.json", `{"levels":[{"key":"one"}],"metadata":[]}`, nil},
		{"missing key", "manifest.json", `{"levels":[{"title":"One"}]}`, nil},
		{"blank key", "manifest.json", `{"levels":[{"key":" "}]}`, nil},
		{"duplicate manifest key", "manifest.json", `{"levels":[{"key":"one"},{"key":"one"}]}`, nil},
		{"nonobject part", "one.json", `[]`, nil},
		{"null part", "one.json", `null`, nil},
		{"bad part", "one.json", `{"key":`, nil},
		{"missing part key", "one.json", `{"title":"One"}`, nil},
		{"unknown part key", "one.json", `{"key":"three"}`, nil},
		{"duplicate object key", "one.json", `{"key":"one","key":"two"}`, nil},
		{name: "duplicate part", parts: []any{"one.json", "one.json"}},
		{name: "empty parts", parts: []any{}},
		{name: "nonstring part", parts: []any{42}},
		{name: "too many parts", parts: make([]string, maxJSONParts+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, tool := assemblyFixture(t)
			if tc.file != "" {
				mustWrite(t, filepath.Join(root, tc.file), tc.content)
			}
			args := assemblyArgs()
			if tc.parts != nil {
				args["parts"] = tc.parts
			}
			if res := tool.Execute(t.Context(), args); !res.IsError {
				t.Fatalf("accepted malformed input: %+v", res)
			}
			if _, err := os.Stat(filepath.Join(root, "output.json")); !os.IsNotExist(err) {
				t.Fatalf("invalid inputs left output: %v", err)
			}
		})
	}
}

func TestAssembleLocalJSONConfinesEveryPath(t *testing.T) {
	root, tool := assemblyFixture(t)
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "one.json"), `{"key":"one"}`)
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		key   string
		value any
	}{
		{"manifest", "../manifest.json"}, {"manifest", "escape/one.json"},
		{"parts", []any{"escape/one.json", "two.json"}},
		{"parts", []any{filepath.Join(outside, "one.json"), "two.json"}},
		{"parts", []any{"../one.json", "two.json"}},
		{"path", "escape/output.json"}, {"path", filepath.Join(outside, "output.json")},
	} {
		args := assemblyArgs()
		args[tc.key] = tc.value
		if res := tool.Execute(t.Context(), args); !res.IsError {
			t.Errorf("accepted escape %v", tc)
		}
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "one.json" {
		t.Fatalf("wrote outside root: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(root, "output.json")); !os.IsNotExist(err) {
		t.Fatalf("failed escape left output: %v", err)
	}
}

func TestAssembleLocalJSONBoundsTotalInput(t *testing.T) {
	root, tool := assemblyFixture(t)
	for _, key := range []string{"one", "two"} {
		mustWrite(t, filepath.Join(root, key+".json"), fmt.Sprintf(`{"key":%q,"text":%q}`, key, strings.Repeat("x", MaxJSONBytes/2)))
	}
	if res := tool.Execute(t.Context(), assemblyArgs()); !res.IsError || !strings.Contains(res.Content, "лимит") {
		t.Fatalf("accepted oversized input: %+v", res)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !reflect.DeepEqual(names, []string{"manifest.json", "one.json", "two.json"}) {
		t.Fatalf("partial output left: %v", names)
	}
}
