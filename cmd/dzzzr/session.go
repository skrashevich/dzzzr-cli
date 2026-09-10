package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

// Session files live next to other per-user configuration and hold the site
// token, the captain login and PIN, and the organizer password, so they are
// created readable by their owner only.
const (
	sessionDirPerm  fs.FileMode = 0o700
	sessionFilePerm fs.FileMode = 0o600
)

// sessionDir returns ~/.config/dzzzr.
func sessionDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fatal("не удалось определить домашний каталог: %v", err)
	}
	return filepath.Join(home, ".config", "dzzzr"), nil
}

// sanitizeCity turns a city into a file name: the engine accepts a path
// segment there, and a URL-shaped -city must not escape the session
// directory.
func sanitizeCity(city string) string {
	city = strings.TrimSpace(city)
	if city == "" {
		return "default"
	}
	return strings.NewReplacer("/", "_", ":", "_", `\`, "_").Replace(city)
}

// sessionPath returns the session file of one city.
func sessionPath(city string) (string, error) {
	dir, err := sessionDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sanitizeCity(city)+".json"), nil
}

// saveSession writes the client's credentials and token to the city's
// session file and returns its path.
func saveSession(cfg *config, c *dzzzr.Client) (string, error) {
	path, err := sessionPath(cfg.city)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, sessionDirPerm); err != nil {
		return "", fatal("не удалось создать каталог %s: %v", dir, err)
	}
	// MkdirAll leaves an existing directory alone, so its mode is set too.
	if err := os.Chmod(dir, sessionDirPerm); err != nil {
		return "", fatal("не удалось выставить права на %s: %v", dir, err)
	}
	data, err := c.ExportSession()
	if err != nil {
		return "", fatal("не удалось сохранить сессию: %v", err)
	}
	if err := writeSecretFile(path, data); err != nil {
		return "", err
	}
	cfg.debugf("сессия сохранена в %s", path)
	return path, nil
}

// writeSecretFile writes data through a fresh temporary file and renames it
// into place. Writing over the file directly would keep the mode it already
// had, leaving a PIN and the organizer's password readable to everyone until
// a later chmod, and would truncate the old contents before the new ones are
// safe on disk.
func writeSecretFile(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_TRUNC, sessionFilePerm)
	if err != nil {
		// A leftover temporary file from a killed run must not block the save.
		if errors.Is(err, fs.ErrExist) {
			if rmErr := os.Remove(tmp); rmErr == nil {
				f, err = os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_TRUNC, sessionFilePerm)
			}
		}
		if err != nil {
			return fatal("не удалось создать %s: %v", tmp, err)
		}
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fatal("не удалось записать %s: %v", tmp, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fatal("не удалось закрыть %s: %v", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fatal("не удалось заменить %s: %v", path, err)
	}
	return nil
}

// loadSession restores a saved session into the client. A missing file is
// not an error: the run may still authenticate from flags.
func loadSession(cfg *config, c *dzzzr.Client) (string, bool, error) {
	path, err := sessionPath(cfg.city)
	if err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return path, false, nil
	}
	if err != nil {
		return path, false, fatal("не удалось прочитать %s: %v", path, err)
	}
	if err := c.ImportSession(data); err != nil {
		return path, false, fatal("файл сессии %s повреждён: %v", path, err)
	}
	cfg.debugf("сессия загружена из %s", path)
	return path, true, nil
}

// removeSession deletes the city's session file, reporting whether there was
// one.
func removeSession(cfg *config) (string, bool, error) {
	path, err := sessionPath(cfg.city)
	if err != nil {
		return "", false, err
	}
	err = os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return path, false, nil
	}
	if err != nil {
		return path, false, fatal("не удалось удалить %s: %v", path, err)
	}
	return path, true, nil
}

// applyCredentialOverrides lets flags and environment variables win over
// whatever the session file carried.
func applyCredentialOverrides(cfg *config, c *dzzzr.Client) {
	cr := c.Credentials()
	if cfg.captain != "" {
		cr.Captain = cfg.captain
	}
	if cfg.pin != "" {
		cr.Pin = cfg.pin
	}
	c.SetCredentials(cr)
	if cfg.adminLogin != "" {
		c.SetAdminCredentials(cfg.adminLogin, cfg.adminPassword)
	}
}

// requireAuth prepares a client that can play. What the engine insists on is
// the site session; the captain login and PIN are optional, because Classic
// teams have none and the API's Basic wall only checks that a header exists
// (see dzzzr.Client.basicIdentity). requireAuth signs in when the session is
// missing and -login/-password were given, and stores the result for later
// runs.
func requireAuth(ctx context.Context, cfg *config, c *dzzzr.Client) error {
	path, _, err := loadSession(cfg, c)
	if err != nil {
		return err
	}
	applyCredentialOverrides(cfg, c)

	if c.Session() == "" {
		if !canSignIn(cfg) {
			return fatal("нет сохранённой сессии (%s). Выполните «dzzzr login» или задайте -login и -password (DZZZR_LOGIN/DZZZR_PASSWORD)", path)
		}
		return signIn(ctx, cfg, c)
	}
	return nil
}

// optionalAuth loads a stored session for a command that does not need one.
// The archive is public, and refusing to show it because nobody has signed in
// yet would be the client's own restriction rather than the engine's.
func optionalAuth(cfg *config, c *dzzzr.Client) error {
	if _, _, err := loadSession(cfg, c); err != nil {
		return err
	}
	applyCredentialOverrides(cfg, c)
	return nil
}

// canSignIn reports whether the run carries enough to obtain a session.
func canSignIn(cfg *config) bool { return cfg.login != "" && cfg.password != "" }

// signIn exchanges the site login and password for a session and stores it.
func signIn(ctx context.Context, cfg *config, c *dzzzr.Client) error {
	resp, err := c.Login(ctx, cfg.login, cfg.password)
	if err != nil {
		return err
	}
	if !resp.OK() {
		return fatal("вход не выполнен: %s", dzzzr.LoginCodeText(resp.Code.Int()))
	}
	if _, err := saveSession(cfg, c); err != nil {
		return err
	}
	return nil
}

// refreshSession signs in again after the engine rejected the stored session,
// and reports whether a retry of the command makes sense.
//
// Only a session rejection qualifies. APIconst.php checks the session before
// any handler runs, so a request refused that way never reached the game and
// repeating it cannot submit a code twice — unlike an unreadable reply, which
// IsUndecodableAccepted deliberately treats as already applied.
func refreshSession(ctx context.Context, cfg *config, c *dzzzr.Client, err error) bool {
	if dzzzr.AuthErrorKindOf(err) != dzzzr.AuthSession || !canSignIn(cfg) {
		return false
	}
	c.Logout()
	if signIn(ctx, cfg, c) != nil {
		return false
	}
	cfg.debugf("сессия истекла, выполнен повторный вход")
	return true
}

// requireAdminAuth prepares a client that can reach the administration
// area. The organizer credentials come from flags, the environment or the
// session file.
func requireAdminAuth(cfg *config, c *dzzzr.Client) error {
	if _, _, err := loadSession(cfg, c); err != nil {
		return err
	}
	applyCredentialOverrides(cfg, c)
	if !c.HasAdminCredentials() {
		return fatal("не заданы учётные данные организатора. Укажите -admin-login и -admin-password (DZZZR_ADMIN_LOGIN/DZZZR_ADMIN_PASSWORD)")
	}
	return nil
}
