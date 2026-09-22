package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/auth"
)

// isolateCodexAuth is isolateLLMEnv for the sign-in tests: it also forgets the
// token stores earlier tests built and returns where the sign-in is stored.
func isolateCodexAuth(t *testing.T) string {
	t.Helper()
	isolateLLMEnv(t)
	resetCodexTokenStores()
	t.Cleanup(resetCodexTokenStores)
	path := mustCodexAuthFile(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustCodexAuthFile(t *testing.T) string {
	t.Helper()
	path, err := codexAuthFile()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTestCodexCredential(t *testing.T, cred *codexCredential) {
	t.Helper()
	if err := saveCodexCredential(cred); err != nil {
		t.Fatalf("saveCodexCredential: %v", err)
	}
}

func TestCodexCredentialRoundTrip(t *testing.T) {
	path := isolateCodexAuth(t)
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	writeTestCodexCredential(t, &codexCredential{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		ExpiresAt:    expires,
	})

	checkFileMode(t, path, 0o600)

	got, err := loadCodexCredential()
	if err != nil {
		t.Fatalf("loadCodexCredential: %v", err)
	}
	if got.AccessToken != "access-1" || got.RefreshToken != "refresh-1" || got.AccountID != "acct-1" {
		t.Fatalf("credential = %+v", got)
	}
	if !got.ExpiresAt.Equal(expires) {
		t.Fatalf("credential = %+v, want expiry %v", got, expires)
	}
	if !hasCodexCredential() {
		t.Fatal("hasCodexCredential = false after a successful save")
	}

	if err := deleteCodexCredential(); err != nil {
		t.Fatalf("deleteCodexCredential: %v", err)
	}
	if hasCodexCredential() {
		t.Fatal("hasCodexCredential = true after delete")
	}
	// Removing an absent credential is what codex-logout does on a clean machine.
	if err := deleteCodexCredential(); err != nil {
		t.Fatalf("deleteCodexCredential on a missing file: %v", err)
	}
}

// os.WriteFile applies its mode only when it creates the file, so a credential
// written over an existing world-readable file would stay world-readable.
func TestSaveCodexCredentialTightensAnExistingFile(t *testing.T) {
	path := isolateCodexAuth(t)
	if err := os.WriteFile(path, []byte(`{"access_token":"old"}`), 0644); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	writeTestCodexCredential(t, &codexCredential{AccessToken: "new"})

	checkFileMode(t, path, 0o600)
	got, err := loadCodexCredential()
	if err != nil {
		t.Fatalf("loadCodexCredential: %v", err)
	}
	if got.AccessToken != "new" {
		t.Fatalf("access token = %q", got.AccessToken)
	}
	// The atomic write must not leave its temp file behind.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want only the credential", len(entries))
	}
}

// The web interface runs chat turns concurrently; a store per turn would let two of them
// rotate the same refresh token and log the user out.
func TestCodexProvidersShareOneTokenStore(t *testing.T) {
	isolateCodexAuth(t)
	writeTestCodexCredential(t, &codexCredential{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
	})

	if _, err := newCodexProvider(); err != nil {
		t.Fatalf("first newCodexProvider: %v", err)
	}
	if _, err := newCodexProvider(); err != nil {
		t.Fatalf("second newCodexProvider: %v", err)
	}
	if len(codexStores) != 1 {
		t.Fatalf("token stores = %d, want one per credential file", len(codexStores))
	}

	// A token refreshed by one provider must be visible to the next one built,
	// instead of being re-read stale from disk.
	store := codexStores[mustCodexAuthFile(t)]
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		return &auth.AuthCredential{AccessToken: "fresh", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	store.persist = func(*codexCredential) error { return nil }
	store.cred.ExpiresAt = time.Now().Add(time.Minute)
	if _, _, err := store.tokenSource()(); err != nil {
		t.Fatalf("tokenSource: %v", err)
	}
	if token, _ := store.snapshot(); token != "fresh" {
		t.Fatalf("snapshot token = %q, want the refreshed one", token)
	}
}

// The point of sharing the store is that concurrent web turns produce exactly
// one rotation of the refresh token, not one per turn.
func TestConcurrentTurnsRefreshTheTokenOnce(t *testing.T) {
	isolateCodexAuth(t)
	writeTestCodexCredential(t, &codexCredential{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		ExpiresAt:    time.Now().Add(time.Minute),
	})

	var refreshes atomic.Int64
	store := sharedCodexTokenStore(mustCodexAuthFile(t), &codexCredential{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		ExpiresAt:    time.Now().Add(time.Minute),
	})
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		refreshes.Add(1)
		return &auth.AuthCredential{
			AccessToken:  "fresh",
			RefreshToken: "refresh-2",
			ExpiresAt:    time.Now().Add(time.Hour),
		}, nil
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			provider, err := newCodexProvider()
			if err != nil {
				t.Errorf("newCodexProvider: %v", err)
				return
			}
			_ = provider
			token, _, err := store.tokenSource()()
			if err != nil {
				t.Errorf("tokenSource: %v", err)
				return
			}
			if token != "fresh" {
				t.Errorf("token = %q, want the refreshed one", token)
			}
		}()
	}
	wg.Wait()

	if got := refreshes.Load(); got != 1 {
		t.Fatalf("refreshes = %d, want exactly 1 across concurrent turns", got)
	}
}

func TestLoadCodexCredentialReportsMissingSignIn(t *testing.T) {
	isolateCodexAuth(t)
	_, err := loadCodexCredential()
	if !errors.Is(err, errNoCodexCredential) {
		t.Fatalf("error = %v, want errNoCodexCredential", err)
	}
}

func TestLoadCodexCredentialRejectsEmptyAccessToken(t *testing.T) {
	path := isolateCodexAuth(t)
	if err := os.WriteFile(path, []byte(`{"refresh_token":"r"}`), 0600); err != nil {
		t.Fatalf("write credential: %v", err)
	}
	_, err := loadCodexCredential()
	if err == nil || !strings.Contains(err.Error(), "ChatGPT") {
		t.Fatalf("error = %v, want a re-sign-in hint", err)
	}
}

func TestCodexTokenStoreKeepsAValidToken(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		ExpiresAt:    time.Now().Add(time.Hour),
	})
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		t.Fatal("refresh must not run for a token that is not near expiry")
		return nil, nil
	}
	store.persist = func(*codexCredential) error { return nil }

	token, accountID, err := store.tokenSource()()
	if err != nil {
		t.Fatalf("tokenSource: %v", err)
	}
	if token != "access-1" || accountID != "acct-1" {
		t.Fatalf("token = %q, account = %q", token, accountID)
	}
}

