package pdfsource

import (
	"strings"
	"testing"
)

func TestUnicodeRangesAndSurrogatePairs(t *testing.T) {
	for _, test := range []struct{ name, cmap, raw, want string }{
		{"range", "1 beginbfrange <0001> <0003> <0410> endbfrange", "\x00\x01\x00\x02\x00\x03", "АБВ"},
		{"four-byte range", "1 beginbfrange <FFFFFFFE> <FFFFFFFF> <0041> endbfrange", "\xff\xff\xff\xfe\xff\xff\xff\xff", "AB"},
		{"array", "1 beginbfrange <0001> <0003> [<0410> <0412> <0415>] endbfrange", "\x00\x01\x00\x02\x00\x03", "АВЕ"},
		{"surrogate", "1 beginbfchar <01> <D83DDE00> endbfchar", "\x01", "😀"},
		{"ligature", "1 beginbfchar <01> <00660069> endbfchar", "\x01", "fi"},
	} {
		t.Run(test.name, func(t *testing.T) {
			decoder, err := newFontDecoder(t.Context(), nil, []byte(test.cmap))
			if err != nil {
				t.Fatal(err)
			}
			if got := decoder.decode(test.raw); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestUnicodeRangeExpansionLimit(t *testing.T) {
	cmap := "1 beginbfrange <0000> <FFFF> <" + strings.Repeat("0041", 64) + "> endbfrange"
	if _, err := newFontDecoder(t.Context(), nil, []byte(cmap)); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("unbounded expanded Unicode map: %v", err)
	}
}
