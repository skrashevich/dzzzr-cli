package dzzzrmobile

// ExportSession serializes the session token and credentials so the app can
// keep the player signed in. The bytes contain secrets (the game PIN and, if
// set, the organizer password) and belong in the Keychain or an encrypted
// preference, not in a plain file.
func (c *DzzzrClient) ExportSession() ([]byte, error) { return c.client.ExportSession() }

// ImportSession restores what ExportSession produced.
func (c *DzzzrClient) ImportSession(data []byte) error { return c.client.ImportSession(data) }

// SetAdminCredentials installs organizer credentials for the administration
// area. A player app does not need them.
func (c *DzzzrClient) SetAdminCredentials(login, password string) {
	c.client.SetAdminCredentials(login, password)
}

// HasAdminCredentials reports whether organizer credentials are set.
func (c *DzzzrClient) HasAdminCredentials() bool { return c.client.HasAdminCredentials() }