func TestCodexTokenStoreRefreshesAndPersists(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		// Inside codexTokenRefreshSkew, so the token must be renewed before use.
		ExpiresAt: time.Now().Add(time.Minute),
	})
	refreshes := 0
	var sawRefreshToken string
	store.refresh = func(current *auth.AuthCredential) (*auth.AuthCredential, error) {
		// Runs on the goroutine refreshBounded starts, so it must not call t.Fatal.
		refreshes++
		sawRefreshToken = current.RefreshToken
		// A renewal that omits the account ID must not lose it.
		return &auth.AuthCredential{
			AccessToken:  "fresh",
			RefreshToken: "refresh-2",
			ExpiresAt:    time.Now().Add(time.Hour),
		}, nil
	}
	var persisted []*codexCredential
	store.persist = func(cred *codexCredential) error {
		persisted = append(persisted, cred)
		return nil
	}

	source := store.tokenSource()
	token, accountID, err := source()
	if err != nil {
		t.Fatalf("tokenSource: %v", err)
	}
	if token != "fresh" || accountID != "acct-1" {
		t.Fatalf("token = %q, account = %q", token, accountID)
	}
	if sawRefreshToken != "refresh-1" {
		t.Fatalf("refresh was called with token %q", sawRefreshToken)
	}
	if len(persisted) != 1 || persisted[0].AccessToken != "fresh" || persisted[0].RefreshToken != "refresh-2" {
		t.Fatalf("persisted = %+v", persisted)
	}

	// The renewed token is far from expiry, so the next call must reuse it.
	if _, _, err := source(); err != nil {
		t.Fatalf("second tokenSource: %v", err)
	}
	if refreshes != 1 {
		t.Fatalf("refreshes = %d, want 1", refreshes)
	}
}

func TestCodexTokenStoreRefreshesOnceWithoutExpiry(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "unknown-age",
		RefreshToken: "refresh-1",
	})
	refreshes := 0
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		refreshes++
		return &auth.AuthCredential{AccessToken: "fresh", RefreshToken: "refresh-2"}, nil
	}
	store.persist = func(*codexCredential) error { return nil }

	source := store.tokenSource()
	for range 3 {
		if _, _, err := source(); err != nil {
			t.Fatalf("tokenSource: %v", err)
		}
	}
	if refreshes != 1 {
		t.Fatalf("refreshes = %d, want exactly 1 for a credential without an expiry", refreshes)
	}
}

