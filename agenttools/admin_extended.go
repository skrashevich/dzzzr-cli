package agenttools

import (
	"context"
	"fmt"
	"strings"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func adminManagementTools(e Engine, g *gate) []*Tool {
	return []*Tool{
		{
			name:        "admin_team",
			description: "Карточка команды в игре: капитан, штаб, статус заявки, PIN, лига и состав. roster_on_team — логины тех, кого движок считает в команде; roster_size — размер всего списка; full_roster=true добавляет roster_listed_only с остальными.",
			parameters: schema(map[string]any{
				"game_id":     intProp("Идентификатор игры"),
				"team_id":     intProp("Идентификатор команды"),
				"full_roster": boolProp("Добавить roster_listed_only — тех, кто в списке, но не отмечен в команде")}, "game_id", "team_id"),
			mutating: false,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				teamID, err := requireAdminInt(args, "team_id")
				if err != nil {
					return nil, err
				}
				if teamID < 1 {
					return nil, fmt.Errorf("team_id must be at least 1")
				}
				info, err := e.AdminGetTeam(ctx, gameID, teamID)
				if err != nil {
					return nil, err
				}
				out := map[string]any{"id": info.ID, "name": info.Name, "captain": info.Captain, "shtab": info.Shtab, "helper": info.Helper, "status": info.Status, "pin": info.Pin, "points": info.Points, "novice": info.Novice, "blocked": info.Blocked, "website": info.Website}
				if info.League != "" {
					out["league"] = info.League
				}
				// The roster can run to a couple of hundred logins, so the
				// members the engine counts as being on the team are returned
				// by default and the rest only on request. roster_size is
				// always present, so "no members" is distinguishable from a
				// card the client could not read.
				onTeam := make([]string, 0, len(info.Roster))
				for _, p := range info.Roster {
					if p.OnTeam {
						onTeam = append(onTeam, p.Login)
					}
				}
				out["roster_on_team"] = onTeam
				out["roster_size"] = len(info.Roster)
				if full, ok := args.optionalBool("full_roster"); ok && full {
					listed := make([]string, 0, len(info.Roster))
					for _, p := range info.Roster {
						if !p.OnTeam {
							listed = append(listed, p.Login)
						}
					}
					out["roster_listed_only"] = listed
				}
				return out, nil
			},
		},
		{
			name:        "admin_city_teams",
			description: "Справочник команд города из формы заявки на игру: id и название. Нужен, чтобы найти team_id для admin_add_team. Аргумент contains фильтрует по подстроке названия.",
			parameters: schema(map[string]any{
				"game_id":  intProp("Идентификатор игры, на чьей странице читается справочник"),
				"contains": strProp("Фильтр по подстроке названия, регистр не важен")}, "game_id"),
			mutating: false,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				teams, err := e.AdminListCityTeams(ctx, gameID)
				if err != nil {
					return nil, err
				}
				needle, _ := args.optionalString("contains")
				out := make([]map[string]any, 0, len(teams))
				for _, t := range teams {
					if needle != "" && !strings.Contains(strings.ToLower(t.Name), strings.ToLower(needle)) {
						continue
					}
					out = append(out, map[string]any{"id": t.ID, "name": t.Name})
				}
				return map[string]any{"teams": out, "total": len(teams)}, nil
			},
		},
		{
			name:        "admin_create_game",
			description: "Создать игру. В params обязательны name, date (DD.MM.YYYY), time (HH:MM).",
			parameters: schema(map[string]any{
				"params": gameParamsSchema()}, "params"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				var params dzzzr.GameParams
				if err := decodeAdminParams(args, &params); err != nil {
					return nil, err
				}
				id, err := e.AdminCreateGame(ctx, params)
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": id}, nil
			},
		},
		{
			name:        "admin_update_game",
			description: "Изменить настройки игры. Неуказанные поля сохраняются; числовой 0 и пустые строки сохраняют текущее значение.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры"),
				"params":  gameParamsSchema()}, "game_id", "params"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				var params dzzzr.GameParams
				if err := decodeAdminParams(args, &params); err != nil {
					return nil, err
				}
				if err := e.AdminUpdateGame(ctx, gameID, params); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_delete_game",
			description: "Удалить игру. Движок разрешает удаление только игры без уровней.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры")}, "game_id"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				if err := e.AdminDeleteGame(ctx, gameID); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_copy_game",
			description: "Копировать игру в новую, опционально вместе с уровнями.",
			parameters: schema(map[string]any{
				"source_game_id": intProp("Идентификатор исходной игры"),
				"with_levels":    boolProp("Копировать вместе с уровнями; по умолчанию false")}, "source_game_id"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				sourceGameID, err := requireAdminInt(args, "source_game_id")
				if err != nil {
					return nil, err
				}
				if sourceGameID < 1 {
					return nil, fmt.Errorf("source_game_id must be at least 1")
				}
				withLevels, ok := args.optionalBool("with_levels")
				if _, supplied := args["with_levels"]; supplied && !ok {
					return nil, fmt.Errorf("invalid with_levels")
				}
				id, err := e.AdminCopyGame(ctx, sourceGameID, withLevels)
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": id}, nil
			},
		},
		{
			name:        "admin_create_level",
			description: "Создать уровень. Нужны title или question; каждый основной код требует danger. Для публикации нужны задание, обе подсказки и код.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры"),
				"params":  levelParamsSchema()}, "game_id", "params"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				var params dzzzr.LevelParams
				if err := decodeAdminParams(args, &params); err != nil {
					return nil, err
				}
				id, err := e.AdminCreateLevel(ctx, gameID, params)
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": id}, nil
			},
		},
		{
			name:        "admin_update_level",
			description: "Изменить уровень. Списки заменяются целиком, [] очищает список. Пустые строки и непоинтерные числовые 0 сохраняют прежнее значение; false для флагов и penalty=0 применяются.",
			parameters: schema(map[string]any{
				"game_id":  intProp("Идентификатор игры"),
				"level_id": intProp("ID уровня из admin_levels, не порядковый номер"),
				"params":   levelParamsSchema()}, "game_id", "level_id", "params"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				levelID, err := requireAdminInt(args, "level_id")
				if err != nil {
					return nil, err
				}
				if levelID < 1 {
					return nil, fmt.Errorf("level_id must be at least 1")
				}
				var params dzzzr.LevelParams
				if err := decodeAdminParams(args, &params); err != nil {
					return nil, err
				}
				if err := e.AdminUpdateLevel(ctx, gameID, levelID, params); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_delete_level",
			description: "Удалить уровень. Движок разрешает это до старта игры и при отсутствии цепочек.",
			parameters: schema(map[string]any{
				"game_id":  intProp("Идентификатор игры"),
				"level_id": intProp("ID уровня из admin_levels, не порядковый номер")}, "game_id", "level_id"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				levelID, err := requireAdminInt(args, "level_id")
				if err != nil {
					return nil, err
				}
				if levelID < 1 {
					return nil, fmt.Errorf("level_id must be at least 1")
				}
				if err := e.AdminDeleteLevel(ctx, gameID, levelID); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_move_level",
			description: "Передвинуть уровень на одну позицию: up=true вверх, иначе вниз. Движок ничего не сообщает, поэтому порядок читается до и после: в ответе order_before и order_after.",
			parameters: schema(map[string]any{
				"game_id":  intProp("Идентификатор игры"),
				"level_id": intProp("ID уровня из admin_levels, не порядковый номер"),
				"up":       boolProp("Вверх (true) или вниз (false)")}, "game_id", "level_id", "up"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				levelID, err := requireAdminInt(args, "level_id")
				if err != nil {
					return nil, err
				}
				if levelID < 1 {
					return nil, fmt.Errorf("level_id must be at least 1")
				}
				up, ok := args.optionalBool("up")
				if !ok {
					return nil, fmt.Errorf("up must be a boolean")
				}
				// The engine answers a refused move exactly like an accepted
				// one, so the order is read on both sides. game_id is not part
				// of the move itself; it is what makes the check possible.
				before, err := levelPosition(ctx, e, gameID, levelID)
				if err != nil {
					return nil, err
				}
				if err := e.AdminMoveLevel(ctx, levelID, up); err != nil {
					return nil, err
				}
				after, err := levelPosition(ctx, e, gameID, levelID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"done": true, "moved": before != after, "order_before": before, "order_after": after}, nil
			},
		},
		{
			name:        "admin_copy_levels",
			description: "Скопировать все уровни из source_game_id в игру game_id.",
			parameters: schema(map[string]any{
				"game_id":        intProp("Идентификатор игры"),
				"source_game_id": intProp("Идентификатор исходной игры")}, "game_id", "source_game_id"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				sourceGameID, err := requireAdminInt(args, "source_game_id")
				if err != nil {
					return nil, err
				}
				if sourceGameID < 1 {
					return nil, fmt.Errorf("source_game_id must be at least 1")
				}
				if err := e.AdminCopyLevels(ctx, gameID, sourceGameID); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_set_application",
			description: "Изменить заявку команды: статус, PIN, очки, новичок. comment отправляется капитану по электронной почте.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры"),
				"team_id": intProp("Идентификатор команды"),
				"params":  applicationParamsSchema()}, "game_id", "team_id", "params"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				teamID, err := requireAdminInt(args, "team_id")
				if err != nil {
					return nil, err
				}
				if teamID < 1 {
					return nil, fmt.Errorf("team_id must be at least 1")
				}
				var params dzzzr.ApplicationParams
				if err := decodeAdminParams(args, &params); err != nil {
					return nil, err
				}
				if params.Status != nil && (*params.Status < 0 || *params.Status > 5) {
					return nil, fmt.Errorf("status must be between 0 and 5")
				}
				if err := e.AdminSetApplication(ctx, gameID, teamID, params); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_accept_all",
			description: "Принять все ожидающие заявки игры и выдать PIN.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры")}, "game_id"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				if err := e.AdminAcceptAllApplications(ctx, gameID); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_generate_pins",
			description: "Выдать новые PIN всем принятым командам игры.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры")}, "game_id"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				if err := e.AdminGeneratePins(ctx, gameID); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_add_team",
			description: "Добавить существующую команду в игру.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры"),
				"team_id": intProp("Идентификатор команды")}, "game_id", "team_id"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				teamID, err := requireAdminInt(args, "team_id")
				if err != nil {
					return nil, err
				}
				if teamID < 1 {
					return nil, fmt.Errorf("team_id must be at least 1")
				}
				if err := e.AdminAddTeamToGame(ctx, gameID, teamID); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_remove_application",
			description: "Удалить заявку вместе с прогрессом, кодами и планом уровней команды. status должен совпадать с текущим статусом заявки.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры"),
				"team_id": intProp("Идентификатор команды"),
				"status":  map[string]any{"type": "integer", "enum": []int{0, 1, 2, 3, 4, 5}, "description": "Статус заявки: 0 ожидает, 1 принята, 2 отклонена, 3 вне зачёта, 4 автор, 5 дисквалификация"}}, "game_id", "team_id", "status"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				teamID, err := requireAdminInt(args, "team_id")
				if err != nil {
					return nil, err
				}
				if teamID < 1 {
					return nil, fmt.Errorf("team_id must be at least 1")
				}
				status, err := requireAdminInt(args, "status")
				if err != nil {
					return nil, err
				}
				if status < 0 {
					return nil, fmt.Errorf("status must be at least 0")
				}
				if status > 5 {
					return nil, fmt.Errorf("status must be between 0 and 5")
				}
				if err := e.AdminRemoveApplication(ctx, gameID, teamID, status); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_create_team",
			description: "Зарегистрировать новую команду с указанным логином капитана. Возвращает ID, если движок его сообщил.",
			parameters: schema(map[string]any{
				"name":    strProp("Название команды"),
				"captain": strProp("Логин капитана")}, "name", "captain"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				name, err := args.requireString("name")
				if err != nil {
					return nil, err
				}
				captain, err := args.requireString("captain")
				if err != nil {
					return nil, err
				}
				id, err := e.AdminCreateTeam(ctx, name, captain)
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": id}, nil
			},
		},
		{
			name:        "admin_accept_level",
			description: "Засчитать команде уровень целиком, заполнив все коды.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры"),
				"team_id": intProp("Идентификатор команды"),
				"level":   intProp("Номер уровня"),
				"time":    strProp("YYYY-MM-DD HH:MM:SS; если необязательно — по умолчанию сейчас")}, "game_id", "team_id", "level"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				teamID, err := requireAdminInt(args, "team_id")
				if err != nil {
					return nil, err
				}
				if teamID < 1 {
					return nil, fmt.Errorf("team_id must be at least 1")
				}
				level, err := requireAdminInt(args, "level")
				if err != nil {
					return nil, err
				}
				if level < 1 {
					return nil, fmt.Errorf("level must be at least 1")
				}
				time, ok := args.optionalString("time")
				if _, supplied := args["time"]; supplied && !ok {
					return nil, fmt.Errorf("invalid time")
				}
				if err := e.AdminAcceptLevel(ctx, gameID, teamID, level, time); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_give_and_accept_level",
			description: "Выдать команде уровень и сразу засчитать его.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры"),
				"team_id": intProp("Идентификатор команды"),
				"level":   intProp("Номер уровня"),
				"time":    strProp("YYYY-MM-DD HH:MM:SS; если необязательно — по умолчанию сейчас")}, "game_id", "team_id", "level"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				teamID, err := requireAdminInt(args, "team_id")
				if err != nil {
					return nil, err
				}
				if teamID < 1 {
					return nil, fmt.Errorf("team_id must be at least 1")
				}
				level, err := requireAdminInt(args, "level")
				if err != nil {
					return nil, err
				}
				if level < 1 {
					return nil, fmt.Errorf("level must be at least 1")
				}
				time, ok := args.optionalString("time")
				if _, supplied := args["time"]; supplied && !ok {
					return nil, fmt.Errorf("invalid time")
				}
				if err := e.AdminGiveAndAcceptLevel(ctx, gameID, teamID, level, time); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_clear_level_codes",
			description: "Удалить все введённые командой коды уровня.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры"),
				"team_id": intProp("Идентификатор команды"),
				"level":   intProp("Номер уровня")}, "game_id", "team_id", "level"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				teamID, err := requireAdminInt(args, "team_id")
				if err != nil {
					return nil, err
				}
				if teamID < 1 {
					return nil, fmt.Errorf("team_id must be at least 1")
				}
				level, err := requireAdminInt(args, "level")
				if err != nil {
					return nil, err
				}
				if level < 1 {
					return nil, fmt.Errorf("level must be at least 1")
				}
				if err := e.AdminClearLevelCodes(ctx, gameID, teamID, level); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_remove_level",
			description: "Удалить уровень из прохождения команды; team_id=0 удаляет у всех команд.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры"),
				"team_id": intProp("Идентификатор команды"),
				"level":   intProp("Номер уровня")}, "game_id", "team_id", "level"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				teamID, err := requireAdminInt(args, "team_id")
				if err != nil {
					return nil, err
				}
				if teamID < 0 {
					return nil, fmt.Errorf("team_id must be at least 0")
				}
				level, err := requireAdminInt(args, "level")
				if err != nil {
					return nil, err
				}
				if level < 1 {
					return nil, fmt.Errorf("level must be at least 1")
				}
				if err := e.AdminRemoveLevel(ctx, gameID, teamID, level); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_clear_team_progress",
			description: "Удалить весь игровой лог команды.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры"),
				"team_id": intProp("Идентификатор команды")}, "game_id", "team_id"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				teamID, err := requireAdminInt(args, "team_id")
				if err != nil {
					return nil, err
				}
				if teamID < 1 {
					return nil, fmt.Errorf("team_id must be at least 1")
				}
				if err := e.AdminClearTeamProgress(ctx, gameID, teamID); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_plan_level",
			description: "Добавить уровень в конец запланированной очереди команды.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры"),
				"team_id": intProp("Идентификатор команды"),
				"level":   intProp("Номер уровня")}, "game_id", "team_id", "level"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				teamID, err := requireAdminInt(args, "team_id")
				if err != nil {
					return nil, err
				}
				if teamID < 1 {
					return nil, fmt.Errorf("team_id must be at least 1")
				}
				level, err := requireAdminInt(args, "level")
				if err != nil {
					return nil, err
				}
				if level < 1 {
					return nil, fmt.Errorf("level must be at least 1")
				}
				if err := e.AdminPlanLevel(ctx, gameID, teamID, level); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_unplan_level",
			description: "Удалить уровень из запланированной очереди по позиции order.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры"),
				"team_id": intProp("Идентификатор команды"),
				"order":   intProp("Позиция в запланированной очереди")}, "game_id", "team_id", "order"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				teamID, err := requireAdminInt(args, "team_id")
				if err != nil {
					return nil, err
				}
				if teamID < 1 {
					return nil, fmt.Errorf("team_id must be at least 1")
				}
				order, err := requireAdminInt(args, "order")
				if err != nil {
					return nil, err
				}
				if order < 1 {
					return nil, fmt.Errorf("order must be at least 1")
				}
				if err := e.AdminUnplanLevel(ctx, gameID, teamID, order); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_delete_corrections",
			description: "Удалить все бонусные либо штрафные корректировки команды.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры"),
				"team_id": intProp("Идентификатор команды"),
				"kind":    map[string]any{"type": "string", "enum": []string{"bonus", "penalty"}, "description": "bonus — бонусы, penalty — штрафы"}}, "game_id", "team_id", "kind"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				teamID, err := requireAdminInt(args, "team_id")
				if err != nil {
					return nil, err
				}
				if teamID < 1 {
					return nil, fmt.Errorf("team_id must be at least 1")
				}
				kind, err := args.requireString("kind")
				if err != nil {
					return nil, err
				}
				if kind != "bonus" && kind != "penalty" {
					return nil, fmt.Errorf("kind must be bonus or penalty")
				}
				if err := e.AdminDeleteCorrections(ctx, gameID, teamID, kind); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_toggle_engine",
			description: "Переключить состояние движка для всего проекта. Если нужное состояние известно, используйте admin_set_engine.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры")}, "game_id"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				if err := e.AdminToggleEngine(ctx, gameID); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_finish_game",
			description: "Завершить игру и очистить запланированные уровни.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры")}, "game_id"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				if err := e.AdminFinishGame(ctx, gameID); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_set_event_time",
			description: "Изменить время выдачи уровня (event=1) или последнего принятого кода (event=2).",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры"),
				"team_id": intProp("Идентификатор команды"),
				"level":   intProp("Номер уровня"),
				"event":   map[string]any{"type": "integer", "enum": []int{1, 2}, "description": "1 — выдача уровня, 2 — последний принятый код"},
				"time":    strProp("YYYY-MM-DD HH:MM:SS; если необязательно — по умолчанию сейчас")}, "game_id", "team_id", "level", "event", "time"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				teamID, err := requireAdminInt(args, "team_id")
				if err != nil {
					return nil, err
				}
				if teamID < 1 {
					return nil, fmt.Errorf("team_id must be at least 1")
				}
				level, err := requireAdminInt(args, "level")
				if err != nil {
					return nil, err
				}
				if level < 1 {
					return nil, fmt.Errorf("level must be at least 1")
				}
				event, err := requireAdminInt(args, "event")
				if err != nil {
					return nil, err
				}
				if event < 1 {
					return nil, fmt.Errorf("event must be at least 1")
				}
				if event != 1 && event != 2 {
					return nil, fmt.Errorf("event must be 1 or 2")
				}
				time, err := args.requireString("time")
				if err != nil {
					return nil, err
				}
				if err := e.AdminSetEventTime(ctx, gameID, teamID, level, event, time); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_delete_message",
			description: "Удалить сообщение организатора по его метке времени.",
			parameters: schema(map[string]any{
				"game_id":   intProp("Идентификатор игры"),
				"timestamp": strProp("Метка времени сообщения из admin_messages")}, "game_id", "timestamp"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				timestamp, err := args.requireString("timestamp")
				if err != nil {
					return nil, err
				}
				if err := e.AdminDeleteMessage(ctx, gameID, timestamp); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
		{
			name:        "admin_delete_all_messages",
			description: "Удалить все сообщения организатора в игре.",
			parameters: schema(map[string]any{
				"game_id": intProp("Идентификатор игры")}, "game_id"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := requireAdminInt(args, "game_id")
				if err != nil {
					return nil, err
				}
				if gameID < 1 {
					return nil, fmt.Errorf("game_id must be at least 1")
				}
				if err := e.AdminDeleteAllMessages(ctx, gameID); err != nil {
					return nil, err
				}
				return map[string]any{"done": true}, nil
			},
		},
	}
}

// levelPosition returns a level's position in its game, so a caller can tell
// whether a move the engine reported nothing about actually happened.
func levelPosition(ctx context.Context, e Engine, gameID, levelID int) (int, error) {
	levels, err := e.AdminListLevels(ctx, gameID)
	if err != nil {
		return 0, err
	}
	for _, l := range levels {
		if l.ID == levelID {
			return l.Order, nil
		}
	}
	return 0, fmt.Errorf("level %d is not in game %d", levelID, gameID)
}
