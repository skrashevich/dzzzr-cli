package agenttools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Options configures a catalog.
type Options struct {
	// Policy decides what the agent may do. Empty means DefaultPolicy.
	Policy Policy
	// Confirmer authorizes mutating calls under PolicyApprove.
	Confirmer Confirmer
	// ReadCacheTTL memoizes read-tool results for this long. Zero disables
	// caching. Callers that serve discrete turns should also call
	// InvalidateCache between them.
	ReadCacheTTL time.Duration
	// IncludeAdmin adds the admin_* tools. They need organizer credentials on
	// the client; without them the tools are omitted.
	IncludeAdmin bool
	// FilesRoot restricts local upload paths. Empty uses DZZR_FILES_ROOT or the working directory.
	FilesRoot string
	// Reauthenticate restores a rejected site session using credentials held by
	// the host. Read tools retry once; mutating workflows are never replayed.
	Reauthenticate func(context.Context) error
}

// Catalog is the engine toolset bound to one engine and one access policy.
type Catalog struct {
	policy Policy
	cache  *readCache
	all    []*Tool
	byName map[string]*Tool
}

// NewCatalog builds the toolset.
func NewCatalog(engine Engine, opts Options) (*Catalog, error) {
	if engine == nil {
		return nil, errors.New("agenttools: engine is required")
	}
	policy, err := ParsePolicy(string(opts.Policy))
	if err != nil {
		return nil, err
	}
	if policy == PolicyApprove && opts.Confirmer == nil {
		return nil, errors.New("agenttools: policy \"approve\" requires a Confirmer")
	}
	g := &gate{policy: policy, confirmer: opts.Confirmer}
	c := &Catalog{policy: policy, cache: newReadCache(opts.ReadCacheTTL), byName: map[string]*Tool{}}
	c.add(playerReadTools(engine, g)...)
	c.add(playerMutatingTools(engine, g)...)
	root, err := uploadRoot(opts.FilesRoot)
	if err != nil {
		return nil, err
	}
	c.add(pdfReadTool(g, root))
	c.add(pdfMappingTools(g, root)...)
	if opts.IncludeAdmin && engine.HasAdminCredentials() {
		c.add(adminReadTools(engine, g)...)
		c.add(adminMutatingTools(engine, g)...)
		c.add(adminManagementTools(engine, g)...)
		c.add(adminSourceTools(engine, g, root)...)
		c.add(adminScenarioTools(engine, g, root)...)
		c.add(pdfUploadTool(engine, g, root))
	}
	for _, t := range c.all {
		t.reauthenticate = opts.Reauthenticate
	}
	return c, nil
}

func (c *Catalog) add(list ...*Tool) {
	for _, t := range list {
		t.cache = c.cache
		c.all = append(c.all, t)
		c.byName[t.name] = t
	}
}

// InvalidateCache drops every memoized read. Callers serving discrete
// requests should call this between them: the game moves while the player
// reads, and answering a new question from a stale level is worse than a
// slower fresh read.
func (c *Catalog) InvalidateCache() { c.cache.clear() }

// Policy returns the access policy the catalog was built with.
func (c *Catalog) Policy() Policy { return c.policy }

// Tools returns the tools an agent may see. Under PolicyReadonly the mutating
// ones are withheld so the model is never tempted to call them.
func (c *Catalog) Tools() []*Tool {
	out := make([]*Tool, 0, len(c.all))
	for _, t := range c.all {
		if t.mutating && c.policy == PolicyReadonly {
			continue
		}
		out = append(out, t)
	}
	return out
}

// All returns every tool, including the ones the policy hides.
func (c *Catalog) All() []*Tool { return append([]*Tool(nil), c.all...) }

// Lookup finds a tool by name, ignoring policy visibility.
func (c *Catalog) Lookup(name string) (*Tool, bool) {
	t, ok := c.byName[name]
	return t, ok
}

