package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skrashevich/dzzzr-cli/agentloop"
	"github.com/skrashevich/dzzzr-cli/agenttools"
)

func quietf(string, ...any) {}

func TestChatStoreCreateListGet(t *testing.T) {
	s := newChatStore(t.TempDir())
	first := s.create("moscow", agenttools.PolicyApprove)
	// list sorts by the moment a chat was last touched, so the two must not
	// share one.
	time.Sleep(2 * time.Millisecond)
	second := s.create("moscow", agenttools.PolicyReadonly)

	list := s.list()
	if len(list) != 2 {
		t.Fatalf("чатов %d, ожидалось 2", len(list))
	}
	if list[0].ID != second.ID {
		t.Errorf("первым идёт %q, ожидался последний созданный %q", list[0].ID, second.ID)
	}
	if list[0].Policy != agenttools.PolicyReadonly {
		t.Errorf("права чата = %q", list[0].Policy)
	}

	got, ok := s.get(first.ID)
	if !ok || got.City != "moscow" {
		t.Errorf("get вернул %+v, ok=%v", got, ok)
	}
	if _, ok := s.get("нет такого"); ok {
		t.Error("get нашёл несуществующий чат")
	}
}

func TestChatStoreAppendUserSetsTitle(t *testing.T) {
	s := newChatStore(t.TempDir())
	snap := s.create("moscow", agenttools.PolicyFull)

	if busy, ok := s.appendUser(snap.ID, "Разбери задание третьего уровня\nи предложи версии"); !ok || busy {
		t.Fatalf("appendUser: ok=%v busy=%v", ok, busy)
	}
	got, _ := s.get(snap.ID)
	if got.Title != "Разбери задание третьего уровня" {
		t.Errorf("заголовок = %q", got.Title)
	}
	if len(got.Lines) != 1 || got.Lines[0].Role != chatRoleUser {
		t.Errorf("лента = %+v", got.Lines)
	}

	// A chat that already has a title keeps it.
	if _, ok := s.appendUser(snap.ID, "второй вопрос"); !ok {
		t.Fatal("второе сообщение не принято")
	}
	got, _ = s.get(snap.ID)
	if got.Title != "Разбери задание третьего уровня" {
		t.Errorf("заголовок перезаписан вторым сообщением: %q", got.Title)
	}
}

func TestAutoTitleTruncates(t *testing.T) {
	long := strings.Repeat("я", 60)
	got := autoTitle("новый чат", long)
	if len([]rune(got)) != 49 || !strings.HasSuffix(got, "…") {
		t.Errorf("длинный заголовок = %q (%d рун)", got, len([]rune(got)))
	}
	if autoTitle("новый чат", "   \n  ") != "новый чат" {
		t.Error("пустое сообщение не должно менять заголовок")
	}
}

func TestChatStoreRunLifecycle(t *testing.T) {
	s := newChatStore(t.TempDir())
	snap := s.create("moscow", agenttools.PolicyFull)
	s.appendUser(snap.ID, "вопрос")

	canceled := false
	history, ok := s.beginRun(snap.ID, func() { canceled = true })
	if !ok {
		t.Fatal("beginRun отказал на свободном чате")
	}
	if len(history) != 1 || history[0].Role != agentloop.RoleUser {
		t.Errorf("история для модели = %+v", history)
	}

	if _, ok := s.beginRun(snap.ID, nil); ok {
		t.Error("beginRun разрешил второй ход поверх идущего")
	}
	if busy, ok := s.appendUser(snap.ID, "ещё"); ok || !busy {
		t.Errorf("во время хода сообщение принято: ok=%v busy=%v", ok, busy)
	}
	if got, _ := s.get(snap.ID); !got.Running {
		t.Error("признак running не выставлен")
	}

	if !s.cancelRun(snap.ID) {
		t.Error("cancelRun не нашёл идущий ход")
	}
	if !canceled {
		t.Error("функция отмены не вызвана")
	}

	s.finishRun(snap.ID, append(history, agentloop.Message{Role: agentloop.RoleAssistant, Content: "ответ"}))
	got, _ := s.get(snap.ID)
	if got.Running {
		t.Error("признак running не снят")
	}
	if _, ok := s.beginRun(snap.ID, nil); !ok {
		t.Error("после finishRun новый ход не начинается")
	}
}

