package main

import (
	"encoding/json/jsontext"
	"net/http"
	"strings"

	"github.com/skrashevich/dzzzr-cli/agenttools"
	"github.com/skrashevich/dzzzr-cli/dzzzr"
	"github.com/skrashevich/dzzzr-cli/editorform"
	"github.com/skrashevich/dzzzr-cli/gamesource"
)

type webFormSpec = editorform.Spec

var webFieldType = editorform.FieldType
var webGameFormSpec = editorform.Game
var webLevelFormSpec = editorform.Level
var webClearableParams = editorform.Clearable
var webValidateGameParams = editorform.ValidateGame

// httpAdminSchema serves the form layout together with the raw schema. It asks
// for no organizer credentials on purpose: this is a static description of the
// parameters, and the editor needs it to draw its empty state before anyone
// has signed in.
func (h *webHub) httpAdminSchema(w http.ResponseWriter, r *http.Request) {
	game, level := agenttools.GameParamsSchema(), agenttools.LevelParamsSchema()
	webWriteJSON(w, http.StatusOK, map[string]any{
		"game": map[string]any{
			"form":      webGameFormSpec(),
			"schema":    game,
			"clearable": webClearableParams(game, dzzzr.GameTextParam),
		},
		"level": map[string]any{
			"form":      webLevelFormSpec(),
			"schema":    level,
			"clearable": webClearableParams(level, dzzzr.LevelTextParam),
		},
	})
}

// adminValidateBody is the body of POST /api/v1/admin/validate.
type adminValidateBody struct {
	Kind   string         `json:"kind"`
	Params jsontext.Value `json:"params"`
}

// httpAdminValidate checks parameters without touching the engine, so the
// editor can tell an author about a duplicate code while they are still typing
// instead of after a refused write.
func (h *webHub) httpAdminValidate(w http.ResponseWriter, r *http.Request) {
	var body adminValidateBody
	if !webReadJSON(w, r, &body) {
		return
	}
	switch body.Kind {
	case "level":
		var params dzzzr.LevelParams
		if !decodeParams(w, body.Params, &params) {
			return
		}
		// A one-level create plan is the exported way into gamesource's level
		// checks, and it runs exactly what «dzzzr admin-validate-source» runs.
		plan := gamesource.Plan{GameID: 1, Mode: "create", Levels: []gamesource.Level{{Params: params}}}
		webWriteValidation(w, webValidationProblems(plan.Validate()))
	case "game":
		var params dzzzr.GameParams
		if !decodeParams(w, body.Params, &params) {
			return
		}
		webWriteValidation(w, webValidateGameParams(params))
	default:
		webError(w, http.StatusBadRequest, "неизвестный вид проверки %q: ожидается game или level", body.Kind)
	}
}

// webWriteValidation answers with the verdict and every problem separately: an
// author fixing three things wants to see three lines.
func webWriteValidation(w http.ResponseWriter, problems []string) {
	webWriteJSON(w, http.StatusOK, map[string]any{"ok": len(problems) == 0, "errors": problems})
}

// webValidationProblems splits what gamesource reports. Plan.Validate joins its
// findings with errors.Join, which renders one error per line, so the newline
// is the joiner to split on. The texts themselves are passed through word for
// word: they are what the command-line validator prints, and a translation
// table here would be a second source of truth that drifts silently.
func webValidationProblems(err error) []string {
	problems := []string{}
	if err == nil {
		return problems
	}
	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// The plan always holds exactly one level, so its index is noise.
		problems = append(problems, strings.TrimPrefix(line, "levels[0]: "))
	}
	return problems
}