// SystemPromptAddendum describes the engine and the active policy to a model.
func (c *Catalog) SystemPromptAddendum() string {
	var b strings.Builder
	b.WriteString("You act for a team playing Dozor Classic (classic.dzzzr.ru) through the tools below. ")
	b.WriteString("Game state changes constantly, so read it again before reasoning about it instead of ")
	b.WriteString("trusting an earlier turn.\n")
	b.WriteString("A level is passed by submitting codes. The engine answers every submission with a numeric ")
	b.WriteString("result code and its Russian wording; report that wording rather than inventing your own.\n")
	b.WriteString("Levels can carry bonus codes (extra minutes), fake codes (a penalty), spoilers unlocked by ")
	b.WriteString("their own code, and hints that open on a timer or on request. Taking a hint early costs the ")
	b.WriteString("team minutes, so never do it unasked.\n")
	switch c.policy.normalized() {
	case PolicyReadonly:
		b.WriteString("This session is read-only: you cannot submit codes or change anything. Describe what you would do.\n")
	case PolicyApprove:
		b.WriteString("Every action that changes the game asks the user first. State plainly what you are about to do.\n")
	case PolicyFull:
		b.WriteString("You may act without asking, so be conservative: submitting a wrong code counts against the team's attempt limit.\n")
	}
	b.WriteString(pdfScenarioInstructions)
	b.WriteString(pdfMappingInstructions)
	return b.String()
}

