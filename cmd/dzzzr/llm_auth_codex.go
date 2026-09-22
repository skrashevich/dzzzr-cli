package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/providers"
)

// This file holds dzzzr's own ChatGPT sign-in: the credential the web settings
// panel obtains through OAuth, and the store that keeps its access token fresh
// while chat turns use it. Codex CLI's file remains usable beside it; see
// codexCredentials.

// codexTokenRefreshSkew renews an access token slightly before it expires.
//
// One agent run makes a request per turn over several minutes, so a token with
// seconds of life left would be attached to a request and expire before OpenAI
// validated it, failing the turn with a 401 that no retry covers.
const codexTokenRefreshSkew = 5 * time.Minute

// codexRefreshTimeout bounds a single renewal attempt.
//
// PicoClaw's RefreshAccessToken posts through http.DefaultClient with no
// timeout and no context, so a blackholed issuer would otherwise wedge every
// chat turn for as long as the connection stayed open.
//
// This bounds the wait, not the request: the attempt keeps running and keeps
// the inFlight latch, which is what stops the next turn opening a second
// renewal with the same refresh token.
//
// A var, not a const, so tests can pin a short deadline.
var codexRefreshTimeout = 30 * time.Second

// codexRefreshRetryCooldown throttles renewal after a failed attempt, so a dead
// issuer costs one call a minute rather than one per agent turn.
const codexRefreshRetryCooldown = time.Minute

// errNoCodexCredential is returned when no ChatGPT sign-in is stored. It is a
// sentinel so callers can tell "not signed in" from a broken file.
var errNoCodexCredential = errors.New("вход в ChatGPT не выполнен: войдите в веб-интерфейсе (⚙ Настройки LLM → Подписка ChatGPT) или через Codex CLI")

// codexCredential is the persisted half of a ChatGPT sign-in. It is a subset of
// auth.AuthCredential: provider and auth method are implied by the file.
type codexCredential struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	AccountID    string    `json:"account_id,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitzero"`
}

const codexCredentialWhat = "вход в ChatGPT"

// codexAuthFile is where dzzzr keeps its own sign-in. It has no override of
// its own: DZZZR_CODEX_AUTH_FILE has always named a Codex CLI file, and keeps
// that meaning.
func codexAuthFile() (string, error) {
	return stateFilePath("", "codex", "auth.json")
}

func loadCodexCredential() (*codexCredential, error) {
	path, err := codexAuthFile()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, errNoCodexCredential
	}
	cred, err := loadJSONState[codexCredential](path, codexCredentialWhat)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cred.AccessToken) == "" {
		return nil, fmt.Errorf("в файле %s нет токена доступа: войдите в ChatGPT заново", path)
	}
	return &cred, nil
}

func saveCodexCredential(cred *codexCredential) error {
	if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		return errors.New("вход в ChatGPT без токена доступа не сохраняется")
	}
	path, err := codexAuthFile()
	if err != nil {
		return err
	}
	return saveJSONState(path, codexCredentialWhat, *cred)
}

func deleteCodexCredential() error {
	path, err := codexAuthFile()
	if err != nil {
		return err
	}
	return deleteJSONState(path)
}

// hasCodexCredential reports whether dzzzr holds a usable sign-in of its own.
// Codex CLI's file does not count: its presence alone must not move a user off
// the API key they configured.
func hasCodexCredential() bool {
	_, err := loadCodexCredential()
	return err == nil
}

func codexCredentialFromAuth(cred *auth.AuthCredential) *codexCredential {
	return &codexCredential{
		AccessToken:  cred.AccessToken,
		RefreshToken: cred.RefreshToken,
		AccountID:    cred.AccountID,
		ExpiresAt:    cred.ExpiresAt,
	}
}

func (c *codexCredential) authCredential() *auth.AuthCredential {
	return &auth.AuthCredential{
		AccessToken:  c.AccessToken,
		RefreshToken: c.RefreshToken,
		AccountID:    c.AccountID,
		ExpiresAt:    c.ExpiresAt,
		Provider:     "openai",
		AuthMethod:   "oauth",
	}
}

