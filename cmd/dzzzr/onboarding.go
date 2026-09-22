package main

// onboardingFileEnvVar relocates the state file; tests use it to stay out of
// the developer's real configuration.
const onboardingFileEnvVar = "DZZZR_ONBOARDING_FILE"

// onboardingState records that the first-run wizard has been through once. It
// exists so a user who already configured dzzzr is not walked through the
// steps again on every start of the web interface.
type onboardingState struct {
	Completed   bool   `json:"completed"`
	CompletedAt string `json:"completed_at,omitempty"`
	Skipped     bool   `json:"skipped,omitempty"`
}

// onboardingWhat is the user-facing noun in this file's errors.
const onboardingWhat = "состояние мастера настройки"

// onboardingFile resolves the state path. The "onboarding" subdirectory keeps
// it away from the per-city session files a logout deletes; losing it there
// would silently reopen the wizard on the next start.
func onboardingFile() (string, error) {
	return stateFilePath(onboardingFileEnvVar, "onboarding", "state.json")
}

// loadOnboardingState reads the wizard's state. A missing file means the
// wizard has never finished, which is the normal state of a new machine.
func loadOnboardingState() (onboardingState, error) {
	path, err := onboardingFile()
	if err != nil {
		return onboardingState{}, err
	}
	return loadJSONState[onboardingState](path, onboardingWhat)
}

func saveOnboardingState(s onboardingState) error {
	path, err := onboardingFile()
	if err != nil {
		return err
	}
	return saveJSONState(path, onboardingWhat, s)
}

// resetOnboardingState makes the wizard required again.
func resetOnboardingState() error {
	path, err := onboardingFile()
	if err != nil {
		return err
	}
	return deleteJSONState(path)
}