func playerReadTools(e Engine, g *gate) []*Tool {
	return []*Tool{
		{
			name:        "game_status",
			description: "Текущее состояние игры команды: игра, уровень, найденные коды, подсказки, таймер, сообщения организатора.",
			parameters:  schema(nil),
			gate:        g,
			run: func(ctx context.Context, _ arguments) (any, error) {
				st, err := e.GetGame(ctx)
				if err != nil {
					return nil, err
				}
				return newGameView(st), nil
			},
		},
		{
			name:        "level_info",
			description: "Подробности текущего уровня: коды сложности с отметкой найденных, секторы, бонусные коды, доступность перерыва и отказа.",
			parameters:  schema(nil),
			gate:        g,
			run: func(ctx context.Context, _ arguments) (any, error) {
				info, err := e.GetLevelInfo(ctx)
				if err != nil {
					return nil, err
				}
				return newLevelInfoView(info), nil
			},
		},
		{
			name:        "bonus_levels",
			description: "Сквозные бонусные уровни, которые команда решает параллельно основным.",
			parameters:  schema(nil),
			gate:        g,
			run: func(ctx context.Context, _ arguments) (any, error) {
				info, err := e.GetBonusLevelInfo(ctx)
				if err != nil {
					return nil, err
				}
				return newLevelInfoView(info), nil
			},
		},
		{
			name:        "team_stat",
			description: "Статистика команды по уровням: время на каждом уровне.",
			parameters:  schema(nil),
			gate:        g,
			run: func(ctx context.Context, _ arguments) (any, error) {
				st, err := e.GetStat(ctx)
				if err != nil {
					return nil, err
				}
				return statView{Time: st.Time, Rows: st.Rows()}, nil
			},
		},
		{
			name:        "team_log",
			description: "Лог команды: выдача уровней, принятые и отклонённые коды.",
			parameters:  schema(nil),
			gate:        g,
			run: func(ctx context.Context, _ arguments) (any, error) {
				entries, err := e.GetLog(ctx)
				if err != nil {
					return nil, err
				}
				return newLogViews(entries), nil
			},
		},
		{
			name:        "messages",
			description: "Переписка с организатором и штабом.",
			parameters: schema(map[string]any{
				"after": strProp("Показать только сообщения позже этого момента, формат YYYY-MM-DD HH:MM:SS"),
			}),
			gate: g,
			run: func(ctx context.Context, args arguments) (any, error) {
				after, _ := args.optionalString("after")
				msgs, err := e.GetMessages(ctx, after)
				if err != nil {
					return nil, err
				}
				return newChatViews(msgs), nil
			},
		},
		{
			name:        "game_stat",
			description: "Итоговая статистика завершённой игры по всем командам: время на каждом задании, штрафы, бонусы, место. Читается из архива по номеру игры и доступна всем.",
			parameters:  schema(map[string]any{"game_id": intProp("Номер игры из архива (games_list)")}, "game_id"),
			gate:        g,
			run: func(ctx context.Context, args arguments) (any, error) {
				id, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				st, err := e.GetArchiveStat(ctx, id)
				if err != nil {
					return nil, err
				}
				return newArchiveStatView(st), nil
			},
		},
		{
			name: "game_description",
			description: "Опубликованный сценарий завершённой игры: легенда, авторы и все задания с кодами, подсказками, спойлерами и комментариями авторов. " +
				"Тексты заданий движок показывает только капитану, замкапитана или начштаба команды, недавно игравшей в этом городе; иначе в ответе restricted=true, " +
				"есть только шапка, и notice объясняет причину — так и передайте пользователю. " +
				"Целиком это длинный документ, поэтому для разбора одного задания указывайте level.",
			parameters: schema(map[string]any{
				"game_id": intProp("Номер игры из архива (games_list)"),
				"level":   intProp("Порядковый номер задания начиная с 1; без него возвращаются все"),
			}, "game_id"),
			gate: g,
			run: func(ctx context.Context, args arguments) (any, error) {
				id, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				d, err := e.GetArchiveDescription(ctx, id)
				if err != nil {
					return nil, err
				}
				n, ok := args.optionalInt("level")
				if !ok {
					return d, nil
				}
				if n < 1 || n > len(d.Levels) {
					return nil, fmt.Errorf("game %d has %d levels, no level %d", id, len(d.Levels), n)
				}
				one := *d
				one.Levels = d.Levels[n-1 : n]
				return one, nil
			},
		},
		{
			name: "game_log",
			description: "Полный лог игры по всем командам: выдача уровней, принятые и отклонённые коды, подсказки. " +
				"Права те же, что у сценария: капитан, замкапитана или начштаба недавно игравшей команды; иначе возвращается ошибка о нехватке прав.",
			parameters: schema(map[string]any{"game_id": intProp("Номер игры"), "offset": intProp("Смещение записей, с нуля"), "limit": intProp("Размер страницы: 1–500, по умолчанию 100")}, "game_id"),
			gate:       g,
			run: func(ctx context.Context, args arguments) (any, error) {
				id, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				offset, limit := 0, 100
				if _, ok := args["offset"]; ok {
					offset, err = args.requireInt("offset")
					if err != nil {
						return nil, err
					}
				}
				if _, ok := args["limit"]; ok {
					limit, err = args.requireInt("limit")
					if err != nil {
						return nil, err
					}
				}
				if offset < 0 || limit < 1 || limit > 500 {
					return nil, fmt.Errorf("offset must be non-negative and limit must be 1..500")
				}
				log, err := e.GetGameLog(ctx, id)
				if err != nil {
					return nil, err
				}
				start := min(offset, len(log.Entries))
				end := start + min(limit, len(log.Entries)-start)
				result := map[string]any{"game_id": log.GameID, "columns": log.Columns, "entries": log.Entries[start:end], "total": len(log.Entries), "offset": start}
				if end < len(log.Entries) {
					result["next_offset"] = end
				}
				return result, nil
			},
		},
		{
			name: "games_list",
			description: "Список игр города: анонсы, идущие и архивные. Возвращает краткую страницу, по умолчанию 20 игр, в порядке движка. " +
				"Для поиска игр конкретной команды задайте team: для анонсов и идущих игр CLI фильтрует по составам из списка. " +
				"В архиве (archive=true) составов в списке нет, поэтому CLI сам читает публичную таблицу результатов каждой завершённой игры " +
				"и возвращает место и отставание команды (колонки «Место» и «Отставание»); это ожидаемо и ограничено по числу запросов, next_offset продолжает просмотр. " +
				"stat_errors — число архивных игр, чью таблицу результатов прочитать не удалось: при stat_errors > 0 список неполон, продолжите просмотр и не выдавайте его за окончательный ответ. " +
				"next_offset указывает продолжение: передайте его как offset с теми же фильтрами. Не называйте одну страницу полным архивом; загружайте следующие, если пользователь просит весь список.",
			parameters: schema(map[string]any{
				"new_only": boolProp("Только будущие игры"),
				"team":     strProp("Точное название команды, без учёта регистра. Фильтрация внутри CLI; для прошедших игр задайте archive=true — тогда фильтр идёт по таблицам результатов, а в ответ добавляются place и gap"),
				"archive":  boolProp("Только завершённые игры"),
				"offset":   intProp("Смещение игр, с нуля; для продолжения используйте next_offset"),
				"limit":    intProp("Размер страницы: 1–50, по умолчанию 20"),
			}),
			gate: g,
			run: func(ctx context.Context, args arguments) (any, error) {
				return gamesListPage(ctx, e, args)
			},
		},
	}
}

