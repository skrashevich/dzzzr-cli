package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/skrashevich/dzzzr-cli/agenttools"
)

// confirmOverHTTP starts a confirmation, waits for it to reach the gate, then
// answers it the way the browser would and returns what the agent was told.
func confirmOverHTTP(t *testing.T, action string) (allowed bool, err error) {
	t.Helper()
	hub := newTestHub(t, nil)
	srv := httptest.NewServer(hub.newMux())
	defer srv.Close()

	snap := hub.store.create("moscow", agenttools.PolicyApprove)
	confirmer := &webConfirmer{hub: hub, chatID: snap.ID}

	done := make(chan struct{})
	go func() {
		defer close(done)
		allowed, err = confirmer.ConfirmToolCall(context.Background(),
			agenttools.ConfirmRequest{Tool: "send_code", Args: map[string]any{"code": "рубин"}})
	}()

	// The confirmer publishes and blocks; the gate appears a moment later.
	var prompt map[string]any
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		prompt = nil
		if code := webDo(t, srv, http.MethodGet, "/api/v1/chats/"+snap.ID+"/approval", "", &prompt); code == http.StatusOK {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if prompt["tool"] != "send_code" {
		t.Fatalf("запрос на согласование не появился: %+v", prompt)
	}
	if args, _ := prompt["args"].(string); !strings.Contains(args, "рубин") {
		t.Errorf("аргументы вызова потеряны: %+v", prompt)
	}

	if code := webDo(t, srv, http.MethodPost, "/api/v1/chats/"+snap.ID+"/approval",
		`{"action":"`+action+`"}`, nil); code != http.StatusOK {
		t.Fatalf("ответ %q вернул %d", action, code)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("агент не получил ответа")
	}
	return allowed, err
}

func TestWebApprovalYesAllowsCall(t *testing.T) {
	allowed, err := confirmOverHTTP(t, "yes")
	if err != nil || !allowed {
		t.Fatalf("«да» дало allowed=%t, err=%v", allowed, err)
	}
}

func TestWebApprovalNoRefusesOneCall(t *testing.T) {
	allowed, err := confirmOverHTTP(t, "no")
	if err != nil || allowed {
		t.Fatalf("«нет» дало allowed=%t, err=%v", allowed, err)
	}
}

// «Стоп» is not the same as «нет»: the error ends the whole run instead of
// letting the model try the next mutating call.
func TestWebApprovalQuitStopsTheRun(t *testing.T) {
	allowed, err := confirmOverHTTP(t, "quit")
	if allowed {
		t.Fatal("«стоп» разрешило вызов")
	}
	if err == nil {
		t.Fatal("«стоп» не остановило выполнение")
	}
}

func TestWebApprovalWithoutPendingCall(t *testing.T) {
	hub := newTestHub(t, nil)
	srv := httptest.NewServer(hub.newMux())
	defer srv.Close()
	snap := hub.store.create("moscow", agenttools.PolicyApprove)

	if code := webDo(t, srv, http.MethodGet, "/api/v1/chats/"+snap.ID+"/approval", "", nil); code != http.StatusNotFound {
		t.Errorf("чтение без ожидающего вызова вернуло %d", code)
	}
	if code := webDo(t, srv, http.MethodPost, "/api/v1/chats/"+snap.ID+"/approval", `{"action":"yes"}`, nil); code != http.StatusNotFound {
		t.Errorf("ответ без ожидающего вызова вернул %d", code)
	}
	if code := webDo(t, srv, http.MethodPost, "/api/v1/chats/"+snap.ID+"/approval", `{"action":"может быть"}`, nil); code != http.StatusBadRequest {
		t.Errorf("неизвестное действие принято с кодом %d", code)
	}
}

func TestParseApprovalAction(t *testing.T) {
	for in, want := range map[string]approvalAction{
		"yes": approvalYes, "y": approvalYes, "да": approvalYes,
		"no": approvalNo, "": approvalNo, "нет": approvalNo,
		"quit": approvalQuit, "q": approvalQuit, "стоп": approvalQuit,
	} {
		got, err := parseApprovalAction(in)
		if err != nil || got != want {
			t.Errorf("parseApprovalAction(%q) = %q, %v; ожидалось %q", in, got, err, want)
		}
	}
	if _, err := parseApprovalAction("наверное"); err == nil {
		t.Error("неизвестное действие принято")
	}
}

// A second answer to the same call must not be queued for the next one.
func TestApprovalGateAnswersOnce(t *testing.T) {
	gate := newApprovalGate()
	if err := gate.respond(approvalYes); err != nil {
		t.Fatalf("первый ответ отклонён: %v", err)
	}
	if err := gate.respond(approvalNo); err == nil {
		t.Error("второй ответ принят")
	}
	action, err := gate.wait(context.Background())
	if err != nil || action != approvalYes {
		t.Fatalf("получено %q, %v", action, err)
	}
	gate.close()
	if err := gate.respond(approvalYes); err == nil {
		t.Error("закрытая калитка приняла ответ")
	}
}

// A canceled run must release the agent rather than leave it waiting.
func TestApprovalGateReleasesOnCancel(t *testing.T) {
	gate := newApprovalGate()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gate.wait(ctx); err == nil {
		t.Fatal("отменённое согласование не вернуло ошибку")
	}
}
