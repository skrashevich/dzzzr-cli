package gamesource

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"maps"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"golang.org/x/net/html"
)

const maxScenarioAssetBytes = 64 << 20

// ScenarioFiles confines file access to Root. HTTP fetches use a separate
// client without organizer cookies or credentials.
type ScenarioFiles struct {
	Root       string
	HTTPClient *http.Client
}

func validateAsset(a ScenarioAsset) error {
	if strings.TrimSpace(a.Filename) == "" || strings.ContainsFunc(a.Filename, unicode.IsControl) {
		return fmt.Errorf("invalid filename")
	}
	if a.Path != "" {
		if !filepath.IsLocal(a.Path) || strings.ContainsAny(a.Path, "\\:") {
			return fmt.Errorf("asset path must be relative")
		}
		for part := range strings.SplitSeq(a.Path, "/") {
			if part == "." || part == ".." || part == "" {
				return fmt.Errorf("asset path contains a dot/empty segment")
			}
		}
	}
	for _, raw := range []string{a.URL, a.OriginalURL} {
		if raw != "" {
			if _, err := httpURL(raw); err != nil {
				return err
			}
		}
	}
	if a.SizeBytes != nil && *a.SizeBytes > dzzzr.MaxAdminFileBytes {
		return fmt.Errorf("asset is larger than 32 MiB")
	}
	return nil
}

func ReadScenarioFile(root, name string, limit int64) ([]byte, error) {
	if !filepath.IsLocal(name) {
		return nil, fmt.Errorf("file must stay inside the scenario root")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	info, err := r.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("expected a regular file up to %d bytes", limit)
	}
	f, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("expected a regular file")
	}
	return readBounded(f, limit)
}

// WriteScenarioFile creates a new private file without overwriting an export.
func WriteScenarioFile(root, name string, data []byte) error {
	if !filepath.IsLocal(name) {
		return fmt.Errorf("file must stay inside the scenario root")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	f, err := r.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = r.Remove(name)
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}
	return nil
}

func readBounded(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	return b, nil
}

func (f ScenarioFiles) fetch(ctx context.Context, raw string) ([]byte, string, error) {
	if _, err := httpURL(raw); err != nil {
		return nil, "", err
	}
	client := &http.Client{Timeout: 45 * time.Second}
	if f.HTTPClient != nil {
		copy := *f.HTTPClient
		client = &copy
	}
	client.Jar = nil
	previous := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if _, err := httpURL(req.URL.String()); err != nil {
			return err
		}
		if len(via) >= 5 {
			return fmt.Errorf("too many asset redirects")
		}
		if previous != nil {
			return previous(req, via)
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("asset download HTTP %d", resp.StatusCode)
	}
	b, err := readBounded(resp.Body, dzzzr.MaxAdminFileBytes)
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if media == "" {
		media = http.DetectContentType(b)
	}
	return b, media, err
}

func (f ScenarioFiles) load(ctx context.Context, a ScenarioAsset) ([]byte, error) {
	var data []byte
	var err error
	switch {
	case a.DataBase64 != nil:
		if base64.StdEncoding.DecodedLen(len(*a.DataBase64)) > dzzzr.MaxAdminFileBytes+2 {
			return nil, fmt.Errorf("embedded asset too large")
		}
		data, err = base64.StdEncoding.Strict().DecodeString(*a.DataBase64)
	case a.Path != "":
		data, err = ReadScenarioFile(f.Root, filepath.FromSlash(a.Path), dzzzr.MaxAdminFileBytes)
	case a.URL != "":
		var media string
		data, media, err = f.fetch(ctx, a.URL)
		if err == nil && imageReceivedHTML(a.MediaType, media, data) {
			return nil, fmt.Errorf("image URL returned an HTML page")
		}
	default:
		return nil, fmt.Errorf("missing asset payload")
	}
	if err != nil {
		return nil, err
	}
	if len(data) > dzzzr.MaxAdminFileBytes {
		return nil, fmt.Errorf("asset too large")
	}
	if err := validateRelocatableAsset(a, data); err != nil {
		return nil, err
	}
	if a.SizeBytes != nil && *a.SizeBytes != int64(len(data)) {
		return nil, fmt.Errorf("asset size mismatch")
	}
	if a.SHA256 != "" && a.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
		return nil, fmt.Errorf("asset SHA-256 mismatch")
	}
	return data, nil
}