// currentLevel resolves the level number an action needs when the caller did
// not name one.
func currentLevel(ctx context.Context, e Engine, args arguments) (int, error) {
	if n, ok := args.optionalInt("level"); ok && n > 0 {
		return n, nil
	}
	st, err := e.GetGame(ctx)
	if err != nil {
		return 0, err
	}
	if st.Level == nil || st.Level.LevelNumber.Int() == 0 {
		return 0, fmt.Errorf("no current level; pass \"level\" explicitly")
	}
	return st.Level.LevelNumber.Int(), nil
}

// hintTarget resolves the level and hint number for an early hint request,
// defaulting to the current level's first hint the engine has not issued.
func hintTarget(ctx context.Context, e Engine, args arguments) (level, hint int, err error) {
	level, _ = args.optionalInt("level")
	hint, hintGiven := args.optionalInt("hint")
	// An out-of-range hint is a mistake, not a request to pick one: taking
	// the other hint instead would charge the team a penalty they did not
	// ask for.
	if hintGiven && hint != 1 && hint != 2 {
		return 0, 0, fmt.Errorf("hint must be 1 or 2, got %d", hint)
	}
	if level > 0 && hintGiven {
		return level, hint, nil
	}
	st, err := e.GetGame(ctx)
	if err != nil {
		return 0, 0, err
	}
	if st.Level == nil || st.Level.LevelNumber.Int() == 0 {
		return 0, 0, fmt.Errorf("no current level; pass \"level\" and \"hint\" explicitly")
	}
	if level <= 0 {
		level = st.Level.LevelNumber.Int()
	}
	if !hintGiven {
		switch {
		case st.Level.Hint1 == "":
			hint = 1
		case st.Level.Hint2 == "":
			hint = 2
		default:
			return 0, 0, fmt.Errorf("both hints of level %d are already issued", level)
		}
	}
	return level, hint, nil
}

