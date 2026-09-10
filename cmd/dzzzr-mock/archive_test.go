package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func TestArchiveThroughClient(t *testing.T) {
	srv, mock := startMock(t)
	public := dzzzr.New("moscow", dzzzr.WithBaseURL(srv.URL+"/moscow/"))
	stat, err := public.GetArchiveStat(t.Context(), 4242)
	if err != nil {
		t.Fatal(err)
	}
	if len(stat.Teams) != 1 || stat.Teams[0].Name != "MockTeam" || len(stat.Columns) != len(mock.state.Levels)+5 {
		t.Fatalf("stat: %+v", stat)
	}
	desc, err := public.GetArchiveDescription(t.Context(), 4242)
	if err != nil || !desc.Restricted || len(desc.Levels) != 0 {
		t.Fatalf("public description: %+v, %v", desc, err)
	}
	captain := signIn(t, srv, "demo")
	desc, err = captain.GetArchiveDescription(t.Context(), 4242)
	if err != nil || desc.Restricted || len(desc.Levels) != len(mock.state.Levels) {
		t.Fatalf("captain description: %+v, %v", desc, err)
	}
	if desc.Levels[0].Task == "" || len(desc.Levels[0].Codes) == 0 {
		t.Fatalf("missing scenario content: %+v", desc.Levels[0])
	}
	before, err := captain.GetGameLog(t.Context(), 4242)
	if err != nil {
		t.Fatal(err)
	}
	send(t, captain, "WRONG-ARCHIVE-CODE")
	after, err := captain.GetGameLog(t.Context(), 4242)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Entries) <= len(before.Entries) || after.Columns[5] != "Данные (2)" {
		t.Fatalf("log: %+v", after)
	}
	last := after.Entries[len(after.Entries)-1]
	if !strings.Contains(last["Данные"], "WRONG-ARCHIVE-CODE") || last["Данные (2)"] != "demo" {
		t.Fatalf("event: %+v", last)
	}
	reader := signIn(t, srv, "reader")
	if _, err := reader.GetGameLog(t.Context(), 4242); !dzzzr.IsEngineError(err) {
		t.Fatalf("reader access: %v", err)
	}
	if _, err := captain.GetArchiveStat(t.Context(), 999); err == nil {
		t.Fatal("unknown game accepted")
	}
}

func TestArchiveLogRequiresCookie(t *testing.T) {
	srv, mock := startMock(t)
	c := signIn(t, srv, "demo")
	for _, cookie := range []string{"", "invalid", c.Session()} {
		req := httptest.NewRequest(http.MethodGet, "/moscow/gameLogJSON.php?gmid=4242&s="+c.Session(), nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "dozorSiteSession", Value: cookie})
		}
		rec := httptest.NewRecorder()
		mock.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatal(rec.Code)
		}
		if (rec.Body.Len() > 0) != (cookie == c.Session()) {
			t.Fatalf("unexpected access result: %d bytes", rec.Body.Len())
		}
	}
}
