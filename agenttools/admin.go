package agenttools

import (
	"context"
	"fmt"
)

// Admin tools mirror the organizer's screens. They are only added when the
// caller asked for them and the client actually holds organizer credentials,
// so a player session never sees a tool it cannot use.

func adminReadTools(e Engine, g *gate) []*Tool {
	gameIDProp := intProp("Идентификатор игры")
	return []*Tool{
		{
			name:        "admin_games",
			description: "Список игр организатора: дата, номер, название, статус.",
			parameters:  schema(nil),
			gate:        g,
			run: func(ctx context.Context, _ arguments) (any, error) {
				return e.AdminListGames(ctx)
			},
		},
		{
			name:        "admin_game",
			description: "Настройки игры: название, дата, интервалы подсказок, мастер-код, штрафы.",
			parameters:  schema(map[string]any{"game_id": gameIDProp}, "game_id"),
			gate:        g,
			run: func(ctx context.Context, args arguments) (any, error) {
				id, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				info, err := e.AdminGetGame(ctx, id)
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": info.ID, "params": info.Params}, nil
			},
		},
		{
			name:        "admin_levels",
			description: "Уровни игры: номер, название, коды, сколько кодов нужно, тип уровня.",
			parameters:  schema(map[string]any{"game_id": gameIDProp}, "game_id"),
			gate:        g,
			run: func(ctx context.Context, args arguments) (any, error) {
				id, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				return e.AdminListLevels(ctx, id)
			},
		},
		{
			name:        "admin_level",
			description: "Полное содержимое уровня: задание, подсказки, коды с секторами, бонусные и ложные коды, спойлеры.",
			parameters: schema(map[string]any{
				"game_id":  gameIDProp,
				"level_id": intProp("Идентификатор уровня из admin_levels (поле id, не номер)"),
			}, "game_id", "level_id"),
			gate: g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				levelID, err := args.requireInt("level_id")
				if err != nil {
					return nil, err
				}
				info, err := e.AdminGetLevel(ctx, gameID, levelID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": info.ID, "order": info.Order, "params": info.Params}, nil
			},
		},
		{
			name:        "admin_teams",
			description: "Команды игры: статус заявки, PIN, рейтинговые очки.",
			parameters:  schema(map[string]any{"game_id": gameIDProp}, "game_id"),
			gate:        g,
			run: func(ctx context.Context, args arguments) (any, error) {
				id, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				return e.AdminListTeams(ctx, id)
			},
		},
		{
			name:        "admin_monitor",
			description: "Экран мониторинга игры: команды, уровни, работает ли движок.",
			parameters:  schema(map[string]any{"game_id": gameIDProp}, "game_id"),
			gate:        g,
			run: func(ctx context.Context, args arguments) (any, error) {
				id, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				return e.AdminMonitor(ctx, id)
			},
		},
		{
			name:        "admin_log",
			description: "Свежие события игры: выдачи уровней, принятые и неверные коды. Возвращает записи и метку времени для следующего запроса.",
			parameters: schema(map[string]any{
				"game_id": gameIDProp,
				"since":   strProp("Метка времени предыдущего вызова, формат YYYY-MM-DD HH:MM:SS"),
			}, "game_id"),
			gate: g,
			run: func(ctx context.Context, args arguments) (any, error) {
				id, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				since, _ := args.optionalString("since")
				entries, next, err := e.AdminGetLog(ctx, id, since)
				if err != nil {
					return nil, err
				}
				return map[string]any{"entries": entries, "next_since": next}, nil
			},
		},
		{
			name:        "admin_messages",
			description: "Переписка организатора с командами в игре.",
			parameters: schema(map[string]any{
				"game_id": gameIDProp,
				"team_id": intProp("Показать только переписку с этой командой"),
			}, "game_id"),
			gate: g,
			run: func(ctx context.Context, args arguments) (any, error) {
				id, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				teamID, _ := args.optionalInt("team_id")
				return e.AdminListMessages(ctx, id, teamID)
			},
		},
	}
}

