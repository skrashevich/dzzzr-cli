package dzzzr_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// The type documents itself as safe for concurrent use, and the mobile
// bindings change settings from one thread while another is mid-request. This
// runs both at once so -race has something to look at.
func TestClientSettersAreSafeDuringRequests(t *testing.T) {
	c := authedClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"games" : null}`))
	}))
	c.SetAdminCredentials("org", "secret")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				if _, err := c.GetGamesList(ctx, dzzzr.GamesListOptions{}); err != nil {
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 25 {
			c.SetUserAgent("dzzzr-test/1")
			_ = c.UserAgent()
			c.SetHARRecordingEnabled(i%2 == 0)
			if _, err := c.ExportHAR(); err != nil {
				t.Error(err)
				return
			}
			c.SetAdminDelay(time.Duration(i) * time.Millisecond)
			_ = c.AdminDelay()
			c.SetCredentials(dzzzr.Credentials{Captain: "Cap", Pin: "1234"})
			_ = c.Credentials()
			c.SetSession("TOKEN")
			_ = c.Session()
		}
	}()
	wg.Wait()
}