func (s *Scenario) prepareAssets(ctx context.Context, files ScenarioFiles) (map[string][]byte, error) {
	out := map[string][]byte{}
	total := 0
	for _, key := range slices.Sorted(maps.Keys(s.Assets)) {
		data, err := files.load(ctx, s.Assets[key])
		if err != nil {
			return nil, fmt.Errorf("asset %s: %w", key, err)
		}
		total += len(data)
		if total > maxScenarioAssetBytes {
			return nil, fmt.Errorf("scenario assets exceed 64 MiB")
		}
		out[key] = data
	}
	return out, nil
}

// URL rewriting is restricted to HTML attributes and CSS url() tokens. Text
// and script bodies are not subject to global string substitution.
var scenarioAttr = regexp.MustCompile(`(?is)(\s)([a-z_:][a-z0-9_:.-]*)(\s*=\s*)("[^"]*"|'[^']*'|[^\s>]+)`)
var scenarioCSSURL = regexp.MustCompile(`(?is)url\(\s*(?:"([^"]*)"|'([^']*)'|([^\s)]*))\s*\)`)

func rewriteCSS(text string, visit func(string) (string, error)) (string, error) {
	var first error
	out := scenarioCSSURL.ReplaceAllStringFunc(text, func(token string) string {
		m := scenarioCSSURL.FindStringSubmatch(token)
		raw := m[1] + m[2] + m[3]
		next, err := visit(raw)
		if err != nil {
			first = err
			return token
		}
		if next == raw {
			return token
		}
		return `url("` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(next) + `")`
	})
	return out, first
}

func rewriteHTML(text string, visit func(string) (string, error)) (string, error) {
	z := html.NewTokenizer(strings.NewReader(text))
	var out strings.Builder
	inStyle := false
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if !errors.Is(z.Err(), io.EOF) {
				return "", z.Err()
			}
			return out.String(), nil
		}
		raw := string(z.Raw())
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			tag, _ := z.TagName()
			name := string(tag)
			inStyle = name == "style"
			var first error
			raw = scenarioAttr.ReplaceAllStringFunc(raw, func(token string) string {
				m := scenarioAttr.FindStringSubmatch(token)
				key := strings.ToLower(m[2])
				value := m[4]
				if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') {
					value = value[1 : len(value)-1]
				}
				value = stdhtml.UnescapeString(value)
				next := value
				var err error
				switch key {
				case "src", "poster", "background", "data":
					next, err = visit(value)
				case "href":
					if name == "link" || strings.HasPrefix(value, "asset:") || strings.Contains(value, "/uploaded/") || fileLink(value) {
						next, err = visit(value)
					}
				case "style":
					next, err = rewriteCSS(value, visit)
				case "srcset":
					// Embedded data URLs contain commas; already self-contained.
					if strings.Contains(value, "data:") {
						break
					}
					parts := strings.Split(value, ",")
					for i, part := range parts {
						fields := strings.Fields(part)
						if len(fields) == 0 {
							continue
						}
						fields[0], err = visit(fields[0])
						if err != nil {
							break
						}
						parts[i] = strings.Join(fields, " ")
					}
					next = strings.Join(parts, ", ")
				}
				if err != nil {
					first = err
					return token
				}
				if next == value {
					return token
				}
				return m[1] + m[2] + m[3] + `"` + stdhtml.EscapeString(next) + `"`
			})
			if first != nil {
				return "", first
			}
		} else if tt == html.TextToken && inStyle {
			var err error
			raw, err = rewriteCSS(raw, visit)
			if err != nil {
				return "", err
			}
		} else if tt == html.EndTagToken {
			inStyle = false
		}
		out.WriteString(raw)
	}
}

func fileLink(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg", ".pdf", ".mp3", ".mp4", ".ogg", ".wav", ".zip", ".txt":
		return true
	}
	return false
}

