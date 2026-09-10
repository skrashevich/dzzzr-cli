package gamesource

import (
	"context"
	_ "embed"
	"encoding/json/v2"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"golang.org/x/text/encoding/charmap"
)

// MaxScenarioBytes bounds a document including embedded files.
const MaxScenarioBytes = 96 << 20

//go:embed scenario.schema.json
var scenarioSchema []byte

// ScenarioSchema returns the same schema used by the decoder.
func ScenarioSchema() []byte { return append([]byte(nil), scenarioSchema...) }

var resolvedScenario = sync.OnceValues(func() (*jsonschema.Resolved, error) {
	var s jsonschema.Schema
	if err := json.Unmarshal(scenarioSchema, &s); err != nil {
		return nil, err
	}
	return s.Resolve(nil)
})

type Scenario struct {
	Schema     string                   `json:"$schema,omitempty"`
	Format     string                   `json:"format"`
	Version    int                      `json:"version"`
	ExportedAt string                   `json:"exported_at,omitempty"`
	Source     *ScenarioSource          `json:"source,omitempty"`
	Game       ScenarioGame             `json:"game"`
	Levels     []ScenarioLevel          `json:"levels"`
	Assets     map[string]ScenarioAsset `json:"assets"`
	Extensions map[string]any           `json:"extensions,omitempty"`
}

type ScenarioSource struct {
	Engine  string `json:"engine"`
	BaseURL string `json:"base_url"`
	Project string `json:"project"`
	GameID  int    `json:"game_id"`
}

