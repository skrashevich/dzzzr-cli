// Package gamesource validates and applies ordered JSON batches of game levels.
// Updates replace all modeled level fields, including omitted zero values.
// It never adds technical levels or fetches external documents.
package gamesource

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"golang.org/x/text/encoding/charmap"
)

type Plan struct {
	GameID int     `json:"game_id"`
	Mode   string  `json:"mode"`
	Levels []Level `json:"levels"`
}

type Level struct {
	ID     int               `json:"id"`
	Params dzzzr.LevelParams `json:"params"`
}

type AppliedLevel struct {
	Index int `json:"index"`
	ID    int `json:"id"`
}

// Result identifies successful writes. FailedIndex is zero-based. A failed write
// may have reached the server: callers must inspect it before retrying a create.
type Result struct {
	Completed   []AppliedLevel `json:"completed"`
	FailedIndex *int           `json:"failed_index,omitempty"`
}

type Engine interface {
	AdminCreateLevel(context.Context, int, dzzzr.LevelParams) (int, error)
	AdminGetLevel(context.Context, int, int) (*dzzzr.AdminLevelInfo, error)
	AdminReplaceLevel(context.Context, int, int, dzzzr.LevelParams) error
}

// Decode rejects unknown fields, duplicate JSON members and invalid plans.
func Decode(data []byte) (*Plan, error) {
	var p Plan
	if err := json.Unmarshal(data, &p, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("game source JSON: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Validate checks every level before any network operation. IDs must be zero
// for creation and positive for replacement; update batches never skip rows.
func (p Plan) Validate() error {
	var errs []error
	if p.GameID <= 0 {
		errs = append(errs, errors.New("game_id must be positive"))
	}
	if p.Mode != "create" && p.Mode != "update" {
		errs = append(errs, errors.New("mode must be create or update"))
	}
	if len(p.Levels) == 0 {
		errs = append(errs, errors.New("levels must not be empty"))
	}
	ids := map[int]bool{}
	for i, l := range p.Levels {
		add := func(err error) {
			if err != nil {
				errs = append(errs, fmt.Errorf("levels[%d]: %w", i, err))
			}
		}
		if p.Mode == "create" && l.ID != 0 {
			add(errors.New("id must be zero for create"))
		}
		if p.Mode == "update" {
			if l.ID <= 0 {
				add(errors.New("id must be positive for update"))
			}
			if ids[l.ID] {
				add(fmt.Errorf("duplicate id %d", l.ID))
			}
			ids[l.ID] = true
		}
		add(validateParams(l.Params))
	}
	return errors.Join(errs...)
}

func validateParams(p dzzzr.LevelParams) error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if strings.TrimSpace(p.Title) == "" && strings.TrimSpace(p.Question) == "" {
		add("title or question is required")
	}
	// Walking the typed parameters also covers newly added textual fields and
	// nested code/spoiler strings, avoiding partial writes from encoding failures.
	var walk func(reflect.Value, string)
	walk = func(v reflect.Value, path string) {
		switch v.Kind() {
		case reflect.Pointer:
			if !v.IsNil() {
				walk(v.Elem(), path)
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				name, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("json"), ",")
				walk(v.Field(i), path+"."+name)
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), fmt.Sprintf("%s[%d]", path, i))
			}
		case reflect.String:
			if strings.Contains(v.String(), "pdf-image://") {
				add("%s contains an unresolved pdf-image:// reference; bind uploaded image URLs in the PDF mapping and re-extract before importing", path)
			}
			if _, err := charmap.Windows1251.NewEncoder().String(v.String()); err != nil {
				add("%s cannot be encoded as Windows-1251: %v", path, err)
			}
		case reflect.Int:
			if v.Int() < 0 {
				add("%s must not be negative", path)
			}
		}
	}
	walk(reflect.ValueOf(p), "params")
	seen := map[string]string{}
	checkCode := func(code, synonyms, path string) {
		if strings.TrimSpace(code) == "" {
			add("%s.code is required", path)
			return
		}
		values := []string{code}
		if synonyms != "" {
			values = append(values, strings.Split(synonyms, "#")...)
		}
		for _, s := range values {
			key := strings.ToUpper(strings.TrimSpace(s))
			if key == "" {
				add("%s has an empty synonym", path)
				continue
			}
			if prev, ok := seen[key]; ok {
				add("duplicate code or synonym %q in %s and %s", s, prev, path)
			}
			seen[key] = path
		}
	}
	danger := func(s, path string, required bool) {
		switch s {
		case "":
			if required {
				add("%s.danger is required", path)
			}
		case "1", "1+", "2", "2+", "3", "3+", "null":
		default:
			add("%s.danger is invalid", path)
		}
	}
	for i, c := range p.Codes {
		path := fmt.Sprintf("codes[%d]", i)
		checkCode(c.Code, c.Synonyms, path)
		danger(c.Danger, path, true)
		if c.Sector > len(p.SectorNames) {
			add("%s.sector exceeds sector_names length", path)
		}
	}
	for i, c := range p.BonusCodes {
		path := fmt.Sprintf("bonus_codes[%d]", i)
		checkCode(c.Code, c.Synonyms, path)
		danger(c.Danger, path, false)
	}
	for i, c := range p.FakeCodes {
		checkCode(c.Code, c.Synonyms, fmt.Sprintf("fake_codes[%d]", i))
	}
	for i, c := range p.Spoilers {
		path := fmt.Sprintf("spoilers[%d]", i)
		checkCode(c.Code, c.Synonyms, path)
		if strings.TrimSpace(c.Text) == "" {
			add("%s.text is required", path)
		}
	}
	if p.CodeCount > len(p.Codes) {
		add("code_count exceeds number of main codes")
	}
	if p.Publish != nil && *p.Publish && (p.Title == "" || p.Question == "" || p.Hint1 == "" || p.Hint2 == "" || len(p.Codes) == 0) {
		add("publish requires title, question, both hints and a main code")
	}
	for _, coord := range []struct {
		name, value string
		min, max    float64
	}{{"lat", p.Lat, -90, 90}, {"lon", p.Lon, -180, 180}, {"radius", p.Radius, 0, math.MaxFloat64}} {
		if coord.value == "" {
			continue
		}
		n, err := strconv.ParseFloat(coord.value, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < coord.min || n > coord.max {
			add("%s must be a finite number between %g and %g", coord.name, coord.min, coord.max)
		}
	}
	return errors.Join(errs...)
}

