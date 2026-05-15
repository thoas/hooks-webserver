package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- Store ---

func TestStore_AddAndDump(t *testing.T) {
	store := NewStore(5, nil)

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("hello"))
	req.Header.Set("Content-Type", "text/plain")
	req.RemoteAddr = "1.2.3.4:5678"

	if err := store.Add(req); err != nil {
		t.Fatal(err)
	}

	requests := store.Dump()
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}
	r := requests[0]
	if r.Method != http.MethodPost {
		t.Errorf("expected method POST, got %s", r.Method)
	}
	if r.Body != "hello" {
		t.Errorf("expected body 'hello', got %s", r.Body)
	}
	if r.ClientIP != "1.2.3.4" {
		t.Errorf("expected client IP '1.2.3.4', got %s", r.ClientIP)
	}
}

func TestStore_Flush(t *testing.T) {
	store := NewStore(5, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	store.Add(req)

	store.Flush()

	if len(store.Dump()) != 0 {
		t.Error("expected 0 requests after flush")
	}
}

func TestStore_RingBuffer(t *testing.T) {
	capacity := 3
	store := NewStore(capacity, nil)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if err := store.Add(req); err != nil {
			t.Fatal(err)
		}
	}

	requests := store.Dump()
	if len(requests) != capacity {
		t.Errorf("expected %d requests (capacity), got %d", capacity, len(requests))
	}
}

func TestStore_DumpSortedByCreatedAt(t *testing.T) {
	store := NewStore(10, nil)
	now := time.Now()

	store.requests[0] = &Request{Method: "GET", URI: "/second", CreatedAt: now.Add(time.Second)}
	store.requests[1] = &Request{Method: "GET", URI: "/first", CreatedAt: now}
	store.requests[2] = &Request{Method: "GET", URI: "/third", CreatedAt: now.Add(2 * time.Second)}

	requests := store.Dump()
	if len(requests) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(requests))
	}
	if requests[0].URI != "/first" || requests[1].URI != "/second" || requests[2].URI != "/third" {
		t.Errorf("wrong sort order: %s %s %s", requests[0].URI, requests[1].URI, requests[2].URI)
	}
}

func TestStore_ExcludedHeaders(t *testing.T) {
	store := NewStore(5, []string{"Authorization"})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "text/plain")

	if err := store.Add(req); err != nil {
		t.Fatal(err)
	}

	requests := store.Dump()
	if _, ok := requests[0].Headers["Authorization"]; ok {
		t.Error("excluded header 'Authorization' should not be stored")
	}
	if _, ok := requests[0].Headers["Content-Type"]; !ok {
		t.Error("non-excluded header 'Content-Type' should be stored")
	}
}

func TestStore_JSONBody(t *testing.T) {
	store := NewStore(5, nil)

	payload := `{"key":"value"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	if err := store.Add(req); err != nil {
		t.Fatal(err)
	}

	requests := store.Dump()
	if requests[0].Json == nil {
		t.Error("expected Json field to be set for application/json content-type")
	}
	if requests[0].Body != "" {
		t.Error("expected Body to be empty when content-type is application/json")
	}
}

// --- Handler ---

func newTestHandler(capacity int) *Handler {
	return NewHandler(NewStore(capacity, nil))
}

func TestHandler_Root(t *testing.T) {
	h := newTestHandler(5)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("data"))
	w := httptest.NewRecorder()

	h.Wrap(h.Root)(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if w.Body.String() != "Ok" {
		t.Errorf("expected body 'Ok', got %s", w.Body.String())
	}
	if len(h.store.Dump()) != 1 {
		t.Error("expected 1 stored request after Root")
	}
}

func TestHandler_Flush(t *testing.T) {
	h := newTestHandler(5)

	addReq := httptest.NewRequest(http.MethodGet, "/", nil)
	h.store.Add(addReq)

	req := httptest.NewRequest(http.MethodPost, "/flush", nil)
	w := httptest.NewRecorder()

	h.Wrap(h.Flush)(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if w.Body.String() != "Ok" {
		t.Errorf("expected body 'Ok', got %s", w.Body.String())
	}
	if len(h.store.Dump()) != 0 {
		t.Error("store should be empty after flush")
	}
}

func TestHandler_Dump(t *testing.T) {
	h := newTestHandler(5)

	addReq := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("payload"))
	h.store.Add(addReq)

	req := httptest.NewRequest(http.MethodGet, "/dump", nil)
	w := httptest.NewRecorder()

	h.Wrap(h.Dump)(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(resp.Requests) != 1 {
		t.Errorf("expected 1 request in dump, got %d", len(resp.Requests))
	}
}

func TestHandler_DumpEmpty(t *testing.T) {
	h := newTestHandler(5)

	req := httptest.NewRequest(http.MethodGet, "/dump", nil)
	w := httptest.NewRecorder()

	h.Wrap(h.Dump)(w, req)

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(resp.Requests) != 0 {
		t.Errorf("expected 0 requests in dump, got %d", len(resp.Requests))
	}
}

// --- Util ---

func TestRemoteIP_FromRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:1234"

	if ip := remoteIP(req); ip != "192.168.1.1" {
		t.Errorf("expected '192.168.1.1', got %s", ip)
	}
}

func TestRemoteIP_FromXForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ""
	req.Header.Set("X-Forwarded-For", "10.0.0.1")

	if ip := remoteIP(req); ip != "10.0.0.1" {
		t.Errorf("expected '10.0.0.1', got %s", ip)
	}
}

func TestRemoteIP_FromOriginWithPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://1.2.3.4:8080")

	if ip := remoteIP(req); ip != "1.2.3.4" {
		t.Errorf("expected '1.2.3.4', got %s", ip)
	}
}

func TestGetBody_NoBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	out, body, err := getBody(req)
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Error("expected nil json out for no-body request")
	}
	if len(body) != 0 {
		t.Errorf("expected empty body, got %q", string(body))
	}
}

func TestGetBody_PlainText(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello world"))
	req.Header.Set("Content-Type", "text/plain")

	out, body, err := getBody(req)
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Error("expected nil json out for plain text")
	}
	if string(body) != "hello world" {
		t.Errorf("expected 'hello world', got %s", string(body))
	}
}

func TestGetBody_JSON(t *testing.T) {
	payload := `{"foo":"bar"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	out, body, err := getBody(req)
	if err != nil {
		t.Fatal(err)
	}
	if out == nil {
		t.Fatal("expected json out to be set")
	}
	if body != nil {
		t.Error("expected nil body for json content-type")
	}
	m, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", out)
	}
	if m["foo"] != "bar" {
		t.Errorf("expected foo=bar, got %v", m["foo"])
	}
}

func TestDrainBody_Nil(t *testing.T) {
	r1, r2, err := drainBody(nil)
	if err != nil {
		t.Fatal(err)
	}
	if r1 != http.NoBody || r2 != http.NoBody {
		t.Error("expected NoBody for nil input")
	}
}

func TestDrainBody_Content(t *testing.T) {
	content := "test content"
	body := io.NopCloser(strings.NewReader(content))

	r1, r2, err := drainBody(body)
	if err != nil {
		t.Fatal(err)
	}

	b1, _ := io.ReadAll(r1)
	b2, _ := io.ReadAll(r2)

	if string(b1) != content {
		t.Errorf("r1: expected %q, got %q", content, string(b1))
	}
	if string(b2) != content {
		t.Errorf("r2: expected %q, got %q", content, string(b2))
	}
}
