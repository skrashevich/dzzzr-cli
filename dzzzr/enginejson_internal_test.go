package dzzzr

import (
	"encoding/json"
	"testing"
)

// The repair pass rewrites bytes of a reply before parsing it, so the one
// guarantee that matters is that it never changes a document the engine sent
// correctly. Anything it does to a malformed one is a judgement call; silently
// altering a valid one would be a bug in every reply.
func FuzzRepairPreservesValidJSON(f *testing.F) {
	for _, s := range []string{
		`{"a":1}`, `{"s":"a,,b"}`, `[1,2,3]`, `{"s":"\"},,\""}`, `{"n":null,"t":true}`,
		`{"u":"A,,","x":[{"y":"з,,"}]}`, `"\\"`, `{"a":[],"b":{}}`,
		`{"s":"\\\"},,\\"}`, `{"s":"A\/\b"}`, `{"s":"\\u0041"}`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		var before any
		if json.Unmarshal([]byte(in), &before) != nil {
			return // only valid documents carry the guarantee
		}
		out, changed := repairEngineJSON([]byte(in))
		var after any
		if err := json.Unmarshal(out, &after); err != nil {
			t.Fatalf("repair broke valid JSON %q -> %q: %v", in, out, err)
		}
		b1, _ := json.Marshal(before)
		b2, _ := json.Marshal(after)
		if string(b1) != string(b2) {
			t.Fatalf("repair changed meaning of %q: %s -> %s (changed=%v)", in, b1, b2, changed)
		}
	})
}
