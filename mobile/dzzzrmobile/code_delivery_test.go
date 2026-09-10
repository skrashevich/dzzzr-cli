package dzzzrmobile_test

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/skrashevich/dzzzr-cli/mobile/dzzzrmobile"
)

func TestCodeDeliveryCertainty(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		close   bool
		notSent bool
	}{
		{"dial failed", 0, true, true},
		{"server error is ambiguous", 502, false, false},
		{"bad reply is ambiguous", 200, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status) }))
			defer server.Close()
			client := dzzzrmobile.NewClientWithOptions("moscow", server.URL+"/moscow/", false, true, 2)
			client.SetSession("TOKEN")
			client.SetCredentials("demo", "pin")
			if test.close {
				server.Close()
			}
			raw, err := client.SendCodeView("answer", 1, "main")
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Error   string `json:"error"`
				NotSent bool   `json:"notSent"`
			}
			if err := json.Unmarshal([]byte(raw), &result); err != nil {
				t.Fatal(err)
			}
			if result.Error == "" || result.NotSent != test.notSent {
				t.Fatalf("unexpected delivery view: %s", raw)
			}
		})
	}
}