// cliCodexTokenStore holds the live credential, renews it when it nears expiry
// and writes the renewal back to disk so the next run starts from a fresh
// token.
type cliCodexTokenStore struct {
	mu sync.Mutex
	// refreshed records that a credential without an expiry has already been
	// renewed successfully; lastRefreshAttempt throttles retries after a failure.
	refreshed          bool
	lastRefreshAttempt time.Time
	cred               *auth.AuthCredential

	// inFlight is non-nil while a renewal is running, and is closed when it
	// finishes. A caller may stop waiting before the attempt returns, so the
	// mutex alone cannot keep renewals exclusive: without this latch the next
	// turn would start a second renewal with the same refresh token, the issuer
	// would rotate it out from under the first, and whichever landed last would
	// persist a credential the issuer had already invalidated.
	inFlight *codexRefreshAttempt

	// retired is set, under codexStoresMu, once a sign-out or a new sign-in
	// replaced this store. A renewal still in flight then keeps its token for
	// the turn that asked, but must not write it back over what replaced it.
	retired bool

	// refresh and persist are injected so the refresh policy can be tested
	// without an OAuth issuer.
	refresh func(*auth.AuthCredential) (*auth.AuthCredential, error)
	persist func(*codexCredential) error
}

func newCLICodexTokenStore(cred *codexCredential) *cliCodexTokenStore {
	return &cliCodexTokenStore{
		cred: cred.authCredential(),
		refresh: func(current *auth.AuthCredential) (*auth.AuthCredential, error) {
			return auth.RefreshAccessToken(current, auth.OpenAIOAuthConfig())
		},
		persist: saveCodexCredential,
	}
}

// codexRefreshAttempt is one renewal. err is written before done is closed, so
// whoever observes the close sees it without taking the store mutex — and sees
// the outcome of THIS attempt rather than of whichever finished most recently.
type codexRefreshAttempt struct {
	done chan struct{}
	err  error
}

// tokenSource matches the refresh callback PicoClaw's Codex provider expects:
// the current access token and account ID, refreshed first when needed.
//
// At most one renewal runs at a time, and everyone who needs a fresh token
// waits for that one rather than starting another with the same refresh token.
func (s *cliCodexTokenStore) tokenSource() func() (string, string, error) {
	return func() (string, string, error) {
		s.mu.Lock()
		if !s.needsRefreshLocked() {
			token, account := s.cred.AccessToken, s.cred.AccountID
			s.mu.Unlock()
			return token, account, nil
		}
		attempt := s.inFlight
		if attempt == nil {
			if strings.TrimSpace(s.cred.RefreshToken) == "" {
				s.mu.Unlock()
				return "", "", errors.New("сессия ChatGPT истекла и не может быть продлена: войдите в ChatGPT заново")
			}
			attempt = s.startRefreshLocked()
		}
		s.mu.Unlock()

		timer := time.NewTimer(codexRefreshTimeout)
		defer timer.Stop()
		select {
		case <-attempt.done:
		case <-timer.C:
			// Give up waiting, but leave the attempt holding the latch: it is
			// still the only rotation in flight, and it commits its own result.
			return "", "", fmt.Errorf("сервер авторизации ChatGPT не ответил за %s", codexRefreshTimeout)
		}

		s.mu.Lock()
		token, account, expiresAt := s.cred.AccessToken, s.cred.AccountID, s.cred.ExpiresAt
		s.mu.Unlock()

		if attempt.err != nil {
			// Renewal starts a whole skew before expiry, so the token in hand is
			// usually still valid; failing the turn then would throw away the
			// slack the skew exists to provide. Only a token past its expiry — or
			// one with no expiry to judge by — makes the failure fatal.
			if !expiresAt.IsZero() && time.Now().Before(expiresAt) {
				return token, account, nil
			}
			return "", "", fmt.Errorf("не удалось продлить сессию ChatGPT: %w", attempt.err)
		}
		return token, account, nil
	}
}

