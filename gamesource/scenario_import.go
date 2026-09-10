package gamesource

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"maps"
	"path"
	"reflect"
	"slices"
	"strings"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

type ScenarioEngine interface {
	ScenarioReader
	AdminCreateGame(context.Context, dzzzr.GameParams) (int, error)
	AdminReplaceGame(context.Context, int, dzzzr.GameParams) error
	AdminCreateLevel(context.Context, int, dzzzr.LevelParams) (int, error)
	AdminReplaceLevel(context.Context, int, int, dzzzr.LevelParams) error
	AdminMoveLevel(context.Context, int, bool) error
	AdminUploadFile(context.Context, int, string, []byte) (*dzzzr.AdminFileUpload, error)
}

type ImportOptions struct {
	// Zero creates a new game; a positive ID selects an explicit destination.
	GameID int
	// Existing nonempty games require a complete key -> destination level ID
	// mapping. The importer never infers identity from source_id or position.
	LevelIDs map[string]int
	Files    ScenarioFiles
}

type ScenarioImportResult struct {
	GameID      int               `json:"game_id"`
	CreatedGame bool              `json:"created_game"`
	LevelIDs    map[string]int    `json:"level_ids"`
	Completed   []string          `json:"completed_levels"`
	Uploaded    map[string]string `json:"uploaded_assets"`
	Stage       string            `json:"stage"`
	FailedKey   string            `json:"failed_key,omitempty"`
	Complete    bool              `json:"complete"`
}

// ValidateScenario checks the structural and semantic snapshot constraints.
// Payloads are additionally read and checked by ImportScenario before writing.
func ValidateScenario(s *Scenario) error {
	data, err := EncodeScenario(s)
	if err != nil {
		return err
	}
	copy, err := DecodeScenario(data)
	if err != nil {
		return err
	}
	return validateScenarioParams(copy)
}

