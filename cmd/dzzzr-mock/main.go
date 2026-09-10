// Command dzzzr-mock serves a small imitation of the Dozor Classic engine.
//
// It answers the same endpoints the real engine does — API/login.php, go/,
// API/game.php, API/messages.php, API/postmessage.php, API/gamesList.php —
// with one fixed game, one team and one captain, so the dzzzr client library
// and the dzzzr CLI can be exercised end to end without touching the network.
//
// The engine's behavior is reproduced from the sources of Dozor Classic:
// go/go2.php for the actions and their result codes, API/*.php for the JSON
// shapes, templates/JSON/go.tpl and go_level.tpl for the game state.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// version is stamped at build time with -ldflags "-X main.version=...".
var version = "dev"

// PZDC, the "lost network" code, holds the connection for networkDownDuration
// and then answers every request of that session with 502 until the outage is
// over; each of those requests waits networkDownSleep first. Tests shorten
// both.
var (
	networkDownDuration = 60 * time.Second
	networkDownSleep    = 2 * time.Second
)

// defaultAddr is the listen address used when neither the flag nor
// DZZZR_MOCK_ADDR says otherwise.
const defaultAddr = "0.0.0.0:18090"

// session is a signed-in site account, the equivalent of a row of usersSite
// with a session token.
type session struct {
	Token     string
	Login     string
	CreatedAt time.Time
	// NetDown is when the simulated network outage of this session ends.
	NetDown time.Time
}

// server holds the whole mock: one city, one game, the signed-in sessions.
// Every handler takes the mutex, so the state stays consistent under the
// parallel requests a CLI or a test may fire.
type server struct {
	city   string
	logger *log.Logger
	quirks quirkSet

	mu       sync.Mutex
	state    *gameState
	sessions map[string]*session
}

// newServer creates a mock for one city with a freshly built game.
func newServer(city string, logger *log.Logger, quirks ...quirk) *server {
	if logger == nil {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	set := quirkSet{}
	for _, q := range quirks {
		set[q] = true
	}
	s := &server{
		city:     city,
		logger:   logger,
		quirks:   set,
		state:    newGameState(time.Now()),
		sessions: map[string]*session{},
	}
	s.applyQuirks()
	return s
}

// applyQuirks bends the freshly built game into the shape the quirks ask for.
// The caller holds no lock: newServer and reset are the only callers and
// neither has published the state yet.
func (s *server) applyQuirks() {
	if s.has(quirkSkvozCurrent) {
		// Make the first main level сквозное, which the fixed scenario alone
		// can never produce: mainLine skips every bonus level, so the only
		// сквозное task never reaches the main slot.
		// Skvoz alone is what makes the template append its separator.
		// BonusTime is the minutes the task is worth, so the reply carries a
		// non-zero bonusLevelTime for a client to show; SkvozMin would be
		// the submission deadline and has no business here.
		if line := s.state.mainLine(); len(line) > 0 {
			line[0].Skvoz = true
			line[0].ShowAsBonus = true
			line[0].BonusTime = 10
		}
	}
	if s.has(quirkLevelFinished) {
		// bonusAfter -1 hands the next level over the moment the main codes
		// are in, so the level is never both finished and current. A
		// non-negative value is what the engine uses for a game that allows
		// bonus hunting afterwards.
		s.state.BonusAfter = 3
	}
}

// reset puts the game back to the state a fresh process would serve.
func (s *server) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = newGameState(time.Now())
	s.sessions = map[string]*session{}
	s.applyQuirks()
}

// adminMounts collects the route registrations contributed by the organizer
// half of the mock. It is empty until admin.go is part of the build, so the
// player half compiles and runs on its own; admin.go appends to it from an
// init function and its routes land under /{city}/admin/.
var adminMounts []func(s *server, mux *http.ServeMux, prefix string)

// Handler builds the routing table of the mock.
func (s *server) Handler() http.Handler {
	mux := http.NewServeMux()
	prefix := "/" + s.city + "/"
	s.registerPlayer(mux, prefix)
	s.registerArchive(mux, prefix)
	for _, mount := range adminMounts {
		mount(s, mux, prefix)
	}
	return s.logRequests(mux)
}

// statusRecorder remembers the status code so the access log can print it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// logRequests writes one line per request to the server's log.
func (s *server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		action := ""
		if r.Method == http.MethodPost {
			if v := r.PostFormValue("action"); v != "" {
				action = " action=" + v
			}
		}
		s.logger.Printf("%s %s%s → %d (%s)", r.Method, r.URL.RequestURI(), action, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

func main() {
	addr := flag.String("addr", envOr("DZZZR_MOCK_ADDR", defaultAddr), "listen address")
	city := flag.String("city", "moscow", "city segment the mock serves")
	quirkList := flag.String("quirks", envOr("DZZZR_MOCK_QUIRKS", ""),
		"comma-separated engine quirks to reproduce ("+quirkNames()+"), env DZZZR_MOCK_QUIRKS")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("dzzzr-mock", version)
		return
	}

	logger := log.New(os.Stderr, "dzzzr-mock ", log.LstdFlags)
	quirks, err := parseQuirks(*quirkList)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dzzzr-mock:", err)
		os.Exit(2)
	}
	srv := newServer(*city, logger, quirks...)

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Actions may hold the connection for a simulated network outage,
		// so the write timeout has to outlast it.
		WriteTimeout: 5 * time.Minute,
	}

	logger.Printf("version %s, listening on %s, city %q, base URL http://%s/%s/", version, *addr, *city, *addr, *city)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("listen: %v", err)
	}
}

// envOr returns the environment variable or the fallback when it is unset.
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