func (s *Scenario) transformHTML(visit func(string) (string, error)) error {
	transform := func(params map[string]any, keys []string) error {
		for _, key := range keys {
			text, ok := params[key].(string)
			if !ok {
				continue
			}
			next, err := rewriteHTML(text, visit)
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			params[key] = next
		}
		return nil
	}
	if err := transform(s.Game.Params, []string{"legend", "legend_comment", "anons", "breaf_place", "greeting"}); err != nil {
		return err
	}
	for _, l := range s.Levels {
		if err := transform(l.Params, []string{"question", "hint1", "hint2", "location", "location_comment", "comment"}); err != nil {
			return fmt.Errorf("level %s: %w", l.Key, err)
		}
		if spoilers, ok := l.Params["spoilers"].([]any); ok {
			for _, spoiler := range spoilers {
				if m, ok := spoiler.(map[string]any); ok {
					if err := transform(m, []string{"text"}); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (s *Scenario) exportAssets(ctx context.Context, opts ExportOptions) error {
	var base *url.URL
	if opts.BaseURL != "" {
		var err error
		base, err = httpURL(strings.TrimRight(opts.BaseURL, "/") + "/admin/")
		if err != nil {
			return err
		}
	}
	seen := map[string]string{}
	total := 0
	return s.transformHTML(func(raw string) (string, error) {
		if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "data:") {
			return raw, nil
		}
		u, err := url.Parse(raw)
		if err != nil {
			return "", err
		}
		if u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" {
			return raw, nil
		}
		if !u.IsAbs() {
			if base == nil {
				return "", fmt.Errorf("base URL is required for relative assets")
			}
			u = base.ResolveReference(u)
		}
		if _, err := httpURL(u.String()); err != nil {
			return "", err
		}
		fragment := ""
		if u.Fragment != "" {
			fragment = "#" + u.EscapedFragment()
		}
		u.Fragment, u.RawFragment = "", ""
		absolute := u.String()
		if key, ok := seen[absolute]; ok {
			return "asset:" + key + fragment, nil
		}
		key := fmt.Sprintf("file_%d", len(seen)+1)
		filename := path.Base(u.Path)
		if filename == "." || filename == "/" || filename == "" {
			filename = key
		}
		media := mime.TypeByExtension(path.Ext(filename))
		media, _, _ = mime.ParseMediaType(media)
		if media == "" {
			media = "application/octet-stream"
		}
		a := ScenarioAsset{Filename: filename, MediaType: media, OriginalURL: absolute}
		if opts.LinkedAssets {
			a.URL = absolute
		} else {
			data, contentType, err := opts.Files.fetch(ctx, absolute)
			if err != nil {
				return "", fmt.Errorf("asset %s: %w", key, err)
			}
			total += len(data)
			if total > maxScenarioAssetBytes {
				return "", fmt.Errorf("scenario assets exceed 64 MiB")
			}
			// A login page is not a successful download of an image.
			if imageReceivedHTML(media, contentType, data) {
				return "", fmt.Errorf("asset %s returned HTML instead of an image", key)
			}
			if err := validateRelocatableAsset(a, data); err != nil {
				return "", fmt.Errorf("asset %s: %w", key, err)
			}
			a.DataBase64 = new(base64.StdEncoding.EncodeToString(data))
			a.SizeBytes = new(int64(len(data)))
			a.SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
		}
		seen[absolute] = key
		s.Assets[key] = a
		return "asset:" + key + fragment, nil
	})
}

func splitAssetReference(raw string) (key, fragment string, ok bool) {
	rest, ok := strings.CutPrefix(raw, "asset:")
	if !ok {
		return "", "", false
	}
	key, suffix, found := strings.Cut(rest, "#")
	if found {
		fragment = "#" + suffix
	}
	return key, fragment, true
}

func imageReceivedHTML(expected, actual string, data []byte) bool {
	return strings.HasPrefix(expected, "image/") && (actual == "text/html" || strings.HasPrefix(http.DetectContentType(data), "text/html"))
}

var cssImport = regexp.MustCompile(`(?i)@import\b`)

// Nested asset dependencies are not a website archive. Reject relocations
// that would break relative URLs instead of claiming a successful import.
func validateRelocatableAsset(a ScenarioAsset, data []byte) error {
	if imageReceivedHTML(a.MediaType, "", data) {
		return fmt.Errorf("image payload contains HTML")
	}
	ext := strings.ToLower(path.Ext(a.Filename))
	if a.MediaType != "text/css" && a.MediaType != "image/svg+xml" && a.MediaType != "text/html" && ext != ".css" && ext != ".svg" && ext != ".html" && ext != ".htm" {
		return nil
	}
	if cssImport.Match(data) {
		return fmt.Errorf("CSS imports cannot be relocated; inline their dependencies or keep the original external stylesheet URL")
	}
	check := func(raw string) (string, error) {
		if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "data:") {
			return raw, nil
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "asset" || (!u.IsAbs() && u.Host == "") {
			return "", fmt.Errorf("relative dependency inside %s cannot be relocated; embed it or use an absolute URL", a.Filename)
		}
		return raw, nil
	}
	if _, err := rewriteCSS(string(data), check); err != nil {
		return err
	}
	if ext == ".css" || a.MediaType == "text/css" {
		return nil
	}
	z := html.NewTokenizer(strings.NewReader(string(data)))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if errors.Is(z.Err(), io.EOF) {
				return nil
			}
			return z.Err()
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		for _, attr := range z.Token().Attr {
			switch attr.Key {
			case "src", "href", "xlink:href", "poster", "background", "data":
				if _, err := check(attr.Val); err != nil {
					return err
				}
			}
		}
	}
}