// ImportScenario stops at the first failed or unverified operation. The result
// is returned on every path, since a failed request may already have committed.
// Publication and lifecycle flags are restored only after content verification.
func ImportScenario(ctx context.Context, e ScenarioEngine, source *Scenario, opts ImportOptions) (*ScenarioImportResult, error) {
	r := &ScenarioImportResult{LevelIDs: map[string]int{}, Completed: []string{}, Uploaded: map[string]string{}, Stage: "preflight"}
	if opts.GameID < 0 {
		return r, fmt.Errorf("game_id must be nonnegative")
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	data, err := EncodeScenario(source)
	if err != nil {
		return r, err
	}
	// Work on a deep copy: URL replacement never modifies the caller's source.
	s, err := DecodeScenario(data)
	if err != nil {
		return r, err
	}
	if err := validateScenarioParams(s); err != nil {
		return r, err
	}
	g, err := typedParams[dzzzr.GameParams](s.Game.Params)
	if err != nil {
		return r, err
	}
	if opts.GameID == 0 && (strings.TrimSpace(g.Name) == "" || g.Date == "" || g.Date == "00.00.0000" || g.Time == "") {
		return r, fmt.Errorf("creating a game requires name, date and time")
	}
	if e == nil {
		return r, fmt.Errorf("scenario engine is required")
	}
	existing := []dzzzr.AdminLevel{}
	if opts.GameID > 0 {
		info, err := e.AdminGetGame(ctx, opts.GameID)
		if err != nil {
			return r, err
		}
		if info == nil || info.ID != opts.GameID {
			return r, fmt.Errorf("destination game mismatch")
		}
		existing, err = e.AdminListLevels(ctx, opts.GameID)
		if err != nil {
			return r, err
		}
	}
	if err := validateLevelMapping(s, existing, opts); err != nil {
		return r, err
	}
	for _, id := range opts.LevelIDs {
		info, err := e.AdminGetLevel(ctx, opts.GameID, id)
		if err != nil {
			return r, err
		}
		if info == nil || info.ID != id || info.GameID != opts.GameID {
			return r, fmt.Errorf("mapped level does not belong to destination game")
		}
	}
	assets, err := s.prepareAssets(ctx, opts.Files)
	if err != nil {
		return r, err
	}
	r.GameID = opts.GameID
	if r.GameID == 0 {
		r.Stage = "create_game"
		seed := g
		seed.Publish, seed.Finished, seed.Invitation = new(false), new(false), new(false)
		// Do not send unresolved asset references into the temporary game.
		seed.Legend, seed.LegendComment, seed.Anons, seed.BreafPlace, seed.Greeting = "", "", "", "", ""
		r.GameID, err = e.AdminCreateGame(ctx, seed)
		if err != nil {
			return r, err
		}
		if r.GameID <= 0 {
			return r, fmt.Errorf("invalid game ID after create; inspect engine before retry")
		}
		r.CreatedGame = true
	}
	for _, key := range slices.Sorted(maps.Keys(assets)) {
		r.Stage, r.FailedKey = "upload_asset", key
		digest := fmt.Sprintf("%x", sha256.Sum256(assets[key]))
		ext := path.Ext(s.Assets[key].Filename)
		if len(ext) > 12 {
			ext = ""
		}
		file, err := e.AdminUploadFile(ctx, r.GameID, digest+ext, assets[key])
		if err != nil {
			return r, err
		}
		if file == nil {
			return r, fmt.Errorf("empty upload result")
		}
		if _, err := httpURL(file.URL); err != nil {
			return r, err
		}
		r.Uploaded[key] = file.URL
		if file.AlreadyExists {
			stored, _, err := opts.Files.fetch(ctx, file.URL)
			if err != nil {
				return r, fmt.Errorf("verify existing asset: %w", err)
			}
			if fmt.Sprintf("%x", sha256.Sum256(stored)) != digest {
				return r, fmt.Errorf("existing asset content differs")
			}
		}
	}
	if err := s.transformHTML(func(raw string) (string, error) {
		if key, fragment, ok := splitAssetReference(raw); ok {
			u, found := r.Uploaded[key]
			if !found {
				return "", fmt.Errorf("unresolved asset %s", key)
			}
			return u + fragment, nil
		}
		return raw, nil
	}); err != nil {
		return r, err
	}
	g, err = typedParams[dzzzr.GameParams](s.Game.Params)
	if err != nil {
		return r, err
	}
	draft := g
	draft.Publish, draft.Finished, draft.Invitation = new(false), new(false), new(false)
	r.Stage, r.FailedKey = "write_game", ""
	if err := e.AdminReplaceGame(ctx, r.GameID, draft); err != nil {
		return r, err
	}
	if err := verifyGame(ctx, e, r.GameID, snapshotParams(draft)); err != nil {
		return r, err
	}
	for _, l := range s.Levels {
		r.Stage, r.FailedKey = "write_level", l.Key
		p, err := typedParams[dzzzr.LevelParams](l.Params)
		if err != nil {
			return r, err
		}
		p.Publish = new(false)
		id := opts.LevelIDs[l.Key]
		if id == 0 {
			// The legacy create endpoint requires a title. A private shell is
			// immediately replaced with the full snapshot, including blank titles.
			id, err = e.AdminCreateLevel(ctx, r.GameID, dzzzr.LevelParams{Title: "Scenario import " + l.Key, Publish: new(false)})
			if err != nil {
				return r, err
			}
			if id <= 0 {
				return r, fmt.Errorf("invalid level ID after create; inspect engine before retry")
			}
		}
		r.LevelIDs[l.Key] = id
		if err := e.AdminReplaceLevel(ctx, r.GameID, id, p); err != nil {
			return r, err
		}
		if err := verifyLevel(ctx, e, r.GameID, id, snapshotParams(p)); err != nil {
			return r, err
		}
		r.Completed = append(r.Completed, l.Key)
	}
	r.Stage, r.FailedKey = "order_levels", ""
	if err := orderScenarioLevels(ctx, e, s, r); err != nil {
		return r, err
	}
	for _, l := range s.Levels {
		r.Stage, r.FailedKey = "restore_level_flags", l.Key
		p, err := typedParams[dzzzr.LevelParams](l.Params)
		if err != nil {
			return r, err
		}
		if p.Publish != nil && *p.Publish {
			if err := e.AdminReplaceLevel(ctx, r.GameID, r.LevelIDs[l.Key], p); err != nil {
				return r, err
			}
			if err := verifyLevel(ctx, e, r.GameID, r.LevelIDs[l.Key], l.Params); err != nil {
				return r, err
			}
		}
	}
	r.Stage, r.FailedKey = "restore_game_flags", ""
	if err := e.AdminReplaceGame(ctx, r.GameID, g); err != nil {
		return r, err
	}
	if err := verifyGame(ctx, e, r.GameID, s.Game.Params); err != nil {
		return r, err
	}
	r.Stage, r.Complete = "complete", true
	return r, nil
}

func validateLevelMapping(s *Scenario, levels []dzzzr.AdminLevel, opts ImportOptions) error {
	if len(levels) == 0 {
		if len(opts.LevelIDs) != 0 {
			return fmt.Errorf("level mapping requires existing destination levels")
		}
		return nil
	}
	if len(levels) != len(s.Levels) || len(opts.LevelIDs) != len(s.Levels) {
		return fmt.Errorf("nonempty destination requires a complete level mapping and equal level count; extra levels are never deleted implicitly")
	}
	available := map[int]bool{}
	for _, l := range levels {
		available[l.ID] = true
	}
	for _, l := range s.Levels {
		id := opts.LevelIDs[l.Key]
		if !available[id] {
			return fmt.Errorf("level %s: missing, duplicate or foreign destination ID", l.Key)
		}
		delete(available, id)
	}
	return nil
}

func compareParams(expected, actual map[string]any) error {
	for _, key := range slices.Sorted(maps.Keys(expected)) {
		// Normalize Go integers and JSON numbers without including game content
		// (codes, text) in an error message.
		a, err := json.Marshal(expected[key])
		if err != nil {
			return err
		}
		b, err := json.Marshal(actual[key])
		if err != nil {
			return err
		}
		var av, bv any
		if err := json.Unmarshal(a, &av); err != nil {
			return err
		}
		if err := json.Unmarshal(b, &bv); err != nil {
			return err
		}
		if !reflect.DeepEqual(av, bv) {
			return fmt.Errorf("engine did not preserve field %s", key)
		}
	}
	return nil
}

func verifyGame(ctx context.Context, e ScenarioReader, id int, expected map[string]any) error {
	info, err := e.AdminGetGame(ctx, id)
	if err != nil {
		return err
	}
	if info == nil || info.ID != id {
		return fmt.Errorf("game readback identity mismatch")
	}
	return compareParams(expected, gameSnapshot(info))
}

func verifyLevel(ctx context.Context, e ScenarioReader, gid, id int, expected map[string]any) error {
	info, err := e.AdminGetLevel(ctx, gid, id)
	if err != nil {
		return err
	}
	if info == nil || info.ID != id || info.GameID != gid {
		return fmt.Errorf("level readback identity mismatch")
	}
	return compareParams(expected, levelSnapshot(info))
}

func orderScenarioLevels(ctx context.Context, e ScenarioEngine, s *Scenario, r *ScenarioImportResult) error {
	levels, err := e.AdminListLevels(ctx, r.GameID)
	if err != nil {
		return err
	}
	if len(levels) != len(s.Levels) {
		return fmt.Errorf("destination level count changed during import")
	}
	for target, l := range s.Levels {
		id := r.LevelIDs[l.Key]
		at := slices.IndexFunc(levels, func(l dzzzr.AdminLevel) bool { return l.ID == id })
		if at < target {
			return fmt.Errorf("destination level disappeared or order changed")
		}
		for at > target {
			if err := e.AdminMoveLevel(ctx, id, true); err != nil {
				return err
			}
			levels, err = e.AdminListLevels(ctx, r.GameID)
			if err != nil {
				return err
			}
			if len(levels) != len(s.Levels) {
				return fmt.Errorf("destination level count changed during reorder")
			}
			next := slices.IndexFunc(levels, func(l dzzzr.AdminLevel) bool { return l.ID == id })
			if next != at-1 {
				return fmt.Errorf("engine refused level move or order changed concurrently")
			}
			at = next
		}
	}
	for i, l := range s.Levels {
		if levels[i].ID != r.LevelIDs[l.Key] {
			return fmt.Errorf("level order verification failed")
		}
	}
	return nil
}
