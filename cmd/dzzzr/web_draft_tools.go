package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"

	"github.com/skrashevich/dzzzr-cli/agentloop"
	"github.com/skrashevich/dzzzr-cli/agenttools"
)

func draftProperties(kind string) map[string]any {
	schema := agenttools.GameParamsSchema()
	if kind == "level" {
		schema = agenttools.LevelParamsSchema()
	}
	props, _ := schema["properties"].(map[string]any)
	return props
}

func (h *webHub) httpDraftChat(w http.ResponseWriter, r *http.Request) {
	h.draftMu.Lock()
	defer h.draftMu.Unlock()
	d, err := h.readDraft(r.PathValue("draft"))
	if err != nil {
		draftError(w, err)
		return
	}
	snap, ok := h.store.get(d.ChatID)
	if !ok {
		snap = h.store.create(h.cfg.city, h.defaultPolicy())
		h.store.mu.Lock()
		h.store.chats[snap.ID].DraftID = d.ID
		h.store.mu.Unlock()
		title := "Черновик новой игры"
		if d.GameID > 0 {
			title = fmt.Sprintf("Черновик игры %d", d.GameID)
		}
		for _, doc := range d.Documents {
			if doc.Kind == "game" {
				if name, ok := doc.Params["name"].(string); ok && strings.TrimSpace(name) != "" {
					title = "Черновик: " + strings.TrimSpace(name)
				}
				break
			}
		}
		h.store.appendUser(snap.ID, title+"\nПомогай редактировать общий черновик; изменения в движок сохраняет автор из редактора.")
		if err = h.store.persist(snap.ID); err != nil {
			draftError(w, err)
			return
		}
		d.ChatID = snap.ID
		if err = h.writeDraft(d); err != nil {
			draftError(w, err)
			return
		}
		snap, _ = h.store.get(snap.ID)
	}
	webWriteJSON(w, 200, snap)
}

type draftTool struct {
	hub       *webHub
	draftID   string
	operation string
	readonly  bool
}

func (t *draftTool) Name() string { return "draft_" + t.operation }
func (t *draftTool) Description() string {
	switch t.operation {
	case "read":
		return "Прочитать актуальный общий черновик игры: active — выбранный документ; documents — поля игры и заданий с revision. Это несохранённые в движок правки автора."
	case "open_level":
		return "Открыть существующее задание игры в общем черновике по level_id. Сохранённые в черновике правки остаются. Для чтения ещё не открытых заданий сначала используйте этот инструмент."
	case "update":
		return "Изменить поля документа общего черновика. Сначала прочитай draft_read и передай revision. params — только изменённые поля (массив заменяется целиком). Пустая строка очищает текст. При конфликте перечитай черновик; не затирай правки автора. В движок ничего не записывается."
	default:
		return "Добавить новое задание в общий черновик, без записи в движок. Поля задаются по схеме LevelParams."
	}
}
func (t *draftTool) Parameters() map[string]any {
	props := map[string]any{}
	required := []string{}
	switch t.operation {
	case "update":
		props["document_id"] = map[string]any{"type": "string"}
		props["revision"] = map[string]any{"type": "integer"}
		fields := draftProperties("game")
		for k, v := range draftProperties("level") {
			fields[k] = v
		}
		props["params"] = map[string]any{"type": "object", "properties": fields, "additionalProperties": false}
		required = []string{"document_id", "revision", "params"}
	case "open_level":
		props["level_id"] = map[string]any{"type": "integer", "minimum": 1}
		required = []string{"level_id"}
	case "add_level":
		props["params"] = agenttools.LevelParamsSchema()
		required = []string{"params"}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}
func (t *draftTool) Execute(ctx context.Context, args map[string]any) agenttools.Result {
	fail := func(err error) agenttools.Result { return agenttools.Result{Content: err.Error(), IsError: true} }
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if t.readonly && t.operation != "read" && t.operation != "open_level" {
		return fail(errors.New("чат доступен только для чтения"))
	}
	t.hub.draftMu.Lock()
	defer t.hub.draftMu.Unlock()
	d, err := t.hub.readDraft(t.draftID)
	if err != nil {
		return fail(err)
	}
	switch t.operation {
	case "open_level":
		raw, e := json.Marshal(args)
		if e != nil {
			return fail(e)
		}
		var body struct {
			LevelID int `json:"level_id"`
		}
		if e = json.Unmarshal(raw, &body); e != nil {
			return fail(e)
		}
		if body.LevelID <= 0 || d.GameID <= 0 {
			return fail(errors.New("нужны сохранённая игра и положительный level_id"))
		}
		found := false
		for _, doc := range d.Documents {
			if doc.Kind == "level" && doc.LevelID == body.LevelID {
				found = true
				break
			}
		}
		if !found {
			info, e := t.hub.client.AdminGetLevel(ctx, d.GameID, body.LevelID)
			if e != nil {
				return fail(e)
			}
			raw, e = json.Marshal(info.Params)
			if e != nil {
				return fail(e)
			}
			var params map[string]any
			if e = json.Unmarshal(raw, &params); e != nil {
				return fail(e)
			}
			doc := &draftDocument{ID: newChatID(), Kind: "level", LevelID: body.LevelID, Revision: 1, Params: params, Base: maps.Clone(params)}
			d.Documents[doc.ID] = doc
			err = t.hub.writeDraft(d)
		}
	case "update":
		raw, e := json.Marshal(args)
		if e != nil {
			return fail(e)
		}
		var body struct {
			DocumentID string         `json:"document_id"`
			Revision   int            `json:"revision"`
			Params     map[string]any `json:"params"`
		}
		if e = json.Unmarshal(raw, &body); e != nil {
			return fail(e)
		}
		d, err = t.hub.patchDraft(d.ID, body.DocumentID, patchDraftBody{Revision: body.Revision, Params: body.Params})
	case "add_level":
		params, ok := args["params"].(map[string]any)
		if !ok {
			return fail(errors.New("нужны поля params"))
		}
		for key := range params {
			if _, ok := draftProperties("level")[key]; !ok {
				return fail(fmt.Errorf("неизвестное поле %s", key))
			}
		}
		doc := &draftDocument{ID: newChatID(), Kind: "level", Revision: 1, Params: params, Base: map[string]any{}}
		d.Documents[doc.ID] = doc
		err = t.hub.writeDraft(d)
	}
	if err != nil {
		return fail(err)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return fail(err)
	}
	if t.operation != "read" && d.ChatID != "" {
		t.hub.publishSSE(d.ChatID, "draft", map[string]string{"id": d.ID})
	}
	return agenttools.Result{Content: string(raw)}
}

// draftTurnTools removes every engine mutation, even in a full-policy chat.
// Local draft edits are reversible; publishing stays an explicit editor action.
func (h *webHub) draftTurnTools(draftID string, policy agenttools.Policy, catalog *agenttools.Catalog) []agentloop.Tool {
	var tools []agentloop.Tool
	for _, tool := range catalog.Tools() {
		if !tool.Mutating() {
			tools = append(tools, tool)
		}
	}
	tools = append(tools, &draftTool{hub: h, draftID: draftID, operation: "read", readonly: true}, &draftTool{hub: h, draftID: draftID, operation: "open_level", readonly: true})
	if policy != agenttools.PolicyReadonly {
		for _, operation := range []string{"update", "add_level"} {
			tools = append(tools, &draftTool{hub: h, draftID: draftID, operation: operation})
		}
	}
	return tools
}
