//go:build e2e

// Package e2e drives the built binaries end to end: the mock server plays the
// engine, the CLI plays the player. It is behind a build tag because it
// compiles both commands and binds a port.
//
//	go test -tags e2e ./e2e -v
package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type harness struct {
	t       *testing.T
	cli     string
	baseURL string
	home    string
}

// configDir is where the CLI under test keeps its session, pinned by
// DZZZR_CONFIG_DIR: HOME alone would not pin it, because os.UserHomeDir
// reads %USERPROFILE% on Windows and the run would land in the real profile.
func (h *harness) configDir() string {
	return filepath.Join(h.home, ".config", "dzzzr")
}

// build compiles a command into dir and returns its path.
func build(t *testing.T, dir, pkg, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", bin, pkg)
	cmd.Dir = ".."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, out)
	}
	return bin
}

// freePort asks the kernel for an unused port.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// start builds both binaries, runs the mock and waits for it to answer.
func start(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	cli := build(t, dir, "./cmd/dzzzr", "dzzzr")
	mock := build(t, dir, "./cmd/dzzzr-mock", "dzzzr-mock")

	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ctx, cancel := context.WithCancel(context.Background())
	server := exec.CommandContext(ctx, mock, "-addr", addr)
	server.Stderr = os.Stderr
	if err := server.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = server.Wait()
	})

	baseURL := "http://" + addr + "/moscow/"
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("mock server did not start on %s", addr)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return &harness{t: t, cli: cli, baseURL: baseURL, home: t.TempDir()}
}

// run executes the CLI and returns stdout, stderr and the exit code.
func (h *harness) run(args ...string) (string, string, int) {
	h.t.Helper()
	args = append([]string{"-base-url", h.baseURL}, args...)
	cmd := exec.Command(h.cli, args...)
	cmd.Env = append(os.Environ(), "HOME="+h.home, "DZZZR_CONFIG_DIR="+h.configDir(), "DZZZR_CITY=moscow")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		h.t.Fatalf("running %v: %v", args, err)
	}
	return stdout.String(), stderr.String(), code
}

