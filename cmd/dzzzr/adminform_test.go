package main

import (
	"io"
	"net/http"
	"net/url"

	"golang.org/x/text/encoding/charmap"
)

// readAdminForm reads a posted form the way the engine's administration area
// does: the pages are served in windows-1251 and the handlers store what they
// receive unchanged, so the bytes are cp1251 text.
func readAdminForm(r *http.Request) (url.Values, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	raw, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}
	decoder := charmap.Windows1251.NewDecoder()
	decode := func(s string) string {
		out, err := decoder.String(s)
		if err != nil {
			return s
		}
		return out
	}
	out := url.Values{}
	for k, vs := range raw {
		key := decode(k)
		for _, v := range vs {
			out.Add(key, decode(v))
		}
	}
	return out, nil
}
