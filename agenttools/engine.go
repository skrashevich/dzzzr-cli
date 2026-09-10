package agenttools

import (
	"context"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// Engine is the slice of the client the tools need. Declaring it here keeps
// the catalog testable and states exactly which engine calls an agent can
// reach.
type Engine interface {
	GetGame(ctx context.Context) (*dzzzr.GameState, error)
	GetLevelInfo(ctx context.Context) (*dzzzr.LevelInfo, error)
	GetBonusLevelInfo(ctx context.Context) (*dzzzr.LevelInfo, error)
	GetStat(ctx context.Context) (*dzzzr.TeamStat, error)
	GetLog(ctx context.Context) ([]dzzzr.LogEntry, error)
	GetMessages(ctx context.Context, after string) ([]dzzzr.ChatMessage, error)
	GetGamesList(ctx context.Context, opts dzzzr.GamesListOptions) ([]dzzzr.GameInfo, error)
	GetArchiveStat(ctx context.Context, gameID int) (*dzzzr.ArchiveStat, error)
	GetArchiveDescription(ctx context.Context, gameID int) (*dzzzr.ArchiveDescription, error)
	GetGameLog(ctx context.Context, gameID int) (*dzzzr.GameLog, error)

	SendCode(ctx context.Context, code string) (*dzzzr.ActionResult, error)
	SendBonusCode(ctx context.Context, levelNumber int, code string) (*dzzzr.ActionResult, error)
	SendSpoilerCode(ctx context.Context, level int, code string) (*dzzzr.ActionResult, error)
	TakeHint(ctx context.Context, level, hint int) (*dzzzr.ActionResult, error)
	TakeHintEarly(ctx context.Context, level, hint int) (*dzzzr.ActionResult, error)
	Abandon(ctx context.Context) (*dzzzr.ActionResult, error)
	TakeBreak(ctx context.Context) (*dzzzr.ActionResult, error)
	StopBreak(ctx context.Context) (*dzzzr.ActionResult, error)
	NextLevel(ctx context.Context, level int) (*dzzzr.ActionResult, error)
	SelectLevel(ctx context.Context, current, next int) (*dzzzr.ActionResult, error)
	PostMessage(ctx context.Context, text string) error
	SendMessageToOrg(ctx context.Context, text string) (*dzzzr.ActionResult, error)

	HasAdminCredentials() bool
	AdminListGames(ctx context.Context) ([]dzzzr.AdminGame, error)
	AdminGetGame(ctx context.Context, gameID int) (*dzzzr.AdminGameInfo, error)
	AdminListLevels(ctx context.Context, gameID int) ([]dzzzr.AdminLevel, error)
	AdminGetLevel(ctx context.Context, gameID, levelID int) (*dzzzr.AdminLevelInfo, error)
	AdminListTeams(ctx context.Context, gameID int) ([]dzzzr.AdminTeam, error)
	AdminMonitor(ctx context.Context, gameID int) (*dzzzr.MonitorState, error)
	AdminGetLog(ctx context.Context, gameID int, since string) ([]dzzzr.AdminLogEntry, string, error)
	AdminListMessages(ctx context.Context, gameID, teamID int) ([]dzzzr.AdminMessage, error)

	AdminGiveLevel(ctx context.Context, gameID, teamID, level int, at string) error
	AdminAcceptCode(ctx context.Context, gameID, teamID, level int, code, at string) error
	AdminAddCorrection(ctx context.Context, gameID, teamID int, kind string, minutes int, comment string) error
	AdminBlockTeam(ctx context.Context, gameID, teamID int) error
	AdminUnblockTeam(ctx context.Context, gameID, teamID int) error
	AdminSetEngine(ctx context.Context, gameID int, running bool) error
	AdminSendMessage(ctx context.Context, gameID, teamID int, text, showAfter string, important bool) error
	AdminGetTeam(ctx context.Context, gameID int, teamID int) (*dzzzr.AdminTeamInfo, error)
	AdminCreateGame(ctx context.Context, params dzzzr.GameParams) (int, error)
	AdminUpdateGame(ctx context.Context, gameID int, params dzzzr.GameParams) error
	AdminDeleteGame(ctx context.Context, gameID int) error
	AdminCopyGame(ctx context.Context, sourceGameID int, withLevels bool) (int, error)
	AdminCreateTechnicalLevel(ctx context.Context, gameID int) (int, error)
	AdminUploadFile(ctx context.Context, gameID int, filename string, data []byte) (*dzzzr.AdminFileUpload, error)
	AdminReplaceLevel(ctx context.Context, gameID int, levelID int, params dzzzr.LevelParams) error
	AdminCreateLevel(ctx context.Context, gameID int, params dzzzr.LevelParams) (int, error)
	AdminUpdateLevel(ctx context.Context, gameID int, levelID int, params dzzzr.LevelParams) error
	AdminDeleteLevel(ctx context.Context, gameID int, levelID int) error
	AdminMoveLevel(ctx context.Context, levelID int, up bool) error
	AdminListCityTeams(ctx context.Context, gameID int) ([]dzzzr.CityTeam, error)
	AdminCopyLevels(ctx context.Context, gameID int, sourceGameID int) error
	AdminSetApplication(ctx context.Context, gameID int, teamID int, params dzzzr.ApplicationParams) error
	AdminAcceptAllApplications(ctx context.Context, gameID int) error
	AdminGeneratePins(ctx context.Context, gameID int) error
	AdminAddTeamToGame(ctx context.Context, gameID int, teamID int) error
	AdminRemoveApplication(ctx context.Context, gameID int, teamID int, status int) error
	AdminCreateTeam(ctx context.Context, name string, captain string) (int, error)
	AdminAcceptLevel(ctx context.Context, gameID int, teamID int, level int, time string) error
	AdminGiveAndAcceptLevel(ctx context.Context, gameID int, teamID int, level int, time string) error
	AdminClearLevelCodes(ctx context.Context, gameID int, teamID int, level int) error
	AdminRemoveLevel(ctx context.Context, gameID int, teamID int, level int) error
	AdminClearTeamProgress(ctx context.Context, gameID int, teamID int) error
	AdminPlanLevel(ctx context.Context, gameID int, teamID int, level int) error
	AdminUnplanLevel(ctx context.Context, gameID int, teamID int, order int) error
	AdminDeleteCorrections(ctx context.Context, gameID int, teamID int, kind string) error
	AdminToggleEngine(ctx context.Context, gameID int) error
	AdminFinishGame(ctx context.Context, gameID int) error
	AdminSetEventTime(ctx context.Context, gameID int, teamID int, level int, event int, time string) error
	AdminDeleteMessage(ctx context.Context, gameID int, timestamp string) error
	AdminDeleteAllMessages(ctx context.Context, gameID int) error
}

var _ Engine = (*dzzzr.Client)(nil)
