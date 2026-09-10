package dzzzr_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func logClient(t *testing.T, body string, seen **http.Request) *dzzzr.Client {
	t.Helper()
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = r
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}), dzzzr.WithSession("TOKEN"))
	return c
}

func TestGetGameLogKeepsEngineFieldNames(t *testing.T) {
	// The export has no documented schema, so the client must not depend on
	// one: whatever fields the engine sends are what the caller gets, in the
	// order the engine listed them.
	for _, tc := range []struct{ name, body string }{
		{"голый массив", `[{"time":"21:03:01","team":"Rising","level":"1","event":"уровень выдан","code":""},
			{"time":"21:06:02","team":"Rising","level":1,"event":"принят код","code":"ЯВСЕВИЖУ"}]`},
		{"в обёртке", `{"gmid":1563,"log":[{"time":"21:03:01","team":"Rising","level":"1","event":"уровень выдан","code":""},
			{"time":"21:06:02","team":"Rising","level":1,"event":"принят код","code":"ЯВСЕВИЖУ"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seen *http.Request
			c := logClient(t, tc.body, &seen)
			log, err := c.GetGameLog(context.Background(), 1563)
			if err != nil {
				t.Fatal(err)
			}
			if seen.URL.Path != "/moscow/gameLogJSON.php" || seen.URL.Query().Get("gmid") != "1563" || seen.URL.Query().Get("s") != "TOKEN" {
				t.Errorf("request = %s", seen.URL)
			}
			want := []string{"time", "team", "level", "event", "code"}
			if len(log.Columns) != len(want) {
				t.Fatalf("columns = %q", log.Columns)
			}
			for i, w := range want {
				if log.Columns[i] != w {
					t.Errorf("column %d = %q, want %q", i, log.Columns[i], w)
				}
			}
			if len(log.Entries) != 2 || log.GameID != 1563 {
				t.Fatalf("entries = %+v", log.Entries)
			}
			// The engine types the same column as a string in one record and a
			// number in the next; both read as the same text.
			if log.Entries[0]["level"] != "1" || log.Entries[1]["level"] != "1" {
				t.Errorf("level = %q / %q", log.Entries[0]["level"], log.Entries[1]["level"])
			}
			if log.Entries[1]["code"] != "ЯВСЕВИЖУ" || log.Entries[0]["code"] != "" {
				t.Errorf("codes = %+v", log.Entries)
			}
		})
	}
}

func TestGetGameLogEmptyBodyMeansNoRights(t *testing.T) {
	// The handler prints nothing at all rather than an error when it will not
	// show the log, which is how a visitor without the rights leaves.
	var seen *http.Request
	c := logClient(t, "", &seen)
	_, err := c.GetGameLog(context.Background(), 1563)
	if !dzzzr.IsEngineError(err) {
		t.Fatalf("err = %v", err)
	}
	var e *dzzzr.EngineError
	if !asErr(err, &e) || e.Code != dzzzr.ErrNoPermission {
		t.Fatalf("err = %v", err)
	}
}

func TestGetGameLogSitePageIsReportedAsRefusal(t *testing.T) {
	var seen *http.Request
	c := logClient(t,
		"<html><body><!-- CONTENT -->\n\t\tТребуется авторизация.\t\t<!-- CONTENT END --></body></html>", &seen)
	_, err := c.GetGameLog(context.Background(), 1563)
	if dzzzr.AuthErrorKindOf(err) != dzzzr.AuthSession {
		t.Fatalf("err = %v", err)
	}
}

func TestGetGameLogWithoutSessionFails(t *testing.T) {
	var seen *http.Request
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		_, _ = w.Write([]byte("[]"))
	}))
	if _, err := c.GetGameLog(context.Background(), 1563); dzzzr.AuthErrorKindOf(err) != dzzzr.AuthSession {
		t.Fatalf("err = %v", err)
	}
	if seen != nil {
		t.Error("request sent without a session")
	}
}

func TestGetGameLogRejectsUnexpectedPayload(t *testing.T) {
	var seen *http.Request
	c := logClient(t, `{"gmid":1563}`, &seen)
	if _, err := c.GetGameLog(context.Background(), 1563); !dzzzr.IsUndecodable(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestGameLogSpreadsheetRows(t *testing.T) {
	var seen *http.Request
	c := logClient(t, `{"10":["later","event",null,"level","code","user"],"2":["earlier","event","team","level",null,null],"1":["Время","Действие","Команда","Уровень","Данные","Данные"]}`, &seen)
	log, err := c.GetGameLog(t.Context(), 1563)
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := seen.Cookie("dozorSiteSession")
	if err != nil || cookie.Value != "TOKEN" {
		t.Fatal("missing site cookie")
	}
	if len(log.Entries) != 2 || log.Entries[0]["Время"] != "earlier" || log.Entries[1]["Данные"] != "code" || log.Entries[1]["Данные (2)"] != "user" || log.Entries[0]["Данные"] != "" {
		t.Fatalf("log: %+v", log)
	}
}

func TestGameLogSpreadsheetRejectsWrongWidth(t *testing.T) {
	var seen *http.Request
	c := logClient(t, `{"1":["Time","Data"],"2":["time","data","lost"]}`, &seen)
	if _, err := c.GetGameLog(t.Context(), 1); !dzzzr.IsUndecodable(err) {
		t.Fatalf("error: %v", err)
	}
}