func TestChatStoreFinishRunKeepsHistoryOnFailure(t *testing.T) {
	s := newChatStore(t.TempDir())
	snap := s.create("moscow", agenttools.PolicyFull)
	s.appendUser(snap.ID, "вопрос")
	s.beginRun(snap.ID, nil)

	// A failed run has nothing to store; what the user said must survive.
	s.finishRun(snap.ID, nil)
	history, ok := s.beginRun(snap.ID, nil)
	if !ok || len(history) != 1 {
		t.Errorf("история после неудачного хода = %+v", history)
	}
}

func TestChatStoreUpdateAndRemove(t *testing.T) {
	s := newChatStore(t.TempDir())
	snap := s.create("moscow", agenttools.PolicyApprove)

	title, policy := "своё имя", agenttools.PolicyReadonly
	got, ok := s.update(snap.ID, chatPatch{Title: &title, Policy: &policy})
	if !ok || got.Title != title || got.Policy != policy {
		t.Errorf("update вернул %+v, ok=%v", got, ok)
	}
	if _, ok := s.update("нет такого", chatPatch{}); ok {
		t.Error("update изменил несуществующий чат")
	}

	if !s.remove(snap.ID) {
		t.Error("remove не удалил чат")
	}
	if s.remove(snap.ID) {
		t.Error("remove удалил чат дважды")
	}
	if len(s.list()) != 0 {
		t.Error("удалённый чат остался в списке")
	}
}

func TestChatStorePersistRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "chats")
	s := newChatStore(dir)
	snap := s.create("ekb", agenttools.PolicyReadonly)
	s.appendUser(snap.ID, "что с уровнем")
	s.appendLine(snap.ID, chatRoleTool, "· game_status {}", "game_status")
	s.finishRun(snap.ID, nil)
	if err := s.persist(snap.ID); err != nil {
		t.Fatalf("persist: %v", err)
	}

	path := filepath.Join(dir, snap.ID+".json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("файл чата не создан: %v", err)
	}
	if perm := info.Mode().Perm(); perm != sessionFilePerm {
		t.Errorf("права файла %v, ожидались %v", perm, sessionFilePerm)
	}
	if dirInfo, err := os.Stat(dir); err != nil {
		t.Fatalf("каталог чатов не создан: %v", err)
	} else if perm := dirInfo.Mode().Perm(); perm != sessionDirPerm {
		t.Errorf("права каталога %v, ожидались %v", perm, sessionDirPerm)
	}

	restored := newChatStore(dir)
	if err := restored.loadFromDisk(quietf); err != nil {
		t.Fatalf("loadFromDisk: %v", err)
	}
	got, ok := restored.get(snap.ID)
	if !ok {
		t.Fatal("чат не восстановлен")
	}
	if got.City != "ekb" || got.Policy != agenttools.PolicyReadonly {
		t.Errorf("восстановленный чат = %+v", got)
	}
	if len(got.Lines) != 2 || got.Lines[1].ToolName != "game_status" {
		t.Errorf("лента после восстановления = %+v", got.Lines)
	}
	history, _ := restored.beginRun(snap.ID, nil)
	if len(history) != 1 || history[0].Content != "что с уровнем" {
		t.Errorf("история для модели не восстановлена: %+v", history)
	}
}

