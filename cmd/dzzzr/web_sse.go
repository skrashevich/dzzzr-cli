package main

import (
	"bytes"
	"encoding/json/v2"
	"sync"
)

// sseFrameMax bounds one frame. A tool can return a level with every picture
// in it; a browser that is sent megabytes at once stops repainting, so an
// oversized payload is replaced by a note rather than delivered.
const sseFrameMax = 1 << 20

// chatSSE fans one chat's events out to everyone watching it.
type chatSSE struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func newChatSSE() *chatSSE {
	return &chatSSE{subs: make(map[chan []byte]struct{})}
}

// subscribe adds a listener with a buffer of at least four frames.
func (c *chatSSE) subscribe(buf int) chan []byte {
	if buf < 4 {
		buf = 4
	}
	ch := make(chan []byte, buf)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	c.mu.Unlock()
	return ch
}

// unsubscribe drops a listener and closes its channel exactly once.
func (c *chatSSE) unsubscribe(ch chan []byte) {
	c.mu.Lock()
	_, registered := c.subs[ch]
	if registered {
		delete(c.subs, ch)
	}
	c.mu.Unlock()
	if registered {
		close(ch)
	}
}

// broadcast delivers a frame to every listener that can still take one. A
// browser that has stopped reading must not hold up the agent, so its frame is
// dropped instead of blocking the run.
func (c *chatSSE) broadcast(frame []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for ch := range c.subs {
		select {
		case ch <- frame:
		default:
		}
	}
}

// sseHub routes events to the listeners of one chat.
type sseHub struct {
	mu    sync.Mutex
	chats map[string]*chatSSE
}

func newSSEHub() *sseHub {
	return &sseHub{chats: make(map[string]*chatSSE)}
}

// room returns the fan-out of one chat, creating it on first use.
func (h *sseHub) room(chatID string) *chatSSE {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.chats[chatID]
	if !ok {
		r = newChatSSE()
		h.chats[chatID] = r
	}
	return r
}

// removeChat closes every stream of a deleted chat, so the browser stops
// waiting for a conversation that no longer exists.
func (h *sseHub) removeChat(chatID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.chats[chatID]
	if !ok {
		return
	}
	delete(h.chats, chatID)
	r.mu.Lock()
	for ch := range r.subs {
		close(ch)
	}
	r.subs = nil
	r.mu.Unlock()
}

// formatSSE builds one Server-Sent Events frame: "event: …\ndata: …\n\n".
func formatSSE(eventType string, payload any) []byte {
	data, err := json.Marshal(payload)
	if err != nil {
		data, _ = json.Marshal(map[string]string{"error": err.Error()})
	}
	if len(data) > sseFrameMax {
		data, _ = json.Marshal(map[string]string{"error": "событие слишком велико"})
	}
	var buf bytes.Buffer
	buf.WriteString("event: ")
	buf.WriteString(eventType)
	buf.WriteString("\ndata: ")
	buf.Write(data)
	buf.WriteString("\n\n")
	return buf.Bytes()
}