// Parameter maps preserve required zero values, unlike the patch structs.
type ScenarioGame struct {
	Params     map[string]any `json:"params"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

type ScenarioLevel struct {
	Key         string         `json:"key"`
	SourceID    int            `json:"source_id,omitzero"`
	SourceOrder *int           `json:"source_order,omitempty"`
	Params      map[string]any `json:"params"`
	Extensions  map[string]any `json:"extensions,omitempty"`
}

type ScenarioAsset struct {
	Filename    string  `json:"filename"`
	MediaType   string  `json:"media_type"`
	SHA256      string  `json:"sha256,omitempty"`
	SizeBytes   *int64  `json:"size_bytes,omitempty"`
	DataBase64  *string `json:"data_base64,omitempty"`
	Path        string  `json:"path,omitempty"`
	URL         string  `json:"url,omitempty"`
	OriginalURL string  `json:"original_url,omitempty"`
}

func DecodeScenario(data []byte) (*Scenario, error) {
	if len(data) > MaxScenarioBytes {
		return nil, fmt.Errorf("scenario exceeds %d bytes", MaxScenarioBytes)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("scenario JSON: %w", err)
	}
	schema, err := resolvedScenario()
	if err != nil {
		return nil, fmt.Errorf("scenario schema: %w", err)
	}
	if err := schema.Validate(value); err != nil {
		return nil, fmt.Errorf("scenario schema: %w", err)
	}
	var s Scenario
	if err := json.Unmarshal(data, &s, json.RejectUnknownMembers(true)); err != nil {
		return nil, err
	}
	if err := s.validateReferences(); err != nil {
		return nil, err
	}
	return &s, nil
}

// EncodeScenario always validates before producing an export document.
func EncodeScenario(s *Scenario) ([]byte, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	if _, err := DecodeScenario(data); err != nil {
		return nil, err
	}
	return data, nil
}

func (s *Scenario) validateReferences() error {
	keys := map[string]bool{}
	for _, l := range s.Levels {
		if keys[l.Key] {
			return fmt.Errorf("duplicate level key %q", l.Key)
		}
		keys[l.Key] = true
	}
	if s.ExportedAt != "" {
		if _, err := time.Parse(time.RFC3339, s.ExportedAt); err != nil {
			return fmt.Errorf("exported_at: %w", err)
		}
	}
	for key, a := range s.Assets {
		if err := validateAsset(a); err != nil {
			return fmt.Errorf("assets[%s]: %w", key, err)
		}
	}
	return s.transformHTML(func(raw string) (string, error) {
		if key, _, ok := splitAssetReference(raw); ok {
			if _, found := s.Assets[key]; !found {
				return "", fmt.Errorf("unresolved asset %q", key)
			}
		}
		return raw, nil
	})
}

// completeValue materializes patch structs without omitempty. Only penalty
// has a meaningful null; other nil pointers become explicit zero values.
func completeValue(v reflect.Value, field string) any {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			if field == "penalty" {
				return nil
			}
			return completeValue(reflect.Zero(v.Type().Elem()), field)
		}
		return completeValue(v.Elem(), field)
	}
	switch v.Kind() {
	case reflect.Struct:
		out := map[string]any{}
		for i := 0; i < v.NumField(); i++ {
			name, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("json"), ",")
			if name == "clear" || (name == "greeting" && v.Type() == reflect.TypeFor[dzzzr.LevelParams]()) {
				continue
			}
			out[name] = completeValue(v.Field(i), name)
		}
		return out
	case reflect.Slice:
		out := make([]any, v.Len())
		for i := range v.Len() {
			out[i] = completeValue(v.Index(i), "")
		}
		return out
	default:
		return v.Interface()
	}
}

func snapshotParams(p any) map[string]any {
	return completeValue(reflect.ValueOf(p), "").(map[string]any)
}

func typedParams[T any](m map[string]any) (T, error) {
	var p T
	b, err := json.Marshal(m)
	if err == nil {
		err = json.Unmarshal(b, &p, json.RejectUnknownMembers(true))
	}
	return p, err
}

// Preserve whitespace in scenario text: the display-oriented client parser
// trims it, while the original form values still carry the full content.
func gameSnapshot(info *dzzzr.AdminGameInfo) map[string]any {
	p := snapshotParams(info.Params)
	for key, wire := range map[string]string{"name": "name", "number": "number", "date": "date", "time": "time", "author": "author", "zone": "zone", "legend": "legend", "legend_comment": "legendComment", "anons": "anons", "master_code": "masterCode", "price": "price", "breaf_place": "breafPlace", "greeting": "greeting"} {
		if info.Fields.Has(wire) {
			p[key] = info.Fields.Get(wire)
		}
	}
	return p
}

func levelSnapshot(info *dzzzr.AdminLevelInfo) map[string]any {
	p := snapshotParams(info.Params)
	for key, wire := range map[string]string{"title": "title", "subtitle": "subtitle", "question": "question", "hint1": "clue1", "hint2": "clue2", "location": "location", "location_comment": "locationComment", "comment": "comment", "skvoz_start": "skvozStart", "lat": "lat", "lon": "lon", "radius": "radius"} {
		if info.Fields.Has(wire) {
			p[key] = info.Fields.Get(wire)
		}
	}
	return p
}

type ScenarioReader interface {
	AdminGetGame(context.Context, int) (*dzzzr.AdminGameInfo, error)
	AdminListLevels(context.Context, int) ([]dzzzr.AdminLevel, error)
	AdminGetLevel(context.Context, int, int) (*dzzzr.AdminLevelInfo, error)
}

type ExportOptions struct {
	BaseURL string
	// LinkedAssets records URLs instead of downloading and embedding files.
	LinkedAssets bool
	Files        ScenarioFiles
}

func ExportScenario(ctx context.Context, e ScenarioReader, gameID int, opts ExportOptions) (*Scenario, error) {
	if gameID <= 0 {
		return nil, fmt.Errorf("game ID must be positive")
	}
	info, err := e.AdminGetGame(ctx, gameID)
	if err != nil {
		return nil, err
	}
	if info == nil || info.ID != gameID {
		return nil, fmt.Errorf("engine returned a different game")
	}
	levels, err := e.AdminListLevels(ctx, gameID)
	if err != nil {
		return nil, err
	}
	s := &Scenario{Format: "dzzzr-scenario", Version: 1, ExportedAt: time.Now().UTC().Format(time.RFC3339), Game: ScenarioGame{Params: gameSnapshot(info)}, Levels: []ScenarioLevel{}, Assets: map[string]ScenarioAsset{}}
	if opts.BaseURL != "" {
		s.Source = &ScenarioSource{Engine: "classic.dzzzr", BaseURL: opts.BaseURL, GameID: gameID}
	}
	seen := map[int]bool{}
	for _, l := range levels {
		if seen[l.ID] || l.ID <= 0 {
			return nil, fmt.Errorf("invalid or duplicate level ID %d", l.ID)
		}
		seen[l.ID] = true
		info, err := e.AdminGetLevel(ctx, gameID, l.ID)
		if err != nil {
			return nil, err
		}
		if info == nil || info.ID != l.ID || info.GameID != gameID {
			return nil, fmt.Errorf("level %d does not belong to game %d", l.ID, gameID)
		}
		s.Levels = append(s.Levels, ScenarioLevel{Key: "level_" + strconv.Itoa(l.ID), SourceID: l.ID, SourceOrder: new(l.Order), Params: levelSnapshot(info)})
	}
	if err := s.exportAssets(ctx, opts); err != nil {
		return nil, err
	}
	// Export is a sequence of reads; reject an order/membership change mid-read.
	after, err := e.AdminListLevels(ctx, gameID)
	if err != nil {
		return nil, err
	}
	if len(after) != len(levels) {
		return nil, fmt.Errorf("level list changed during export; retry")
	}
	for i := range levels {
		if after[i].ID != levels[i].ID {
			return nil, fmt.Errorf("level order changed during export; retry")
		}
	}
	if _, err := EncodeScenario(s); err != nil {
		return nil, err
	}
	return s, nil
}

func validateScenarioParams(s *Scenario) error {
	// Check encoding and integer precision before any engine write.
	var walk func(any) error
	walk = func(v any) error {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				if err := walk(child); err != nil {
					return fmt.Errorf("%s: %w", k, err)
				}
			}
		case []any:
			for i, child := range x {
				if err := walk(child); err != nil {
					return fmt.Errorf("[%d]: %w", i, err)
				}
			}
		case string:
			if _, err := charmap.Windows1251.NewEncoder().String(x); err != nil {
				return err
			}
		case float64:
			if math.Abs(x) > 1<<53 {
				return fmt.Errorf("integer exceeds exact JSON range")
			}
		}
		return nil
	}
	if err := walk(s.Game.Params); err != nil {
		return fmt.Errorf("game: %w", err)
	}
	g, err := typedParams[dzzzr.GameParams](s.Game.Params)
	if err != nil {
		return err
	}
	if g.Date != "" && g.Date != "00.00.0000" {
		if _, err := time.Parse("02.01.2006", g.Date); err != nil {
			return fmt.Errorf("game date: %w", err)
		}
	}
	for _, l := range s.Levels {
		if err := walk(l.Params); err != nil {
			return fmt.Errorf("level %s: %w", l.Key, err)
		}
		p, err := typedParams[dzzzr.LevelParams](l.Params)
		if err != nil {
			return err
		}
		if p.CodeCount > len(p.Codes) {
			return fmt.Errorf("level %s: code_count exceeds code count", l.Key)
		}
		seen := map[string]bool{}
		check := func(code, synonyms string) error {
			if code == "" && synonyms == "" {
				return nil
			}
			values := []string{code}
			if synonyms != "" {
				values = append(values, strings.Split(synonyms, "#")...)
			}
			for _, value := range values {
				key := strings.ToUpper(strings.TrimSpace(value))
				if key == "" || seen[key] {
					return fmt.Errorf("level %s: empty or duplicate code/synonym %q", l.Key, value)
				}
				seen[key] = true
			}
			return nil
		}
		for _, c := range p.Codes {
			if c.Sector > len(p.SectorNames) {
				return fmt.Errorf("level %s: invalid sector", l.Key)
			}
			if err := check(c.Code, c.Synonyms); err != nil {
				return err
			}
		}
		for _, c := range p.BonusCodes {
			if err := check(c.Code, c.Synonyms); err != nil {
				return err
			}
		}
		for _, c := range p.FakeCodes {
			if err := check(c.Code, c.Synonyms); err != nil {
				return err
			}
		}
		for _, c := range p.Spoilers {
			if err := check(c.Code, c.Synonyms); err != nil {
				return err
			}
		}
		for _, c := range []struct {
			value    string
			min, max float64
		}{{p.Lat, -90, 90}, {p.Lon, -180, 180}, {p.Radius, 0, math.MaxFloat64}} {
			if c.value == "" {
				continue
			}
			n, err := strconv.ParseFloat(c.value, 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < c.min || n > c.max {
				return fmt.Errorf("level %s: invalid coordinates/radius", l.Key)
			}
		}
	}
	return nil
}

func httpURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("expected an HTTP(S) URL without credentials")
	}
	return u, nil
}
