package dzzzr_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

func TestSiteCookieRedactedFromHAR(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("dozorSiteSession")
		if err != nil || cookie.Value != "PRIVATE_SITE_TOKEN" {
			t.Error("missing site cookie")
		}
		w.Header().Set("Set-Cookie", "dozorSiteSession=PRIVATE_COOKIE_RESPONSE")
		_, _ = w.Write(fixture(t, "archive_stat.html"))
	}), dzzzr.WithSession("PRIVATE_SITE_TOKEN"), dzzzr.WithHARRecording(true))
	if _, err := c.GetArchiveStat(t.Context(), 1563); err != nil {
		t.Fatal(err)
	}
	b, err := c.ExportHAR()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "PRIVATE_") {
		t.Fatal("HAR exposed session cookie")
	}
}
