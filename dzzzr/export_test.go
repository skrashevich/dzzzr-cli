package dzzzr

// EncodeWindows1251ForTest exposes the charset encoder to external tests.
func EncodeWindows1251ForTest(s string) []byte { return encodeWindows1251(s) }

// DecodeWindows1251ForTest exposes the charset decoder to external tests, so
// a fake administration area can read what a browser would have posted.
func DecodeWindows1251ForTest(b []byte) string { return decodeWindows1251(b) }

// DecodeEngineJSONForTest exposes the lenient decoder, so tests can feed it
// the malformed documents the engine actually sends.
func DecodeEngineJSONForTest(body []byte, v any) error { return decodeEngineJSON(body, v) }
