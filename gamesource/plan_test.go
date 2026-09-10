package gamesource

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func validPlan(mode string) Plan {
	p := Plan{GameID: 42, Mode: mode, Levels: []Level{{Params: dzzzr.LevelParams{Title: "Уровень один", Question: "Задание"}}, {Params: dzzzr.LevelParams{Title: "Уровень два"}}}}
	if mode == "update" {
		p.Levels[0].ID = 10
		p.Levels[1].ID = 20
	}
	return p
}

type fakeEngine struct {
	calls      []string
	failCreate int
	wrongGame  bool
	writes     []dzzzr.LevelParams
}

func (e *fakeEngine) AdminCreateLevel(_ context.Context, _ int, p dzzzr.LevelParams) (int, error) {
	e.calls = append(e.calls, "create")
	e.writes = append(e.writes, p)
	if len(e.writes) == e.failCreate {
		return 0, errors.New("write failed")
	}
	return 100 + len(e.writes), nil
}
func (e *fakeEngine) AdminGetLevel(_ context.Context, gid, id int) (*dzzzr.AdminLevelInfo, error) {
	e.calls = append(e.calls, "get")
	if e.wrongGame && id == 20 {
		gid++
	}
	return &dzzzr.AdminLevelInfo{ID: id, GameID: gid}, nil
}
func (e *fakeEngine) AdminReplaceLevel(_ context.Context, _, _ int, p dzzzr.LevelParams) error {
	e.calls = append(e.calls, "replace")
	e.writes = append(e.writes, p)
	return nil
}

func TestDecodeStrict(t *testing.T) {
	for _, data := range []string{
		`{"game_id":42,"mode":"create","levels":[{"params":{"title":"Good","typo":1}}]}`,
		`{"game_id":42,"game_id":43,"mode":"create","levels":[{"params":{"title":"Good"}}]}`,
		`{"game_id":42,"mode":"create","levels":[{"params":{"title":"Good","hint1_on_request":"true"}}]}`,
		`{"game_id":42,"mode":"create","levels":[{"params":{"title":"Good"}}]} {}`,
		`null`,
	} {
		if _, err := Decode([]byte(data)); err == nil {
			t.Errorf("accepted %s", data)
		}
	}
	if _, err := Decode([]byte(`{"game_id":42,"mode":"create","levels":[{"params":{"title":"Good"}}]}`)); err != nil {
		t.Fatal(err)
	}
}

func TestPreflightInvalidLaterLevel(t *testing.T) {
	p := validPlan("create")
	p.Levels[1].Params.Question = "😀"
	engine := &fakeEngine{}
	result, err := p.Apply(t.Context(), engine)
	if err == nil || !strings.Contains(err.Error(), "levels[1]") || !strings.Contains(err.Error(), "Windows-1251") {
		t.Fatalf("error = %v", err)
	}
	if len(engine.calls) != 0 || len(result.Completed) != 0 {
		t.Fatalf("preflight made changes: %+v %+v", engine, result)
	}
}

func TestValidationAggregatesAndCodeConflicts(t *testing.T) {
	p := validPlan("create")
	p.Levels[0].Params.Codes = []dzzzr.LevelCode{{Code: "CODE", Synonyms: "Alias", Danger: "1"}}
	p.Levels[0].Params.BonusCodes = []dzzzr.BonusCode{{Code: "alias", Minutes: 1}}
	p.Levels[1].Params.ClueMin = -1
	p.Levels[1].Params.Codes = []dzzzr.LevelCode{{Code: "X", Danger: "invalid", Sector: 3}}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, s := range []string{"duplicate code", "levels[0]", "levels[1]", "clue_min", "danger", "sector"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("missing %q in %v", s, err)
		}
	}
}

func TestApplyPartialResult(t *testing.T) {
	p := validPlan("create")
	p.Levels = append(p.Levels, p.Levels[0])
	engine := &fakeEngine{failCreate: 2}
	result, err := p.Apply(t.Context(), engine)
	if err == nil || result.FailedIndex == nil || *result.FailedIndex != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !reflect.DeepEqual(result.Completed, []AppliedLevel{{Index: 0, ID: 101}}) || len(engine.calls) != 2 {
		t.Fatalf("result=%+v calls=%v", result, engine.calls)
	}
}

func TestUpdateChecksAllTargetsFirst(t *testing.T) {
	p := validPlan("update")
	engine := &fakeEngine{wrongGame: true}
	result, err := p.Apply(t.Context(), engine)
	if err == nil || len(result.Completed) != 0 || !reflect.DeepEqual(engine.calls, []string{"get", "get"}) {
		t.Fatalf("result=%+v err=%v calls=%v", result, err, engine.calls)
	}
	engine = &fakeEngine{}
	result, err = p.Apply(t.Context(), engine)
	if err != nil || len(result.Completed) != 2 || !reflect.DeepEqual(engine.calls, []string{"get", "get", "replace", "replace"}) {
		t.Fatalf("result=%+v err=%v calls=%v", result, err, engine.calls)
	}
	if !reflect.DeepEqual(engine.writes[0], p.Levels[0].Params) {
		t.Fatal("params changed")
	}
}

func TestApplyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	engine := &fakeEngine{}
	result, err := validPlan("create").Apply(ctx, engine)
	if !errors.Is(err, context.Canceled) || len(engine.calls) != 0 || result.FailedIndex == nil || *result.FailedIndex != 0 {
		t.Fatalf("result=%+v err=%v engine=%+v", result, err, engine)
	}
}

func TestIDRules(t *testing.T) {
	for _, test := range []struct {
		name string
		plan Plan
	}{
		{"update zero", validPlan("update")}, {"create nonzero", validPlan("create")}, {"update duplicate", validPlan("update")},
	} {
		t.Run(test.name, func(t *testing.T) {
			switch test.name {
			case "update zero":
				test.plan.Levels[1].ID = 0
			case "create nonzero":
				test.plan.Levels[1].ID = 10
			case "update duplicate":
				test.plan.Levels[1].ID = 10
			}
			engine := &fakeEngine{}
			if _, err := test.plan.Apply(t.Context(), engine); err == nil || len(engine.calls) != 0 {
				t.Fatalf("err=%v calls=%v", err, engine.calls)
			}
		})
	}
}