// A failed renewal rotates nothing, so it must not permanently disable renewal
// for a long-lived «dzzzr chat» or web process — but it must not be retried on every
// turn either, since the issuer is dialled under the store mutex.
func TestCodexTokenStoreThrottlesRetriesAfterAFailedRefresh(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "unknown-age",
		RefreshToken: "refresh-1",
	})
	refreshes := 0
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		refreshes++
		return nil, errors.New("issuer unreachable")
	}
	store.persist = func(*codexCredential) error { return nil }

	source := store.tokenSource()
	if _, _, err := source(); err == nil {
		t.Fatal("the first call must surface the refresh failure")
	}
	// Within the cooldown the run continues on the token already in hand.
	token, _, err := source()
	if err != nil {
		t.Fatalf("second tokenSource: %v", err)
	}
	if token != "unknown-age" {
		t.Fatalf("token = %q, want the credential we still hold", token)
	}
	if refreshes != 1 {
		t.Fatalf("refreshes = %d, want the retry throttled inside the cooldown", refreshes)
	}

	// Once the cooldown lapses the renewal re-arms: a transient failure must not
	// disable it for the life of the process.
	store.mu.Lock()
	store.lastRefreshAttempt = time.Now().Add(-codexRefreshRetryCooldown - time.Second)
	store.mu.Unlock()
	if _, _, err := source(); err == nil {
		t.Fatal("the re-armed attempt must surface the failure again")
	}
	if refreshes != 2 {
		t.Fatalf("refreshes = %d, want a retry after the cooldown lapsed", refreshes)
	}

	// A success closes it for good, so the refresh token is not rotated per turn.
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		refreshes++
		return &auth.AuthCredential{AccessToken: "fresh", RefreshToken: "refresh-2"}, nil
	}
	store.mu.Lock()
	store.lastRefreshAttempt = time.Now().Add(-codexRefreshRetryCooldown - time.Second)
	store.mu.Unlock()
	if _, _, err := source(); err != nil {
		t.Fatalf("successful refresh: %v", err)
	}
	for range 3 {
		if _, _, err := source(); err != nil {
			t.Fatalf("after success: %v", err)
		}
	}
	if refreshes != 3 {
		t.Fatalf("refreshes = %d, want no further renewal after a successful one", refreshes)
	}
}

// Giving up on the wait must not give up the exclusion: the abandoned attempt
// still holds the only rotation in flight. Without the latch, each later turn
// starts another renewal with the same refresh token, the issuer rotates it out
// from under the earlier ones, and the last writer persists a credential the
// issuer has already invalidated.
func TestStalledRefreshDoesNotStackConcurrentRenewals(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Hour), // the ordinary expiry path
	})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	var started, peak atomic.Int64
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		running := started.Add(1)
		for {
			was := peak.Load()
			if running <= was || peak.CompareAndSwap(was, running) {
				break
			}
		}
		<-release
		started.Add(-1)
		return nil, errors.New("unblocked after the test")
	}
	store.persist = func(*codexCredential) error { return nil }

	original := codexRefreshTimeout
	codexRefreshTimeout = 30 * time.Millisecond
	t.Cleanup(func() { codexRefreshTimeout = original })

	source := store.tokenSource()
	for turn := range 4 {
		if _, _, err := source(); err == nil {
			t.Fatalf("turn %d: a stalled renewal must not report success", turn+1)
		}
	}

	if got := peak.Load(); got != 1 {
		t.Fatalf("concurrent renewals = %d, want exactly 1 across four stalled turns", got)
	}
}