// mustRun fails the test when the CLI exits non-zero.
func (h *harness) mustRun(args ...string) string {
	h.t.Helper()
	stdout, stderr, code := h.run(args...)
	if code != 0 {
		h.t.Fatalf("dzzzr %s exited %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), code, stdout, stderr)
	}
	return stdout
}

func (h *harness) login() {
	h.t.Helper()
	h.mustRun("login", "-login", "demo", "-password", "demo", "-captain", "demo", "-pin", "1234")
}

func TestPlayerWalkthrough(t *testing.T) {
	h := start(t)
	h.login()

	out := h.mustRun("status")
	if !strings.Contains(out, "Тестовая игра") || !strings.Contains(out, "Уровень 1") {
		t.Fatalf("status after login:\n%s", out)
	}

	if out := h.mustRun("send-code", "1"); !strings.Contains(out, "[8]") {
		t.Errorf("first code:\n%s", out)
	}
	h.mustRun("send-code", "2")
	if out := h.mustRun("send-code", "3"); !strings.Contains(out, "[9]") {
		t.Errorf("last code of level 1:\n%s", out)
	}
	if out := h.mustRun("status"); !strings.Contains(out, "Уровень 2") {
		t.Fatalf("level did not advance:\n%s", out)
	}

	// A wrong code is reported and ends with exit code 2, so a script can tell.
	stdout, _, code := h.run("send-code", "НЕВЕРНЫЙ")
	if code != 2 || !strings.Contains(stdout, "[11]") {
		t.Errorf("wrong code: exit %d\n%s", code, stdout)
	}

	// Level 2 needs only one of its two codes.
	if out := h.mustRun("send-code", "4"); !strings.Contains(out, "[9]") {
		t.Errorf("level 2:\n%s", out)
	}
	if out := h.mustRun("send-code", "6"); !strings.Contains(out, "[9]") && !strings.Contains(out, "[10]") {
		t.Errorf("level 3:\n%s", out)
	}
	if out := h.mustRun("status"); !strings.Contains(out, "завершена") && !strings.Contains(out, "Игра завершена") {
		t.Logf("final status:\n%s", out)
	}
}

func TestPlayerReads(t *testing.T) {
	h := start(t)
	h.login()
	for _, args := range [][]string{
		{"level"}, {"hints"}, {"bonus-levels"}, {"stat"}, {"log"}, {"messages"}, {"games"},
	} {
		if out := h.mustRun(args...); strings.TrimSpace(out) == "" {
			t.Errorf("dzzzr %s printed nothing", strings.Join(args, " "))
		}
	}
}

func TestJSONOutputIsMachineReadable(t *testing.T) {
	h := start(t)
	h.login()
	out := h.mustRun("-json", "status")
	var st struct {
		GameName string `json:"gameName"`
		Level    struct {
			LevelNumber int `json:"levelNumber"`
		} `json:"level"`
	}
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("status -json is not JSON: %v\n%s", err, out)
	}
	if st.GameName == "" || st.Level.LevelNumber == 0 {
		t.Errorf("status -json = %+v", st)
	}

	out = h.mustRun("-json", "send-code", "1")
	var res struct {
		Err      int    `json:"err"`
		Text     string `json:"text"`
		Accepted bool   `json:"accepted"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("send-code -json is not JSON: %v\n%s", err, out)
	}
	if res.Err != 8 || !res.Accepted || res.Text == "" {
		t.Errorf("send-code -json = %+v", res)
	}
}

func TestBonusSpoilerAndMessage(t *testing.T) {
	h := start(t)
	h.login()
	if out := h.mustRun("send-bonus", "4", "SK1"); !strings.Contains(out, "[") {
		t.Errorf("bonus code:\n%s", out)
	}
	if out := h.mustRun("spoiler", "1", "SP1"); !strings.Contains(out, "[55]") {
		t.Errorf("spoiler code:\n%s", out)
	}
	h.mustRun("send-message", "мы", "на", "месте")
	if out := h.mustRun("messages"); !strings.Contains(out, "мы на месте") {
		t.Errorf("posted message is missing:\n%s", out)
	}
}

func TestSessionSurvivesBetweenRuns(t *testing.T) {
	h := start(t)
	h.login()
	// A second process reuses the saved session without credentials.
	if out := h.mustRun("status"); !strings.Contains(out, "MockTeam") {
		t.Fatalf("status without credentials:\n%s", out)
	}
	sessionFile := filepath.Join(h.configDir(), "moscow.json")
	info, err := os.Stat(sessionFile)
	if err != nil {
		t.Fatalf("session file: %v", err)
	}
	// Windows keeps access rules instead of mode bits, and reports back a
	// mode the CLI never set.
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o600 {
		t.Errorf("session file mode = %o, want 600", perm)
	}
	h.mustRun("logout")
	if _, err := os.Stat(sessionFile); !os.IsNotExist(err) {
		t.Error("logout must remove the session file")
	}
	_, stderr, code := h.run("status")
	if code == 0 {
		t.Error("status after logout must fail")
	}
	if !strings.Contains(stderr, "login") {
		t.Errorf("stderr should advise logging in:\n%s", stderr)
	}
}

func TestUnknownCommandAndHelp(t *testing.T) {
	h := start(t)
	_, stderr, code := h.run("нетакой")
	if code == 0 || !strings.Contains(stderr, "нетакой") {
		t.Errorf("unknown command: exit %d\n%s", code, stderr)
	}
	if out := h.mustRun("version"); !strings.Contains(out, "dzzzr") {
		t.Errorf("version = %q", out)
	}
}

// The organizer half. The mock serves the administration area under the same
// Basic credentials the engine uses, so these run the real scraper against
// the real pages.

const (
	mockAdminLogin    = "admin"
	mockAdminPassword = "admin"
)

func (h *harness) admin(args ...string) []string {
	return append([]string{"-admin-login", mockAdminLogin, "-admin-password", mockAdminPassword}, args...)
}

func TestAdminWalkthrough(t *testing.T) {
	h := start(t)

	out := h.mustRun(h.admin("admin-games")...)
	if !strings.Contains(out, "4242") || !strings.Contains(out, "Тестовая игра") {
		t.Fatalf("admin-games:\n%s", out)
	}

	out = h.mustRun(h.admin("admin-levels", "4242")...)
	for _, want := range []string{"901", "Первый уровень", "основной"} {
		if !strings.Contains(out, want) {
			t.Errorf("admin-levels is missing %q:\n%s", want, out)
		}
	}

	out = h.mustRun(h.admin("admin-level", "4242", "901")...)
	if !strings.Contains(out, "codes=") && !strings.Contains(out, "Коды") {
		t.Errorf("admin-level:\n%s", out)
	}

	// Creating a level and reading it back proves the form round trip.
	out = h.mustRun(h.admin("admin-create-level", "4242",
		"title=Из e2e", "question=Вопрос", "hint1=П1", "hint2=П2",
		"codes=E2E1:1:1", "bonus_codes=E2EB:1:5", "code_count=1")...)
	if !strings.Contains(out, "созда") {
		t.Fatalf("admin-create-level:\n%s", out)
	}
	out = h.mustRun(h.admin("admin-levels", "4242")...)
	if !strings.Contains(out, "Из e2e") || !strings.Contains(out, "E2E1") {
		t.Errorf("the created level is not in the list:\n%s", out)
	}

	// The organizer's view of the team, and a change to it.
	out = h.mustRun(h.admin("admin-teams", "4242")...)
	if !strings.Contains(out, "MockTeam") || !strings.Contains(out, "5150") {
		t.Fatalf("admin-teams:\n%s", out)
	}
	h.mustRun(h.admin("admin-set-application", "4242", "5150", "status=1", "points=7")...)
	out = h.mustRun(h.admin("admin-teams", "4242")...)
	if !strings.Contains(out, "принята") || !strings.Contains(out, "7") {
		t.Errorf("the application change is not visible:\n%s", out)
	}

	out = h.mustRun(h.admin("admin-monitor", "4242")...)
	if !strings.Contains(out, "MockTeam") || !strings.Contains(out, "работает") {
		t.Errorf("admin-monitor:\n%s", out)
	}
}

func TestAdminInterventionReachesThePlayer(t *testing.T) {
	h := start(t)
	h.login()

	// The organizer hands the team level 2 and the team sees it.
	h.mustRun(h.admin("admin-give-level", "4242", "5150", "2")...)
	if out := h.mustRun("status"); !strings.Contains(out, "Уровень 2") {
		t.Fatalf("the team did not receive the level:\n%s", out)
	}

	// A message from the organizer arrives in the team's interface.
	h.mustRun(h.admin("admin-send-message", "4242", "Ждите", "-important")...)
	if out := h.mustRun("messages"); !strings.Contains(out, "Ждите") {
		t.Errorf("the message did not reach the team:\n%s", out)
	}

	// Stopping the engine stops the game for the team. The engine itself has
	// only a toggle, so the command reads the state before pressing it.
	h.mustRun(h.admin("admin-engine", "4242", "off")...)
	stdout, _, code := h.run("send-code", "4")
	if code == 0 || !strings.Contains(stdout, "[13]") {
		t.Errorf("a stopped engine must refuse codes: exit %d\n%s", code, stdout)
	}
	h.mustRun(h.admin("admin-engine", "4242", "on")...)
	if out := h.mustRun("send-code", "4"); !strings.Contains(out, "[9]") {
		t.Errorf("after restarting the engine the code must be accepted:\n%s", out)
	}
}

func TestAdminLogFollowsTheGame(t *testing.T) {
	h := start(t)
	h.login()

	out := h.mustRun(h.admin("admin-log", "4242")...)
	if !strings.Contains(out, "выдан уровень") {
		t.Fatalf("admin-log:\n%s", out)
	}
	// The timestamp the command reports feeds the next poll.
	i := strings.Index(out, "-since")
	if i < 0 {
		t.Fatalf("admin-log must report the timestamp for the next poll:\n%s", out)
	}

	h.mustRun("send-code", "1")
	out = h.mustRun(h.admin("admin-log", "4242")...)
	if !strings.Contains(out, "принят код") {
		t.Errorf("the accepted code is not in the organizer's log:\n%s", out)
	}
}

func TestAdminRequiresCredentials(t *testing.T) {
	h := start(t)
	_, stderr, code := h.run("admin-games")
	if code == 0 {
		t.Fatal("admin-games without credentials must fail")
	}
	if !strings.Contains(stderr, "admin-login") {
		t.Errorf("stderr should name the missing flag:\n%s", stderr)
	}
	_, stderr, code = h.run("-admin-login", "org", "-admin-password", "wrong", "admin-games")
	if code == 0 || !strings.Contains(stderr, "организатор") {
		t.Errorf("wrong credentials: exit %d\n%s", code, stderr)
	}
}

func TestMCPServesTools(t *testing.T) {
	h := start(t)
	h.login()

	// A real MCP client keeps the pipe open while it waits, so the test does
	// too: closing stdin ends the server, and it would end before answering.
	cmd := exec.Command(h.cli, "-base-url", h.baseURL, "mcp")
	cmd.Env = append(os.Environ(), "HOME="+h.home, "DZZZR_CONFIG_DIR="+h.configDir(), "DZZZR_CITY=moscow")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	}()

	for _, req := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"e2e","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"game_status","arguments":{}}}`,
	} {
		if _, err := io.WriteString(stdin, req+"\n"); err != nil {
			t.Fatalf("writing to the server: %v\nstderr: %s", err, stderr.String())
		}
	}

	var sawTools, sawStatus bool
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	// Read only until both answers arrive: the server keeps the pipe open
	// waiting for more requests, so another Scan would block forever.
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg struct {
			ID     int `json:"id"`
			Result struct {
				Tools []struct {
					Name        string `json:"name"`
					Annotations struct {
						ReadOnlyHint bool `json:"readOnlyHint"`
					} `json:"annotations"`
				} `json:"tools"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("MCP wrote a line that is not JSON: %v\n%s", err, line)
		}
		switch msg.ID {
		case 2:
			sawTools = true
			names := map[string]bool{}
			for _, tool := range msg.Result.Tools {
				names[tool.Name] = true
				if tool.Name == "game_status" && !tool.Annotations.ReadOnlyHint {
					t.Error("game_status must be annotated read-only")
				}
			}
			if !names["game_status"] || !names["level_info"] {
				t.Errorf("published tools = %v", names)
			}
			// The default policy is read-only, so no mutation is published.
			if names["send_code"] {
				t.Error("a read-only server must not publish send_code")
			}
		case 3:
			sawStatus = true
			if len(msg.Result.Content) == 0 || !strings.Contains(msg.Result.Content[0].Text, "Тестовая игра") {
				t.Errorf("game_status result = %+v", msg.Result.Content)
			}
		}
		if sawTools && sawStatus {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading the server: %v\nstderr: %s", err, stderr.String())
	}
	if !sawTools || !sawStatus {
		t.Errorf("MCP did not answer both requests: tools=%v status=%v\nstderr: %s", sawTools, sawStatus, stderr.String())
	}
}