func playerMutatingTools(e Engine, g *gate) []*Tool {
	return []*Tool{
		{
			name:        "send_code",
			description: "Отправить код текущего основного уровня. Неверный код расходует лимит попыток.",
			parameters:  schema(map[string]any{"code": strProp("Код как он написан на объекте")}, "code"),
			mutating:    true,
			gate:        g,
			run: func(ctx context.Context, args arguments) (any, error) {
				code, err := args.requireString("code")
				if err != nil {
					return nil, err
				}
				r, err := e.SendCode(ctx, code)
				if err != nil {
					return nil, err
				}
				return newActionView(r), nil
			},
		},
		{
			name:        "send_bonus_code",
			description: "Отправить код сквозного бонусного уровня.",
			parameters: schema(map[string]any{
				"level": intProp("Номер бонусного уровня"),
				"code":  strProp("Код"),
			}, "level", "code"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				level, err := args.requireInt("level")
				if err != nil {
					return nil, err
				}
				code, err := args.requireString("code")
				if err != nil {
					return nil, err
				}
				r, err := e.SendBonusCode(ctx, level, code)
				if err != nil {
					return nil, err
				}
				return newActionView(r), nil
			},
		},
		{
			name:        "send_spoiler_code",
			description: "Отправить код спойлера, чтобы открыть скрытую часть задания.",
			parameters: schema(map[string]any{
				"code":  strProp("Код спойлера"),
				"level": intProp("Номер уровня; по умолчанию текущий"),
			}, "code"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				code, err := args.requireString("code")
				if err != nil {
					return nil, err
				}
				level, err := currentLevel(ctx, e, args)
				if err != nil {
					return nil, err
				}
				r, err := e.SendSpoilerCode(ctx, level, code)
				if err != nil {
					return nil, err
				}
				return newActionView(r), nil
			},
		},
		{
			name:        "take_hint",
			description: "Запросить подсказку уровня, который выдаёт подсказки по запросу.",
			parameters: schema(map[string]any{
				"hint":  intProp("Номер подсказки: 1 или 2"),
				"level": intProp("Номер уровня; по умолчанию текущий"),
			}, "hint"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				hint, err := args.requireInt("hint")
				if err != nil {
					return nil, err
				}
				if hint != 1 && hint != 2 {
					return nil, fmt.Errorf("argument \"hint\" must be 1 or 2")
				}
				level, err := currentLevel(ctx, e, args)
				if err != nil {
					return nil, err
				}
				r, err := e.TakeHint(ctx, level, hint)
				if err != nil {
					return nil, err
				}
				return newActionView(r), nil
			},
		},
		{
			name:        "take_hint_early",
			description: "Взять подсказку досрочно. Команде начисляется штраф, поэтому только по явной просьбе.",
			parameters: schema(map[string]any{
				"hint":  intProp("Номер подсказки, 1 или 2; по умолчанию ещё не выданная"),
				"level": intProp("Номер уровня; по умолчанию текущий"),
			}),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				level, hint, err := hintTarget(ctx, e, args)
				if err != nil {
					return nil, err
				}
				r, err := e.TakeHintEarly(ctx, level, hint)
				if err != nil {
					return nil, err
				}
				return newActionView(r), nil
			},
		},
		{
			name:        "abandon_level",
			description: "Отказаться от текущего уровня и получить следующий. Разрешено дважды за игру и только капитану или штабу.",
			parameters:  schema(nil),
			mutating:    true,
			gate:        g,
			run: func(ctx context.Context, _ arguments) (any, error) {
				r, err := e.Abandon(ctx)
				if err != nil {
					return nil, err
				}
				return newActionView(r), nil
			},
		},
		{
			name:        "take_break",
			description: "Взять перерыв 15 минут после текущего уровня. Разрешено дважды за игру.",
			parameters:  schema(nil),
			mutating:    true,
			gate:        g,
			run: func(ctx context.Context, _ arguments) (any, error) {
				r, err := e.TakeBreak(ctx)
				if err != nil {
					return nil, err
				}
				return newActionView(r), nil
			},
		},
		{
			name:        "stop_break",
			description: "Досрочно закончить перерыв.",
			parameters:  schema(nil),
			mutating:    true,
			gate:        g,
			run: func(ctx context.Context, _ arguments) (any, error) {
				r, err := e.StopBreak(ctx)
				if err != nil {
					return nil, err
				}
				return newActionView(r), nil
			},
		},
		{
			name:        "next_level",
			description: "Перейти на следующий уровень, не добирая бонусные коды текущего.",
			parameters:  schema(map[string]any{"level": intProp("Номер уровня; по умолчанию текущий")}),
			mutating:    true,
			gate:        g,
			run: func(ctx context.Context, args arguments) (any, error) {
				level, err := currentLevel(ctx, e, args)
				if err != nil {
					return nil, err
				}
				r, err := e.NextLevel(ctx, level)
				if err != nil {
					return nil, err
				}
				return newActionView(r), nil
			},
		},
		{
			name:        "select_level",
			description: "Выбрать следующий уровень в играх со свободным порядком.",
			parameters:  schema(map[string]any{"next": intProp("Номер уровня, который берём следующим")}, "next"),
			mutating:    true,
			gate:        g,
			run: func(ctx context.Context, args arguments) (any, error) {
				next, err := args.requireInt("next")
				if err != nil {
					return nil, err
				}
				current, err := currentLevel(ctx, e, arguments{})
				if err != nil {
					return nil, err
				}
				r, err := e.SelectLevel(ctx, current, next)
				if err != nil {
					return nil, err
				}
				return newActionView(r), nil
			},
		},
		{
			name:        "send_message",
			description: "Отправить сообщение организатору от имени команды.",
			parameters:  schema(map[string]any{"text": strProp("Текст сообщения")}, "text"),
			mutating:    true,
			gate:        g,
			run: func(ctx context.Context, args arguments) (any, error) {
				text, err := args.requireString("text")
				if err != nil {
					return nil, err
				}
				// Not PostMessage: API/postmessage.php answers Ok and
				// delivers nothing.
				r, err := e.SendMessageToOrg(ctx, text)
				if err != nil {
					return nil, err
				}
				return newActionView(r), nil
			},
		},
	}
}
