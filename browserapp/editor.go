package browserapp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"github.com/skrashevich/dzzzr-cli/editorform"
	"github.com/skrashevich/dzzzr-cli/gamesource"
)

// Workspace is the browser's local game library. All editor and agent writes
// share this lock and revisioned documents. No engine credentials are involved.
type Workspace struct {
	mu    sync.Mutex
	State WorkspaceState
}
type WorkspaceState struct {
	Version int                `json:"version"`
	Next    int                `json:"next"`
	Games   map[int]*LocalGame `json:"games"`
	Drafts  map[string]*Draft  `json:"drafts"`
}
type LocalGame struct {
	ID       int                  `json:"id"`
	Scenario *gamesource.Scenario `json:"scenario"`
	LevelIDs []int                `json:"level_ids"`
}
type Draft struct {
	ID        string               `json:"id"`
	GameID    int                  `json:"game_id"`
	ChatID    string               `json:"chat_id"`
	Active    string               `json:"active"`
	Documents map[string]*Document `json:"documents"`
}
type Document struct {
	ID       string         `json:"id"`
	Order    int            `json:"order"`
	Kind     string         `json:"kind"`
	LevelID  int            `json:"level_id"`
	Revision int            `json:"revision"`
	Params   map[string]any `json:"params"`
	Base     map[string]any `json:"base"`
}
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string             { return e.Message }
func apiError(code int, message string) error { return &APIError{code, message} }
func clone[T any](v T) T                      { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }
func (w *Workspace) init() {
	if w.State.Games == nil {
		w.State = WorkspaceState{Version: 1, Games: map[int]*LocalGame{}, Drafts: map[string]*Draft{}}
	}
}
func (w *Workspace) id() int { w.State.Next++; return w.State.Next }
func paramsFor(kind string) map[string]any {
	if kind == "level" {
		return gamesource.CompleteLevelParams(dzzzr.LevelParams{})
	}
	return gamesource.CompleteGameParams(dzzzr.GameParams{})
}
func schemaFor(kind string) map[string]any {
	if kind == "level" {
		return agenttools.LevelParamsSchema()
	}
	return agenttools.GameParamsSchema()
}
func mergeParams(kind string, old, patch map[string]any) (map[string]any, error) {
	if kind != "game" && kind != "level" {
		return nil, fmt.Errorf("неверный вид документа")
	}
	props := schemaFor(kind)["properties"].(map[string]any)
	out := clone(old)
	if out == nil {
		out = map[string]any{}
	}
	for k, v := range patch {
		if _, ok := props[k]; !ok {
			return nil, fmt.Errorf("неизвестное поле %s", k)
		}
		out[k] = clone(v)
	}
	_, err := completeParams(kind, out)
	return out, err
}