// startRefreshLocked launches the one renewal allowed at a time and returns
// it. The caller holds s.mu.
//
// The attempt commits its own result, so a renewal that lands after its
// starter stopped waiting is still kept: discarding it would strand the
// credential on a refresh token the issuer has already rotated.
func (s *cliCodexTokenStore) startRefreshLocked() *codexRefreshAttempt {
	attempt := &codexRefreshAttempt{done: make(chan struct{})}
	s.inFlight = attempt
	s.lastRefreshAttempt = time.Now()
	previous, refresh := s.cred, s.refresh

	go func() {
		renewed, err := refresh(previous)

		s.mu.Lock()
		if err == nil && renewed != nil {
			if renewed.AccountID == "" {
				renewed.AccountID = previous.AccountID
			}
			s.cred = renewed
			s.refreshed = true
		}
		s.inFlight = nil
		s.mu.Unlock()

		// Written outside s.mu, so a slow disk does not stall every token read.
		// A failed write costs only the next process a refresh; the run
		// continues on the token in memory.
		if err == nil && renewed != nil {
			s.persistUnlessRetired(codexCredentialFromAuth(renewed))
		}

		attempt.err = err
		close(attempt.done)
	}()
	return attempt
}

// persistUnlessRetired writes a renewal unless a sign-out or a new sign-in
// has replaced this store since it started. codexStoresMu is held across the
// check and the write, and retireCodexTokenStores takes it too, so a retirement
// lands either before the write — which it then prevents — or after it, in
// which case whatever retired the store writes last.
func (s *cliCodexTokenStore) persistUnlessRetired(cred *codexCredential) {
	codexStoresMu.Lock()
	defer codexStoresMu.Unlock()
	if s.retired {
		return
	}
	_ = s.persist(cred)
}

// needsRefreshLocked reports whether the access token should be renewed. The
// caller holds s.mu.
func (s *cliCodexTokenStore) needsRefreshLocked() bool {
	if !s.cred.ExpiresAt.IsZero() {
		return time.Now().Add(codexTokenRefreshSkew).After(s.cred.ExpiresAt)
	}
	// Without an expiry there is no schedule to follow. Exactly one SUCCESSFUL
	// renewal is allowed, so the run starts on a token of known age without
	// rotating the refresh token on every call. A failed attempt stays
	// retryable, but behind a cooldown.
	if s.refreshed || s.cred.RefreshToken == "" {
		return false
	}
	return s.lastRefreshAttempt.IsZero() || time.Since(s.lastRefreshAttempt) >= codexRefreshRetryCooldown
}

// snapshot returns the credential the provider should start from.
func (s *cliCodexTokenStore) snapshot() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cred.AccessToken, s.cred.AccountID
}

var (
	codexStoresMu sync.Mutex
	codexStores   = map[string]*cliCodexTokenStore{}
)

// sharedCodexTokenStore returns one store per credential file for the lifetime
// of the process.
//
// The web interface runs each chat turn in its own goroutine, so a store built
// per turn would let two turns crossing the refresh window rotate the same
// refresh token, and the last writer could persist a credential that no longer
// works. One store per file serialises those refreshes into a single renewal.
func sharedCodexTokenStore(path string, cred *codexCredential) *cliCodexTokenStore {
	codexStoresMu.Lock()
	defer codexStoresMu.Unlock()
	if store, ok := codexStores[path]; ok {
		return store
	}
	store := newCLICodexTokenStore(cred)
	codexStores[path] = store
	return store
}

// resetCodexTokenStores drops the cache: after a new sign-in or a sign-out the
// store would otherwise keep serving the old credential. The dropped stores
// are retired, so a renewal they still have in flight cannot write the old
// credential back. Callers that change the credential file reset first and
// write after.
func resetCodexTokenStores() {
	codexStoresMu.Lock()
	defer codexStoresMu.Unlock()
	for _, store := range codexStores {
		store.retired = true
	}
	clear(codexStores)
}

// newCodexProvider builds a PicoClaw provider on dzzzr's own sign-in. The
// cached store may already hold a token newer than the file, so the provider
// starts from the store; renewal waits for the first request.
func newCodexProvider() (providers.LLMProvider, error) {
	store, err := ownCodexTokenStore()
	if err != nil {
		return nil, err
	}
	token, account := store.snapshot()
	return providers.NewCodexProviderWithTokenSource(token, account, store.tokenSource()), nil
}

// ownCodexTokenStore is the shared store for dzzzr's own sign-in.
func ownCodexTokenStore() (*cliCodexTokenStore, error) {
	cred, err := loadCodexCredential()
	if err != nil {
		return nil, err
	}
	path, err := codexAuthFile()
	if err != nil {
		return nil, err
	}
	return sharedCodexTokenStore(path, cred), nil
}