// The ordinary expiry path has no cooldown, so a failed renewal must stay
// retryable on the next turn — but strictly one at a time, and many callers
// arriving together must share a single attempt rather than each opening one.
func TestExpiredCredentialRetriesSeriallyUnderLoad(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})
	var attempts, peak atomic.Int64
	fail := true
	var failMu sync.Mutex
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		running := attempts.Add(1)
		for {
			was := peak.Load()
			if running <= was || peak.CompareAndSwap(was, running) {
				break
			}
		}
		defer attempts.Add(-1)
		time.Sleep(time.Millisecond)
		failMu.Lock()
		defer failMu.Unlock()
		if fail {
			fail = false
			return nil, errors.New("issuer hiccup")
		}
		return &auth.AuthCredential{
			AccessToken: "fresh",
			ExpiresAt:   time.Now().Add(time.Hour),
		}, nil
	}
	store.persist = func(*codexCredential) error { return nil }

	source := store.tokenSource()
	// The first wave hits a failing issuer, the second finds it healthy again.
	for range 2 {
		var wg sync.WaitGroup
		for range 6 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, _ = source()
			}()
		}
		wg.Wait()
	}

	if got := peak.Load(); got != 1 {
		t.Fatalf("concurrent renewals = %d, want the latch to serialise them", got)
	}
	if token, account := store.snapshot(); token != "fresh" || account != "acct-1" {
		t.Fatalf("token = %q, account = %q, want the retry to have succeeded", token, account)
	}
}

// A renewal that lands after its starter stopped waiting still rotated the token
// at the issuer, so its result must be adopted rather than dropped.
func TestLateRefreshIsAdoptedNotDiscarded(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})
	release := make(chan struct{})
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		<-release
		return &auth.AuthCredential{
			AccessToken:  "late",
			RefreshToken: "refresh-2",
			ExpiresAt:    time.Now().Add(time.Hour),
		}, nil
	}
	var persisted []*codexCredential
	var persistMu sync.Mutex
	store.persist = func(cred *codexCredential) error {
		persistMu.Lock()
		defer persistMu.Unlock()
		persisted = append(persisted, cred)
		return nil
	}

	original := codexRefreshTimeout
	codexRefreshTimeout = 30 * time.Millisecond
	t.Cleanup(func() { codexRefreshTimeout = original })

	source := store.tokenSource()
	if _, _, err := source(); err == nil {
		t.Fatal("the starter must report the bounded wait failing")
	}

	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if token, account := store.snapshot(); token == "late" {
			if account != "acct-1" {
				t.Fatalf("account = %q, want the one carried forward", account)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the late renewal was discarded instead of committed")
		}
		time.Sleep(5 * time.Millisecond)
	}

	persistMu.Lock()
	defer persistMu.Unlock()
	if len(persisted) != 1 || persisted[0].RefreshToken != "refresh-2" {
		t.Fatalf("persisted = %+v, want the rotated token written to disk", persisted)
	}
}

// Renewal begins a whole skew before expiry, so a failed attempt inside that
// window must not fail the turn: the token in hand is still valid, and the skew
// exists precisely to absorb this.
func TestFailedRefreshInsideTheSkewKeepsTheValidToken(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "still-valid",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		// Inside the skew, so a renewal is due, but four minutes of life remain.
		ExpiresAt: time.Now().Add(4 * time.Minute),
	})
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		return nil, errors.New("issuer down")
	}
	store.persist = func(*codexCredential) error { return nil }

	token, account, err := store.tokenSource()()
	if err != nil {
		t.Fatalf("a failed renewal inside the skew must not fail the turn: %v", err)
	}
	if token != "still-valid" || account != "acct-1" {
		t.Fatalf("token = %q, account = %q", token, account)
	}
}

// Past the expiry there is no valid token to fall back to, so the failure is
// fatal to the turn and must be reported rather than papered over.
func TestFailedRefreshPastExpiryFailsTheTurn(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "dead",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		return nil, errors.New("issuer down")
	}
	store.persist = func(*codexCredential) error { return nil }

	if _, _, err := store.tokenSource()(); err == nil {
		t.Fatal("an expired token with a failed renewal must surface the failure")
	}
}

// picoclaw's RefreshAccessToken has no timeout, so an unresponsive issuer must
// not wedge the run forever.
func TestCodexTokenStoreBoundsAStalledRefresh(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		<-release
		return nil, errors.New("unblocked after the test")
	}
	store.persist = func(*codexCredential) error { return nil }

	// Pin the deadline low so the test does not wait the production timeout.
	original := codexRefreshTimeout
	codexRefreshTimeout = 50 * time.Millisecond
	t.Cleanup(func() { codexRefreshTimeout = original })

	done := make(chan error, 1)
	go func() {
		_, _, err := store.tokenSource()()
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "не ответил") {
			t.Fatalf("error = %v, want the bounded-wait failure", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a stalled refresh was not bounded")
	}
}