// Empty numeric controls mean the explicit default (or inherited level penalty).
// Drafts retain the control representation; scenarios contain typed values.
func completeParams(kind string, params map[string]any) (map[string]any, error) {
	if kind != "game" && kind != "level" {
		return nil, fmt.Errorf("неверный вид документа")
	}
	normalized := clone(params)
	props := schemaFor(kind)["properties"].(map[string]any)
	for key, value := range normalized {
		if value == "" {
			prop, _ := props[key].(map[string]any)
			if prop["type"] != "string" {
				delete(normalized, key)
			}
		}
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	if kind == "level" {
		var p dzzzr.LevelParams
		err = json.Unmarshal(raw, &p, json.RejectUnknownMembers(true))
		return gamesource.CompleteLevelParams(p), err
	}
	var p dzzzr.GameParams
	err = json.Unmarshal(raw, &p, json.RejectUnknownMembers(true))
	return gamesource.CompleteGameParams(p), err
}
func validation(kind string, params map[string]any) []string {
	complete, err := completeParams(kind, params)
	raw, _ := json.Marshal(complete)
	if err == nil {
		if kind == "level" {
			var p dzzzr.LevelParams
			err = json.Unmarshal(raw, &p)
			if err == nil {
				err = (gamesource.Plan{GameID: 1, Mode: "create", Levels: []gamesource.Level{{Params: p}}}).Validate()
			}
		} else {
			var p dzzzr.GameParams
			err = json.Unmarshal(raw, &p)
			if err == nil {
				return editorform.ValidateGame(p)
			}
		}
	}
	if err == nil {
		return []string{}
	}
	return strings.Split(err.Error(), "\n")
}
func (w *Workspace) Snapshot() WorkspaceState {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.init()
	return clone(w.State)
}
func (w *Workspace) Restore(raw any) error {
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	var state WorkspaceState
	if err = json.Unmarshal(b, &state, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	if state.Version != 1 || state.Next < 0 || state.Games == nil || state.Drafts == nil {
		return fmt.Errorf("неподдерживаемое или повреждённое хранилище")
	}
	maxID := 0
	for id, g := range state.Games {
		if g == nil || g.ID != id || g.Scenario == nil || len(g.LevelIDs) != len(g.Scenario.Levels) {
			return fmt.Errorf("повреждена игра %d", id)
		}
		if _, err = gamesource.EncodeScenario(g.Scenario); err != nil {
			return err
		}
		maxID = max(maxID, id)
		for _, n := range g.LevelIDs {
			maxID = max(maxID, n)
		}
	}
	for id, d := range state.Drafts {
		n, _ := strconv.Atoi(strings.TrimPrefix(id, "draft-"))
		maxID = max(maxID, n)
		if d == nil || d.ID != id || d.Documents == nil {
			return fmt.Errorf("повреждён черновик")
		}
		if d.GameID > 0 && state.Games[d.GameID] == nil {
			return fmt.Errorf("игра черновика отсутствует")
		}
		for key, doc := range d.Documents {
			n, _ := strconv.Atoi(strings.TrimPrefix(key, "doc-"))
			maxID = max(maxID, n)
			if doc != nil && doc.Order == 0 {
				doc.Order = n
			}
			if doc == nil || doc.ID != key || doc.Revision < 1 {
				return fmt.Errorf("повреждён документ")
			}
			if _, err = mergeParams(doc.Kind, nil, doc.Params); err != nil {
				return err
			}
		}
		if d.Documents[d.Active] == nil {
			return fmt.Errorf("активный документ отсутствует")
		}
	}
	state.Next = max(state.Next, maxID)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.State = state
	return nil
}
func (w *Workspace) open(b map[string]any) (*Draft, error) {
	kind := str(b, "kind")
	if kind != "game" && kind != "level" {
		return nil, fmt.Errorf("нужен kind game или level")
	}
	gid, lid := integer(b, "game_id", 0), integer(b, "level_id", 0)
	if gid < 0 || lid < 0 {
		return nil, fmt.Errorf("неверный идентификатор")
	}
	var g *LocalGame
	if gid > 0 {
		g = w.State.Games[gid]
		if g == nil {
			return nil, apiError(404, "игра не найдена")
		}
	}
	d := w.State.Drafts[str(b, "draft_id")]
	if str(b, "draft_id") != "" && d == nil {
		return nil, apiError(404, "черновик не найден")
	}
	if d != nil && d.GameID != gid {
		return nil, fmt.Errorf("игра не соответствует черновику")
	}
	if d == nil && gid > 0 {
		for _, candidate := range w.State.Drafts {
			if candidate.GameID == gid {
				d = candidate
				break
			}
		}
	}
	if d == nil {
		d = &Draft{ID: fmt.Sprintf("draft-%d", w.id()), GameID: gid, Documents: map[string]*Document{}}
	}
	doc := d.Documents[str(b, "document_id")]
	if doc == nil && (kind == "game" || lid > 0) {
		for _, item := range d.Documents {
			if item.Kind == kind && item.LevelID == lid {
				doc = item
				break
			}
		}
	}
	if doc == nil {
		params, _ := b["params"].(map[string]any)
		if params == nil && g != nil {
			if kind == "game" {
				params = g.Scenario.Game.Params
			} else {
				index := slices.Index(g.LevelIDs, lid)
				if index < 0 && lid > 0 {
					return nil, apiError(404, "уровень не найден")
				}
				if index >= 0 {
					params = g.Scenario.Levels[index].Params
				}
			}
		}
		values, err := mergeParams(kind, paramsFor(kind), params)
		if err != nil {
			return nil, err
		}
		doc = &Document{ID: fmt.Sprintf("doc-%d", w.id()), Order: w.State.Next, Kind: kind, LevelID: lid, Revision: 1, Params: values, Base: clone(values)}
		d.Documents[doc.ID] = doc
	}
	d.Active = doc.ID
	w.State.Drafts[d.ID] = d
	return d, nil
}
func (w *Workspace) patch(d *Draft, docID string, b map[string]any) (*Draft, error) {
	doc := d.Documents[docID]
	if doc == nil {
		return nil, apiError(404, "документ не найден")
	}
	if integer(b, "revision", 0) != doc.Revision {
		return nil, apiError(409, "черновик уже изменён; перечитайте и объедините правки")
	}
	p, ok := b["params"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("нужен объект params")
	}
	merged, err := mergeParams(doc.Kind, doc.Params, p)
	if err != nil {
		return nil, err
	}
	doc.Params = merged
	doc.Revision++
	return d, nil
}
func newScenario(p map[string]any) *gamesource.Scenario {
	return &gamesource.Scenario{Format: "dzzzr-scenario", Version: 1, Game: gamesource.ScenarioGame{Params: p}, Levels: []gamesource.ScenarioLevel{}, Assets: map[string]gamesource.ScenarioAsset{}}
}
func (w *Workspace) publish(d *Draft, docID string, b map[string]any) (*Draft, error) {
	doc := d.Documents[docID]
	if doc == nil {
		return nil, apiError(404, "документ не найден")
	}
	if doc.Revision != integer(b, "revision", 0) {
		return nil, apiError(409, "черновик уже изменён")
	}
	if issues := validation(doc.Kind, doc.Params); len(issues) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(issues, "\n"))
	}
	if d.GameID == 0 {
		if doc.Kind != "game" {
			return nil, fmt.Errorf("сначала сохраните игру")
		}
		id := w.id()
		w.State.Games[id] = &LocalGame{ID: id, Scenario: newScenario(paramsFor("game")), LevelIDs: []int{}}
		d.GameID = id
	}
	g := w.State.Games[d.GameID]
	if doc.Kind == "game" {
		g.Scenario.Game.Params, _ = completeParams("game", doc.Params)
		delete(g.Scenario.Game.Params, "clear")
	} else {
		params, _ := completeParams(doc.Kind, doc.Params)
		delete(params, "clear")
		delete(params, "greeting")
		index := slices.Index(g.LevelIDs, doc.LevelID)
		if index < 0 {
			doc.LevelID = w.id()
			g.LevelIDs = append(g.LevelIDs, doc.LevelID)
			g.Scenario.Levels = append(g.Scenario.Levels, gamesource.ScenarioLevel{Key: fmt.Sprintf("level-%d", doc.LevelID), Params: params})
		} else {
			g.Scenario.Levels[index].Params = params
		}
	}
	doc.Base = clone(doc.Params)
	doc.Revision++
	return d, nil
}

