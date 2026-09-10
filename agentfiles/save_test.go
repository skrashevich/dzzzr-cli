package agentfiles

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSaveLocalJSON(t *testing.T) {
	root := t.TempDir()
	resolved, err := resolveRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	tools := toolsFor(t, root)
	if len(tools) != 5 {
		t.Fatalf("tool count = %d, want 5", len(tools))
	}
	for _, tc := range []struct{ name, content string }{
		{"object.json", " {\n  \"название\": \"Игра 😀\", \"zero\": 0, \"flag\": false\n}\n"},
		{"array.json", "[1, {\"код\":\"ДОЗОР\"}]\n"},
	} {
		out := call(t, tools["save_local_json"], map[string]any{"path": tc.name, "content": tc.content})
		if out["saved"] != true || out["bytes"] != float64(len(tc.content)) || out["path"] != filepath.Join(resolved, tc.name) {
			t.Fatalf("result: %v", out)
		}
		got, err := os.ReadFile(filepath.Join(root, tc.name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.content {
			t.Fatalf("JSON bytes changed: %q", got)
		}
		info, err := os.Stat(filepath.Join(root, tc.name))
		if err != nil {
			t.Fatal(err)
		}
		// Windows keeps access rules instead of mode bits, so the 0600 the
		// save asks for is not what os.Stat reports back there.
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Errorf("mode=%o", info.Mode().Perm())
		}
		if _, ok := out["content"]; ok {
			t.Fatal("save result should not repeat JSON payload")
		}
	}
	abs := filepath.Join(root, "absolute.json")
	call(t, tools["save_local_json"], map[string]any{"path": abs, "content": "{}"})
}

func TestSaveLocalJSONRejectsInvalidContentBeforeCreation(t *testing.T) {
	root := t.TempDir()
	tool := toolsFor(t, root)["save_local_json"]
	for _, content := range []any{"", "{", "{} trailing", "{} {}", "null", "42", "\"text\"", "true", "{\"x\":1,\"x\":2}", "{\"x\":\"\xff\"}", "{\"x\":\"\\uD800\"}", strings.Repeat(" ", MaxJSONBytes) + "{}", false} {
		res := tool.Execute(t.Context(), map[string]any{"path": "bad.json", "content": content})
		if !res.IsError {
			t.Errorf("accepted invalid JSON of type %T", content)
		}
		if _, err := os.Lstat(filepath.Join(root, "bad.json")); !os.IsNotExist(err) {
			t.Fatalf("invalid content left file: %v", err)
		}
	}
}

func TestSaveLocalJSONNeverOverwritesOrEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	tool := toolsFor(t, root)["save_local_json"]
	existing := filepath.Join(root, "existing.json")
	mustWrite(t, existing, "old content")
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "target.json"), filepath.Join(root, "link.json")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"existing.json", "../escaped.json", "sub/../../escaped.json", filepath.Join(outside, "absolute.json"), "escape/out.json", "link.json", "missing/child.json", "wrong.txt"} {
		if res := tool.Execute(t.Context(), map[string]any{"path": path, "content": "{}"}); !res.IsError {
			t.Errorf("accepted forbidden path %q", path)
		}
	}
	got, err := os.ReadFile(existing)
	if err != nil || string(got) != "old content" {
		t.Fatalf("existing file changed: %q, %v", got, err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("escape wrote outside: %v %v", entries, err)
	}
	entries, err = os.ReadDir(root)
	if err != nil || len(entries) != 3 {
		t.Fatalf("failed save left partial artifact: %v %v", entries, err)
	}
}

func TestSaveLocalJSONInsideSymlinkParent(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(root, "inside")); err != nil {
		t.Fatal(err)
	}
	call(t, toolsFor(t, root)["save_local_json"], map[string]any{"path": "inside/result.json", "content": "[]"})
	if got, err := os.ReadFile(filepath.Join(root, "real/result.json")); err != nil || string(got) != "[]" {
		t.Fatalf("inside symlink save: %q, %v", got, err)
	}
}

func TestSaveLocalJSONCancelledBeforeCall(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result := toolsFor(t, root)["save_local_json"].Execute(ctx, map[string]any{"path": "canceled.json", "content": "{}"})
	if !result.IsError {
		t.Fatal("canceled call saved a file")
	}
	if _, err := os.Lstat(filepath.Join(root, "canceled.json")); !os.IsNotExist(err) {
		t.Fatalf("canceled call left a file: %v", err)
	}
}