func TestCodexTokenStoreFailsWhenItCannotRefresh(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken: "stale",
		ExpiresAt:   time.Now().Add(-time.Hour),
	})
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		t.Fatal("refresh must not run without a refresh token")
		return nil, nil
	}
	store.persist = func(*codexCredential) error { return nil }

	_, _, err := store.tokenSource()()
	if err == nil || !strings.Contains(err.Error(), "ChatGPT") {
		t.Fatalf("error = %v, want a re-sign-in hint", err)
	}
}

func TestCodexTokenStoreSurvivesAFailedPersist(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(time.Minute),
	})
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		return &auth.AuthCredential{AccessToken: "fresh", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	store.persist = func(*codexCredential) error { return errors.New("read-only home") }

	token, _, err := store.tokenSource()()
	if err != nil {
		t.Fatalf("a failed persist must not fail the run: %v", err)
	}
	if token != "fresh" {
		t.Fatalf("token = %q", token)
	}
}


// --- dzzzr: which sign-in the agent runs on ---

// Every dzzzr release before the web sign-in read Codex CLI's file; a user who
// set that up must keep working without signing in again.
func TestCodexFallsBackToTheCodexCLIFile(t *testing.T) {
	isolateCodexAuth(t)
	useCodexCLIFixture(t)
	t.Setenv("DZZZR_LLM_PROVIDER", "codex")
	cfg, err := resolveLLMConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider == nil || cfg.BaseURL != codexEndpoint {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestOwnCodexSignInWinsOverTheCodexCLIFile(t *testing.T) {
	isolateCodexAuth(t)
	useCodexCLIFixture(t)
	writeTestCodexCredential(t, &codexCredential{AccessToken: "own", AccountID: "own-acc", ExpiresAt: time.Now().Add(time.Hour)})
	token, account, _, err := codexCredentials()
	if err != nil || token != "own" || account != "own-acc" {
		t.Fatalf("token=%q account=%q err=%v", token, account, err)
	}
}

// An explicit Codex CLI file keeps its meaning even beside dzzzr's own sign-in.
func TestCodexAuthFileVariableStillNamesACodexCLIFile(t *testing.T) {
	isolateCodexAuth(t)
	writeTestCodexCredential(t, &codexCredential{AccessToken: "own", AccountID: "own-acc"})
	t.Setenv("DZZZR_CODEX_AUTH_FILE", codexAuthFixture(t, "cli-token", "cli-account"))
	if token, _, _, err := codexCredentials(); err != nil || token != "cli-token" {
		t.Fatalf("token=%q err=%v", token, err)
	}
}

func TestCodexWithoutAnySignInExplainsWhereToLogIn(t *testing.T) {
	isolateCodexAuth(t)
	t.Setenv("DZZZR_LLM_PROVIDER", "codex")
	_, err := resolveLLMConfig()
	if err == nil || !strings.Contains(err.Error(), "ChatGPT") {
		t.Fatalf("err = %v", err)
	}
}

// A stored sign-in is picked up only when nothing else was configured.
func TestResolveLLMConfigPrefersAStoredSignInOverNothing(t *testing.T) {
	isolateCodexAuth(t)
	writeTestCodexCredential(t, &codexCredential{AccessToken: "own", AccountID: "own-acc", ExpiresAt: time.Now().Add(time.Hour)})
	cfg, err := resolveLLMConfig()
	if err != nil || cfg.BaseURL != codexEndpoint {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}
	t.Setenv("DZZZR_LLM_BASE_URL", "http://127.0.0.1:8317/v1")
	cfg, err = resolveLLMConfig()
	if err != nil || cfg.BaseURL != "http://127.0.0.1:8317/v1" {
		t.Fatalf("an explicit endpoint must win: cfg = %+v, err = %v", cfg, err)
	}
	t.Setenv("DZZZR_LLM_BASE_URL", "")
	t.Setenv("DZZZR_LLM_PROVIDER", "openrouter")
	t.Setenv("DZZZR_LLM_API_KEY", "k")
	cfg, err = resolveLLMConfig()
	if err != nil || cfg.BaseURL != defaultLLMBaseURL {
		t.Fatalf("an explicit API-key choice must win: cfg = %+v, err = %v", cfg, err)
	}
}

// The Codex CLI file alone never switches a user off their API key.
func TestCodexCLIFileAloneDoesNotSelectCodex(t *testing.T) {
	isolateCodexAuth(t)
	useCodexCLIFixture(t)
	if hasCodexCredential() {
		t.Fatal("hasCodexCredential must only report dzzzr's own sign-in")
	}
}
