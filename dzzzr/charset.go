package dzzzr

import (
	"net/url"
	"slices"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

// decodeWindows1251 converts a windows-1251 byte string to UTF-8. The admin
// area and the classic HTML game page are served in that encoding.
func decodeWindows1251(b []byte) string {
	out, err := charmap.Windows1251.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(out)
}

// encodeWindows1251 converts UTF-8 to windows-1251, replacing characters the
// charset cannot represent with '?'.
func encodeWindows1251(s string) []byte {
	out, err := charmap.Windows1251.NewEncoder().Bytes([]byte(s))
	if err != nil {
		buf := make([]byte, 0, len(s))
		for _, r := range s {
			if b, ok := charmap.Windows1251.EncodeRune(r); ok {
				buf = append(buf, b)
			} else {
				buf = append(buf, '?')
			}
		}
		return buf
	}
	return out
}

// encodeFormCP1251 renders form values the way a browser on a windows-1251
// page does: every byte of the cp1251 text, percent-encoded.
//
// The administration area is served as windows-1251 (admin/admin.php), its
// forms carry no accept-charset, and the handlers do not convert what they
// receive — admin/switchAction.php only ever converts the other way. Posting
// UTF-8 there would store mojibake in the game's own texts. The player API is
// different: go2.php converts the submitted code itself when api=true, so
// those requests stay UTF-8.
func encodeFormCP1251(v url.Values) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	for _, k := range keys {
		key := percentEncodeCP1251(k)
		for _, value := range v[k] {
			if b.Len() > 0 {
				b.WriteByte('&')
			}
			b.WriteString(key)
			b.WriteByte('=')
			b.WriteString(percentEncodeCP1251(value))
		}
	}
	return b.String()
}

// percentEncodeCP1251 is url.QueryEscape over cp1251 bytes.
func percentEncodeCP1251(s string) string {
	const upperhex = "0123456789ABCDEF"
	var b strings.Builder
	for _, c := range encodeWindows1251(s) {
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			b.WriteByte('%')
			b.WriteByte(upperhex[c>>4])
			b.WriteByte(upperhex[c&0x0F])
		}
	}
	return b.String()
}