// Apply validates the entire batch and, for updates, verifies all target game
// memberships before the first write. Writes are ordered and stop on failure.
// There is no rollback; Result is returned even when an error occurs.
func (p Plan) Apply(ctx context.Context, engine Engine) (*Result, error) {
	result := &Result{Completed: []AppliedLevel{}}
	if err := p.Validate(); err != nil {
		return result, err
	}
	if engine == nil {
		return result, errors.New("game source engine is required")
	}
	fail := func(i int, err error) (*Result, error) {
		result.FailedIndex = new(i)
		return result, fmt.Errorf("levels[%d]: %w", i, err)
	}
	if p.Mode == "update" {
		for i, l := range p.Levels {
			if err := ctx.Err(); err != nil {
				return fail(i, err)
			}
			info, err := engine.AdminGetLevel(ctx, p.GameID, l.ID)
			if err != nil {
				return fail(i, err)
			}
			if info == nil || info.ID != l.ID || info.GameID != p.GameID {
				return fail(i, fmt.Errorf("level %d does not belong to game %d", l.ID, p.GameID))
			}
		}
	}
	for i, l := range p.Levels {
		if err := ctx.Err(); err != nil {
			return fail(i, err)
		}
		id := l.ID
		var err error
		if p.Mode == "create" {
			id, err = engine.AdminCreateLevel(ctx, p.GameID, l.Params)
		} else {
			err = engine.AdminReplaceLevel(ctx, p.GameID, l.ID, l.Params)
		}
		if err != nil {
			return fail(i, err)
		}
		if id <= 0 {
			return fail(i, errors.New("engine returned an invalid level id after write"))
		}
		result.Completed = append(result.Completed, AppliedLevel{Index: i, ID: id})
	}
	return result, nil
}