func TestChatStoreLoadSkipsDamagedFiles(t *testing.T) {
	dir := t.TempDir()
	good := newChatStore(dir)
	snap := good.create("moscow", agenttools.PolicyFull)
	good.appendUser(snap.ID, "живой чат")
	good.finishRun(snap.ID, nil)
	if err := good.persist(snap.ID); err != nil {
		t.Fatalf("persist: %v", err)
	}
	for name, content := range map[string]string{
		"broken.json":  "{ это не json",
		"noid.json":    `{"title":"без идентификатора"}`,
		"notes.txt":    "не чат вовсе",
		"emptied.json": "",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	restored := newChatStore(dir)
	if err := restored.loadFromDisk(quietf); err != nil {
		t.Fatalf("loadFromDisk: %v", err)
	}
	list := restored.list()
	if len(list) != 1 || list[0].ID != snap.ID {
		t.Errorf("восстановлено %d чатов: %+v", len(list), list)
	}
}

func TestChatStoreLoadFromMissingDirectory(t *testing.T) {
	s := newChatStore(filepath.Join(t.TempDir(), "нет-такого"))
	if err := s.loadFromDisk(quietf); err != nil {
		t.Errorf("отсутствие каталога не должно быть ошибкой: %v", err)
	}
	if len(s.list()) != 0 {
		t.Error("из пустоты восстановились чаты")
	}
}

func TestChatStoreRemovePersisted(t *testing.T) {
	dir := t.TempDir()
	s := newChatStore(dir)
	snap := s.create("moscow", agenttools.PolicyFull)
	if err := s.persist(snap.ID); err != nil {
		t.Fatalf("persist: %v", err)
	}
	s.removePersisted(snap.ID)
	if _, err := os.Stat(filepath.Join(dir, snap.ID+".json")); !os.IsNotExist(err) {
		t.Errorf("файл чата остался: %v", err)
	}
	// Removing a chat that was never written must not fail either.
	s.removePersisted("нет такого")
}

func TestChatIDsAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id := newChatID()
		if seen[id] {
			t.Fatalf("идентификатор %q выдан дважды", id)
		}
		seen[id] = true
		if strings.ContainsAny(id, `/\.`) {
			t.Fatalf("идентификатор %q не годится для имени файла", id)
		}
	}
}

// Leaving has to stop every run, not only the one the user happens to be
// looking at.
func TestChatStoreCancelAll(t *testing.T) {
	s := newChatStore(t.TempDir())
	first := s.create("moscow", agenttools.PolicyFull)
	second := s.create("moscow", agenttools.PolicyFull)

	stopped := make([]bool, 2)
	s.beginRun(first.ID, func() { stopped[0] = true })
	s.beginRun(second.ID, func() { stopped[1] = true })

	s.cancelAll()
	if !stopped[0] || !stopped[1] {
		t.Errorf("остановлены ходы %v, ожидались оба", stopped)
	}
	// A store with nothing running must not panic on the way out either.
	s.finishRun(first.ID, nil)
	s.finishRun(second.ID, nil)
	s.cancelAll()
}

// -debug has to move cfg.stderr: the run captured os.Stderr into it at
// startup, so swapping only the package variable would leave every diagnostic
// painting over the alternate screen.
func TestRedirectDebugMovesConfigStderr(t *testing.T) {
	isolate(t)
	var screen bytes.Buffer
	cfg := &config{debug: true, stdout: io.Discard, stderr: &screen}

	restore, path, err := redirectDebugToFile(cfg)
	if err != nil {
		t.Fatalf("redirectDebugToFile: %v", err)
	}
	cfg.debugf("запрос к движку %s", "GET /go/")
	restore()

	if screen.Len() != 0 {
		t.Errorf("отладка попала на экран: %q", screen.String())
	}
	logged, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("журнал не создан: %v", err)
	}
	if !strings.Contains(string(logged), "GET /go/") {
		t.Errorf("журнал пуст или без записи: %q", logged)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if perm := info.Mode().Perm(); perm != sessionFilePerm {
		t.Errorf("права журнала %v, ожидались %v", perm, sessionFilePerm)
	}

	// After the restore, diagnostics go back to the stream the run owns.
	cfg.debugf("снова на экран")
	if !strings.Contains(screen.String(), "снова на экран") {
		t.Errorf("после восстановления отладка не вернулась на экран: %q", screen.String())
	}
}

func TestValidChatID(t *testing.T) {
	if !validChatID(newChatID()) {
		t.Error("собственный идентификатор признан недопустимым")
	}
	for _, bad := range []string{
		"", "короткий", "../../etc/passwd", "0123456789abcdeF",
		"0123456789abcde", "0123456789abcdef0", "0123456789abcde/",
	} {
		if validChatID(bad) {
			t.Errorf("идентификатор %q принят", bad)
		}
	}
}

// A chat file names itself, and that name becomes a path again on the next
// save, so an identifier that could escape the directory is refused.
func TestChatStoreLoadRefusesUnsafeID(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "evil.json"),
		[]byte(`{"id":"../../../../tmp/pwned","title":"злой чат","policy":"full"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newChatStore(dir)
	if err := s.loadFromDisk(quietf); err != nil {
		t.Fatalf("loadFromDisk: %v", err)
	}
	if len(s.list()) != 0 {
		t.Errorf("чат с опасным идентификатором загружен: %+v", s.list())
	}
}
