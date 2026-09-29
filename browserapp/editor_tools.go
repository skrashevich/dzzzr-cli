package browserapp

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"maps"
)

// EditorTools binds the model to the same revisioned drafts the form edits.
func (a *App) EditorTools(draftID *string) []Tool {
	list := []Tool{}
	add := func(n, d string, p map[string]any, required []string, run func(context.Context, map[string]any) (any, error)) {
		list = append(list, localTool{n, d, map[string]any{"type": "object", "properties": p, "required": required, "additionalProperties": false}, run})
	}
	stringProp := map[string]any{"type": "string"}
	intProp := map[string]any{"type": "integer"}
	add("scenario_list", "Перечислить локальные игры, открытые в редакторе.", nil, nil, func(_ context.Context, _ map[string]any) (any, error) {
		return a.Editor.API("GET", "/admin/games", nil)
	})
	add("scenario_read", "Прочитать локальную игру и все уровни, включая правки черновиков.", map[string]any{"game_id": intProp}, []string{"game_id"}, func(_ context.Context, b map[string]any) (any, error) {
		return a.Editor.API("GET", fmt.Sprintf("/admin/games/%d/scenario", integer(b, "game_id", 0)), nil)
	})
	add("scenario_export", "Сохранить локальную игру с актуальными черновиками в JSON для скачивания.", map[string]any{"game_id": intProp, "path": stringProp}, []string{"game_id", "path"}, func(_ context.Context, b map[string]any) (any, error) {
		v, err := a.Editor.API("GET", fmt.Sprintf("/admin/games/%d/scenario", integer(b, "game_id", 0)), nil)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		err = a.put(str(b, "path"), raw)
		return map[string]any{"path": str(b, "path")}, err
	})
	add("scenario_import", "Открыть полный сценарий из загруженного JSON как новую локальную игру и общий черновик. Сохраняет HTML, assets и extensions.", map[string]any{"path": stringProp}, []string{"path"}, func(_ context.Context, b map[string]any) (any, error) {
		raw, err := a.read(str(b, "path"))
		if err != nil {
			return nil, err
		}
		var s any
		if err = json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		v, err := a.Editor.API("POST", "/admin/scenario/import", map[string]any{"scenario": s})
		if err != nil {
			return nil, err
		}
		gid := integer(v.(map[string]any), "game_id", 0)
		d, err := a.Editor.API("POST", "/admin/drafts/open", map[string]any{"game_id": gid, "kind": "game"})
		if err == nil {
			*draftID = d.(*Draft).ID
		}
		return d, err
	})
	add("scenario_create", "Создать общий черновик новой локальной игры. Сначала заполни параметры игры, автор сохранит их из редактора.", map[string]any{"params": schemaFor("game")}, []string{"params"}, func(_ context.Context, b map[string]any) (any, error) {
		params, _ := b["params"].(map[string]any)
		d, err := a.Editor.API("POST", "/admin/drafts/open", map[string]any{"kind": "game", "params": params})
		if err == nil {
			*draftID = d.(*Draft).ID
		}
		return d, err
	})
	require := func() error {
		if *draftID == "" {
			return fmt.Errorf("откройте черновик из редактора или через scenario_create/scenario_import")
		}
		return nil
	}
	add("draft_read", "Прочитать общий черновик: active и documents с revision; правки автора и агента.", nil, nil, func(_ context.Context, _ map[string]any) (any, error) {
		if err := require(); err != nil {
			return nil, err
		}
		return a.Editor.API("GET", "/admin/drafts/"+*draftID, nil)
	})
	add("draft_open_level", "Открыть существующее локальное задание в общем черновике. Сохраняет несохранённые правки.", map[string]any{"level_id": intProp}, []string{"level_id"}, func(_ context.Context, b map[string]any) (any, error) {
		if err := require(); err != nil {
			return nil, err
		}
		v, err := a.Editor.API("GET", "/admin/drafts/"+*draftID, nil)
		if err != nil {
			return nil, err
		}
		return a.Editor.API("POST", "/admin/drafts/open", map[string]any{"draft_id": *draftID, "game_id": v.(*Draft).GameID, "kind": "level", "level_id": integer(b, "level_id", 0)})
	})
	props := maps.Clone(schemaFor("game")["properties"].(map[string]any))
	maps.Copy(props, schemaFor("level")["properties"].(map[string]any))
	add("draft_update", "Изменить только запрошенные поля документа. Сначала draft_read; обязательна актуальная revision. Массив заменяется целиком. Пустая строка очищает текст; при конфликте перечитай.", map[string]any{"document_id": stringProp, "revision": intProp, "params": map[string]any{"type": "object", "properties": props, "additionalProperties": false}}, []string{"document_id", "revision", "params"}, func(_ context.Context, b map[string]any) (any, error) {
		if err := require(); err != nil {
			return nil, err
		}
		return a.Editor.API("PATCH", "/admin/drafts/"+*draftID+"/documents/"+str(b, "document_id"), b)
	})
	add("draft_add_level", "Добавить новое задание в общий черновик, без сохранения игры. Автор видит и сохраняет его из редактора.", map[string]any{"params": schemaFor("level")}, []string{"params"}, func(_ context.Context, b map[string]any) (any, error) {
		if err := require(); err != nil {
			return nil, err
		}
		v, err := a.Editor.API("GET", "/admin/drafts/"+*draftID, nil)
		if err != nil {
			return nil, err
		}
		return a.Editor.API("POST", "/admin/drafts/open", map[string]any{"draft_id": *draftID, "game_id": v.(*Draft).GameID, "kind": "level", "params": b["params"]})
	})
	return list
}