// API implements the editor's existing API contract over local documents.
func (w *Workspace) API(method, path string, b map[string]any) (any, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.init()
	if method == "" {
		method = "GET"
	}
	path, _, _ = strings.Cut(path, "?")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	result, err := w.api(method, path, parts, b)
	if err != nil {
		return nil, err
	}
	if d, ok := result.(*Draft); ok {
		return clone(d), nil
	}
	return clone(result), nil
}
func (w *Workspace) api(method, path string, p []string, b map[string]any) (any, error) {
	switch path {
	case "/admin/status":
		return map[string]any{"has_admin": true, "city": "локально", "login": "Сценарии в этом браузере", "admin_url": ""}, nil
	case "/admin/schema":
		game, level := agenttools.GameParamsSchema(), agenttools.LevelParamsSchema()
		return map[string]any{"game": map[string]any{"form": editorform.Game(), "schema": game, "clearable": editorform.Clearable(game, dzzzr.GameTextParam)}, "level": map[string]any{"form": editorform.Level(), "schema": level, "clearable": editorform.Clearable(level, dzzzr.LevelTextParam)}}, nil
	case "/admin/validate":
		params, _ := b["params"].(map[string]any)
		issues := validation(str(b, "kind"), params)
		return map[string]any{"ok": len(issues) == 0, "errors": issues}, nil
	case "/admin/games":
		games := []any{}
		ids := []int{}
		for id := range w.State.Games {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			g := w.State.Games[id]
			games = append(games, map[string]any{"id": id, "name": g.Scenario.Game.Params["name"], "date": g.Scenario.Game.Params["date"], "status": "локальная"})
		}
		return map[string]any{"games": games}, nil
	case "/admin/drafts/open":
		return w.open(b)
	case "/admin/scenario/validate", "/admin/scenario/import":
		raw, err := json.Marshal(b["scenario"])
		if err != nil {
			return nil, err
		}
		s, err := gamesource.DecodeScenario(raw)
		if err == nil {
			err = gamesource.ValidateScenario(s)
		}
		if path == "/admin/scenario/validate" {
			issues := []string{}
			if err != nil {
				issues = strings.Split(err.Error(), "\n")
			}
			return map[string]any{"ok": err == nil, "errors": issues}, nil
		}
		if err != nil {
			return nil, err
		}
		gid := integer(b, "game_id", 0)
		if gid > 0 && w.State.Games[gid] == nil {
			return nil, apiError(404, "игра не найдена")
		}
		if gid > 0 {
			for _, d := range w.State.Drafts {
				if d.GameID == gid {
					return nil, fmt.Errorf("для замены игры с открытыми черновиками импортируйте новую копию")
				}
			}
		} else {
			gid = w.id()
		}
		mapping := map[string]int{}
		g := &LocalGame{ID: gid, Scenario: s, LevelIDs: []int{}}
		requested, _ := b["level_ids"].(map[string]any)
		for _, l := range s.Levels {
			lid := w.id()
			if requested != nil {
				return nil, fmt.Errorf("карта идентификаторов движка не нужна при локальном импорте")
			}
			g.LevelIDs = append(g.LevelIDs, lid)
			mapping[l.Key] = lid
		}
		w.State.Games[gid] = g
		return map[string]any{"game_id": gid, "level_ids": mapping, "complete": true}, nil
	}
	if len(p) >= 3 && p[0] == "admin" && p[1] == "drafts" {
		d := w.State.Drafts[p[2]]
		if d == nil {
			return nil, apiError(404, "черновик не найден")
		}
		if len(p) == 3 && method == "GET" {
			return d, nil
		}
		if len(p) == 4 && p[3] == "chat" {
			d.ChatID = d.ID
			return map[string]any{"id": d.ID}, nil
		}
		if len(p) >= 5 && p[3] == "documents" {
			if len(p) == 6 && p[5] == "publish" && method == "POST" {
				return w.publish(d, p[4], b)
			}
			if len(p) == 5 && method == "PATCH" {
				return w.patch(d, p[4], b)
			}
		}
	}
	if len(p) >= 3 && p[0] == "admin" && p[1] == "games" {
		gid, _ := strconv.Atoi(p[2])
		g := w.State.Games[gid]
		if g == nil {
			return nil, apiError(404, "игра не найдена")
		}
		if len(p) == 3 {
			if method == "DELETE" {
				delete(w.State.Games, gid)
				for id, d := range w.State.Drafts {
					if d.GameID == gid {
						delete(w.State.Drafts, id)
					}
				}
				return map[string]any{"ok": true}, nil
			}
			if method == "GET" {
				return map[string]any{"params": g.Scenario.Game.Params}, nil
			}
		}
		if len(p) == 4 {
			switch p[3] {
			case "assets":
				return map[string]any{"assets": g.Scenario.Assets}, nil
			case "files":
				raw, err := base64.StdEncoding.DecodeString(str(b, "data"))
				if err != nil {
					return nil, err
				}
				if len(raw) > 16<<20 {
					return nil, fmt.Errorf("вложение превышает 16 МБ")
				}
				media := http.DetectContentType(raw)
				if !strings.HasPrefix(media, "image/") {
					return nil, fmt.Errorf("выберите PNG, JPEG, GIF или WebP")
				}
				hash := sha256.Sum256(raw)
				key := "image-" + hex.EncodeToString(hash[:])[:20]
				encoded := base64.StdEncoding.EncodeToString(raw)
				size := int64(len(raw))
				if g.Scenario.Assets == nil {
					g.Scenario.Assets = map[string]gamesource.ScenarioAsset{}
				}
				g.Scenario.Assets[key] = gamesource.ScenarioAsset{Filename: filepath.Base(str(b, "name")), MediaType: media, SHA256: hex.EncodeToString(hash[:]), SizeBytes: &size, DataBase64: &encoded}
				return map[string]any{"url": "asset:" + key}, nil
			case "scenario":
				s := clone(g.Scenario)
				s.ExportedAt = time.Now().UTC().Format(time.RFC3339)
				// Include every saved draft, including agent-created levels. Export never
				// discards pending local work; schema validation reports incomplete fields.
				for _, d := range w.State.Drafts {
					if d.GameID != gid {
						continue
					}
					keys := []string{}
					for k := range d.Documents {
						keys = append(keys, k)
					}
					slices.SortFunc(keys, func(a, b string) int {
						left, right := d.Documents[a].Order, d.Documents[b].Order
						if left < right {
							return -1
						}
						if left > right {
							return 1
						}
						return strings.Compare(a, b)
					})
					for _, k := range keys {
						doc := d.Documents[k]
						params, _ := completeParams(doc.Kind, doc.Params)
						delete(params, "clear")
						if doc.Kind == "game" {
							s.Game.Params = params
						} else {
							delete(params, "greeting")
							i := slices.Index(g.LevelIDs, doc.LevelID)
							if i >= 0 {
								s.Levels[i].Params = params
							} else {
								s.Levels = append(s.Levels, gamesource.ScenarioLevel{Key: doc.ID, Params: params})
							}
						}
					}
				}
				raw, err := gamesource.EncodeScenario(s)
				if err != nil {
					return nil, err
				}
				var out any
				err = json.Unmarshal(raw, &out)
				return out, err
			case "copy":
				copy := clone(g)
				copy.ID = w.id()
				copy.Scenario.Game.Params["name"] = fmt.Sprint(g.Scenario.Game.Params["name"]) + " — копия"
				copy.LevelIDs = []int{}
				if b["with_levels"] == false {
					copy.Scenario.Levels = []gamesource.ScenarioLevel{}
				}
				for range copy.Scenario.Levels {
					copy.LevelIDs = append(copy.LevelIDs, w.id())
				}
				w.State.Games[copy.ID] = copy
				return map[string]any{"game_id": copy.ID, "id": copy.ID}, nil
			case "levels":
				levels := []any{}
				for i, l := range g.Scenario.Levels {
					levels = append(levels, map[string]any{"id": g.LevelIDs[i], "title": l.Params["title"], "order": i + 1, "codes": l.Params["codes"], "published": l.Params["publish"], "kind": ""})
				}
				return map[string]any{"levels": levels}, nil
			}
		}
		if len(p) >= 5 && p[3] == "levels" {
			lid, _ := strconv.Atoi(p[4])
			index := slices.Index(g.LevelIDs, lid)
			if index < 0 {
				return nil, apiError(404, "уровень не найден")
			}
			if len(p) == 5 {
				if method == "GET" {
					return map[string]any{"params": g.Scenario.Levels[index].Params}, nil
				}
				if method == "DELETE" {
					g.Scenario.Levels = slices.Delete(g.Scenario.Levels, index, index+1)
					g.LevelIDs = slices.Delete(g.LevelIDs, index, index+1)
					for _, d := range w.State.Drafts {
						if d.GameID == gid {
							for id, doc := range d.Documents {
								if doc.LevelID == lid {
									delete(d.Documents, id)
									if d.Active == id {
										d.Active = ""
										for k := range d.Documents {
											d.Active = k
											break
										}
									}
								}
							}
						}
					}
					return map[string]any{"ok": true}, nil
				}
			}
			if len(p) == 6 && p[5] == "move" {
				target := index + 1
				if b["up"] == true {
					target = index - 1
				}
				if target >= 0 && target < len(g.LevelIDs) {
					g.LevelIDs[index], g.LevelIDs[target] = g.LevelIDs[target], g.LevelIDs[index]
					g.Scenario.Levels[index], g.Scenario.Levels[target] = g.Scenario.Levels[target], g.Scenario.Levels[index]
				}
				return map[string]any{"ok": true}, nil
			}
		}
	}
	return nil, apiError(404, "локальный метод не поддерживается: "+method+" "+path)
}
