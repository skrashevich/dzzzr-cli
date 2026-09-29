// Package gamestats reconstructs DozoR results from exported game logs.
//
// The original statistics algorithm in dozor_stats.html was authored by
// Sergey <sergey@luberg.me> Luberg and ported to Go for this package.
package gamestats

type Event struct {
	T      int64  `json:"t"`
	A      string `json:"a"`
	Team   string `json:"team"`
	Level  string `json:"level"`
	Data   string `json:"data"`
	Player string `json:"player"`
	Kind   string `json:"-"`
}

type Code struct {
	Elapsed *float64 `json:"elapsed"`
	T       int64    `json:"t"`
	Raw     string   `json:"raw"`
	Player  string   `json:"player"`
	N       string   `json:"n"`
	Count   int      `json:"count"`
}

type Override struct {
	Kind   string `json:"kind"`
	At     int64  `json:"at"`
	Logged int64  `json:"logged"`
}

type Record struct {
	Level      string           `json:"level"`
	Issued     *int64           `json:"issued"`
	ClosedAt   *int64           `json:"closedAt"`
	TimeoutAt  *int64           `json:"timeoutAt"`
	RefusedAt  *int64           `json:"refusedAt"`
	SpoilerAt  *int64           `json:"spoilerAt"`
	LastCodeAt *int64           `json:"lastCodeAt"`
	DoneAt     *int64           `json:"doneAt"`
	Override   *Override        `json:"override"`
	Rejected   *Code            `json:"rejected"`
	Codes      map[string]*Code `json:"codes"`
	Bonus      map[string]*Code `json:"bonus"`
	Spoilers   map[string]*Code `json:"spoilers"`
	SpoilerBad map[string]*Code `json:"spoilerBad"`
	HintReq    int              `json:"hintReq"`
	Order      int              `json:"order"`
	Through    bool             `json:"through"`
}

type Level struct {
	Name         string             `json:"name"`
	Num          string             `json:"num"`
	Title        string             `json:"title"`
	Family       string             `json:"family"`
	AutoKind     string             `json:"autoKind"`
	Kind         string             `json:"kind"`
	DurSrc       string             `json:"durSrc"`
	DurSrcText   string             `json:"durSrcText"`
	CodeValsText string             `json:"codeValsText"`
	Through      bool               `json:"through"`
	ReqCodes     int                `json:"reqCodes"`
	ReqThrough   int                `json:"reqThrough"`
	Dur          float64            `json:"dur"`
	AutoDur      float64            `json:"autoDur"`
	H1           float64            `json:"h1"`
	H2           float64            `json:"h2"`
	Add          float64            `json:"add"`
	Done         float64            `json:"done"`
	PerBonus     float64            `json:"perBonus"`
	PerCode      float64            `json:"perCode"`
	Cap          float64            `json:"cap"`
	HintPen      float64            `json:"hintPen"`
	AutoH1       float64            `json:"autoH1"`
	AutoH2       float64            `json:"autoH2"`
	AutoAdd      float64            `json:"autoAdd"`
	DoneBonusSec float64            `json:"doneBonusSec"`
	CodeVals     map[string]float64 `json:"codeVals"`
	Completed    int                `json:"completed"`
	Average      *float64           `json:"average"`
}

type Game struct {
	Teams        []string                      `json:"teams"`
	Levels       []Level                       `json:"levels"`
	Recs         map[string]map[string]*Record `json:"recs"`
	Plan         map[string][]string           `json:"plan"`
	PlannedTeams []string                      `json:"plannedTeams"`
	Prequel      []string                      `json:"prequel"`
	StartAt      int64                         `json:"startAt"`
	EndAt        int64                         `json:"endAt"`
	EndSrc       string                        `json:"endSrc"`
	CutAt        *int64                        `json:"cutAt"`
	NRows        int                           `json:"nRows"`
	NEvents      int                           `json:"nEvents"`
}

type Config struct {
	Game   map[string]any            `json:"game"`
	Levels map[string]map[string]any `json:"levels"`
	Teams  map[string]map[string]any `json:"teams"`
	Preset string                    `json:"preset"`
}

type Resolved struct {
	Levels       []Level `json:"levels"`
	StopAt       int64   `json:"stopAt"`
	DefaultDur   float64 `json:"defaultDur"`
	PrequelMin   float64 `json:"prequelMin"`
	NoneMode     string  `json:"noneMode"`
	StopMode     string  `json:"stopMode"`
	RejectedMode string  `json:"rejectedMode"`
}

type Correction struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

type Cell struct {
	Kind           string      `json:"kind"`
	Rec            *Record     `json:"rec"`
	St             string      `json:"st"`
	How            string      `json:"how"`
	Sec            *float64    `json:"sec"`
	CountSec       float64     `json:"countSec"`
	AddSec         float64     `json:"addSec"`
	BonusSec       float64     `json:"bonusSec"`
	End            *int64      `json:"end"`
	Issued         *int64      `json:"issued"`
	Show           bool        `json:"show"`
	Implicit       bool        `json:"implicit"`
	VirtualIssue   bool        `json:"virtualIssue"`
	LastDone       bool        `json:"lastDone"`
	Best           bool        `json:"best"`
	RejectedFix    *Correction `json:"rejectedFix"`
	NC             int         `json:"nc"`
	NB             int         `json:"nb"`
	Complete       bool        `json:"complete"`
	Configured     bool        `json:"configured"`
	SpoilerElapsed *float64    `json:"spoilerElapsed"`
	ExpiresAt      *int64      `json:"expiresAt"`
}

type Row struct {
	Team        string           `json:"team"`
	Cells       map[string]*Cell `json:"cells"`
	Clean       float64          `json:"clean"`
	Add         float64          `json:"add"`
	Thr         float64          `json:"thr"`
	LvlBonus    float64          `json:"lvlBonus"`
	Prequel     float64          `json:"prequel"`
	Penalty     float64          `json:"penalty"`
	ManualBonus float64          `json:"manualBonus"`
	BonusOther  float64          `json:"bonusOther"`
	Total       float64          `json:"total"`
	Place       int              `json:"place"`
	PlaceClean  int              `json:"placeClean"`
	GapLeader   *float64         `json:"gapLeader"`
	GapPrev     *float64         `json:"gapPrev"`
}

type Result struct {
	R    Resolved `json:"R"`
	Rows []*Row   `json:"rows"`
}

type Report struct {
	Game   *Game  `json:"game"`
	Config Config `json:"cfg"`
	Result Result `json:"res"`
	Key    string `json:"key"`
}