func adminMutatingTools(e Engine, g *gate) []*Tool {
	gameIDProp := intProp("Идентификатор игры")
	teamIDProp := intProp("Идентификатор команды")
	return []*Tool{
		{
			name:        "admin_give_level",
			description: "Выдать команде уровень.",
			parameters: schema(map[string]any{
				"game_id": gameIDProp, "team_id": teamIDProp,
				"level": intProp("Номер уровня"),
				"time":  strProp("Момент выдачи, формат YYYY-MM-DD HH:MM:SS; по умолчанию сейчас"),
			}, "game_id", "team_id", "level"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, teamID, level, err := adminTriple(args, "level")
				if err != nil {
					return nil, err
				}
				at, _ := args.optionalString("time")
				if err := e.AdminGiveLevel(ctx, gameID, teamID, level, at); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_accept_code",
			description: "Засчитать команде код на уровне, как будто она ввела его сама.",
			parameters: schema(map[string]any{
				"game_id": gameIDProp, "team_id": teamIDProp,
				"level": intProp("Номер уровня"), "code": strProp("Код"),
				"time": strProp("Момент, формат YYYY-MM-DD HH:MM:SS; по умолчанию сейчас"),
			}, "game_id", "team_id", "level", "code"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, teamID, level, err := adminTriple(args, "level")
				if err != nil {
					return nil, err
				}
				code, err := args.requireString("code")
				if err != nil {
					return nil, err
				}
				at, _ := args.optionalString("time")
				if err := e.AdminAcceptCode(ctx, gameID, teamID, level, code, at); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_add_correction",
			description: "Начислить команде бонусные или штрафные минуты.",
			parameters: schema(map[string]any{
				"game_id": gameIDProp, "team_id": teamIDProp,
				"kind":    map[string]any{"type": "string", "enum": []string{"bonus", "penalty"}, "description": "bonus — бонус, penalty — штраф"},
				"minutes": intProp("Количество минут"),
				"comment": strProp("Комментарий, он попадёт в результаты"),
			}, "game_id", "team_id", "kind", "minutes"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, teamID, minutes, err := adminTriple(args, "minutes")
				if err != nil {
					return nil, err
				}
				kind, err := args.requireString("kind")
				if err != nil {
					return nil, err
				}
				comment, _ := args.optionalString("comment")
				if err := e.AdminAddCorrection(ctx, gameID, teamID, kind, minutes, comment); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_block_team",
			description: "Остановить или возобновить игру команды.",
			parameters: schema(map[string]any{
				"game_id": gameIDProp, "team_id": teamIDProp,
				"blocked": boolProp("true — остановить, false — возобновить"),
			}, "game_id", "team_id", "blocked"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				teamID, err := args.requireInt("team_id")
				if err != nil {
					return nil, err
				}
				blocked, ok := args.optionalBool("blocked")
				if !ok {
					return nil, fmt.Errorf("argument \"blocked\" must be a boolean")
				}
				if blocked {
					err = e.AdminBlockTeam(ctx, gameID, teamID)
				} else {
					err = e.AdminUnblockTeam(ctx, gameID, teamID)
				}
				if err != nil {
					return nil, err
				}
				return map[string]any{"blocked": blocked}, nil
			},
		},
		{
			name:        "admin_set_engine",
			description: "Остановить или запустить движок для всех команд.",
			parameters: schema(map[string]any{
				"game_id": gameIDProp,
				"running": boolProp("true — движок работает, false — остановлен"),
			}, "game_id", "running"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				running, ok := args.optionalBool("running")
				if !ok {
					return nil, fmt.Errorf("argument \"running\" must be a boolean")
				}
				if err := e.AdminSetEngine(ctx, gameID, running); err != nil {
					return nil, err
				}
				return map[string]any{"running": running}, nil
			},
		},
		{
			name:        "admin_send_message",
			description: "Отправить сообщение команде или всем командам игры.",
			parameters: schema(map[string]any{
				"game_id":    gameIDProp,
				"team_id":    intProp("Команда; 0 — всем"),
				"text":       strProp("Текст сообщения"),
				"show_after": strProp("Показать не раньше этого момента, формат YYYY-MM-DD HH:MM:SS"),
				"important":  boolProp("Пометить как важное"),
			}, "game_id", "text"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				text, err := args.requireString("text")
				if err != nil {
					return nil, err
				}
				teamID, _ := args.optionalInt("team_id")
				showAfter, _ := args.optionalString("show_after")
				important, _ := args.optionalBool("important")
				if err := e.AdminSendMessage(ctx, gameID, teamID, text, showAfter, important); err != nil {
					return nil, err
				}
				return map[string]any{"sent": true}, nil
			},
		},
	}
}

// adminTriple reads game_id, team_id and one more required integer.
func adminTriple(args arguments, third string) (gameID, teamID, value int, err error) {
	if gameID, err = args.requireInt("game_id"); err != nil {
		return 0, 0, 0, err
	}
	if teamID, err = args.requireInt("team_id"); err != nil {
		return 0, 0, 0, err
	}
	if value, err = args.requireInt(third); err != nil {
		return 0, 0, 0, err
	}
	return gameID, teamID, value, nil
}
