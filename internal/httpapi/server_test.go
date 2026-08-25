package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ennaton/nine-ingest/internal/auth"
	"github.com/ennaton/nine-ingest/internal/kafka"
)

const tenant = "11111111-1111-1111-1111-111111111111"
const apiKey = "nk_test_key_value_1234567890"

func harness(t *testing.T) (http.Handler, *kafka.Recorder) {
	t.Helper()
	keys := auth.NewKeys()
	keys.Add(tenant, apiKey)
	rec := &kafka.Recorder{}
	return New(keys, rec, slog.New(slog.NewTextHandler(io.Discard, nil))).Routes(), rec
}

func post(t *testing.T, h http.Handler, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/events", strings.NewReader(body))
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

const validEvent = `{"event_id":"run-abc12345","agent":"claude-code","occurred_at":"2026-08-25T10:00:00Z","duration_ms":1200,"outcome":"success","cost_micros":41230}`

func TestValidEventIsAcceptedAndPublished(t *testing.T) {
	h, rec := harness(t)
	w := post(t, h, apiKey, validEvent)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", w.Code, w.Body)
	}
	if rec.Count() != 1 {
		t.Fatalf("want 1 message on the log, got %d", rec.Count())
	}
	if got := string(rec.Messages[0].Key); got != tenant {
		t.Errorf("partition key should be the tenant, got %q", got)
	}
	if rec.Messages[0].Topic != TopicEvents {
		t.Errorf("wrong topic: %q", rec.Messages[0].Topic)
	}
}

func TestNoKeyIs401AndPublishesNothing(t *testing.T) {
	h, rec := harness(t)
	w := post(t, h, "", validEvent)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "problem+json") {
		t.Errorf("want problem+json, got %q", ct)
	}
	if rec.Count() != 0 {
		t.Fatal("an unauthenticated event reached the log")
	}
}

func TestWrongKeyIs401(t *testing.T) {
	h, rec := harness(t)
	if w := post(t, h, "nk_not_the_right_key_at_all", validEvent); w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
	if rec.Count() != 0 {
		t.Fatal("an event with a bad key reached the log")
	}
}

// The done criterion from the spec, asserted rather than assumed.
func TestInvalidEventNeverReachesTheLog(t *testing.T) {
	bodies := map[string]string{
		"unknown field carrying a path": `{"event_id":"run-abc12345","agent":"cursor","occurred_at":"2026-08-25T10:00:00Z","duration_ms":5,"outcome":"success","file_path":"/Users/can/secret/keys.ts"}`,
		"unknown agent":                 `{"event_id":"run-abc12345","agent":"my-tool","occurred_at":"2026-08-25T10:00:00Z","duration_ms":5,"outcome":"success"}`,
		"bad timestamp":                 `{"event_id":"run-abc12345","agent":"cursor","occurred_at":"yesterday","duration_ms":5,"outcome":"success"}`,
		"negative duration":             `{"event_id":"run-abc12345","agent":"cursor","occurred_at":"2026-08-25T10:00:00Z","duration_ms":-5,"outcome":"success"}`,
		"malformed json":                `{"event_id":`,
	}
	for name, body := range bodies {
		h, rec := harness(t)
		w := post(t, h, apiKey, body)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: want 422, got %d (%s)", name, w.Code, strings.TrimSpace(w.Body.String()))
		}
		if rec.Count() != 0 {
			t.Errorf("%s: an invalid event reached the log", name)
		}
	}
}

// The rejection body is returned to the client and copied into logs. It must
// name the field and never repeat the value.
func TestRejectionNeverEchoesTheOffendingValue(t *testing.T) {
	secret := "/Users/can/Developer/acme-private/.env"
	body := `{"event_id":"run-abc12345","agent":"cursor","occurred_at":"2026-08-25T10:00:00Z","duration_ms":5,"outcome":"success","file_path":"` + secret + `"}`

	h, _ := harness(t)
	w := post(t, h, apiKey, body)

	if strings.Contains(w.Body.String(), secret) {
		t.Fatalf("the response echoed the rejected value: %s", w.Body)
	}
	if !strings.Contains(w.Body.String(), "file_path") {
		t.Errorf("the response should name the field: %s", w.Body)
	}
}

// A broker that is down must not look like a bad request: the client has to
// know this one is worth retrying.
func TestBrokerFailureIs503WithRetryAfter(t *testing.T) {
	keys := auth.NewKeys()
	keys.Add(tenant, apiKey)
	rec := &kafka.Recorder{Err: io.ErrClosedPipe}
	h := New(keys, rec, slog.New(slog.NewTextHandler(io.Discard, nil))).Routes()

	w := post(t, h, apiKey, validEvent)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("503 without Retry-After leaves the client guessing")
	}
}

// Only fields the contract knows may appear on the log, whatever the client sent.
func TestPublishedPayloadCarriesOnlyContractFields(t *testing.T) {
	h, rec := harness(t)
	post(t, h, apiKey, validEvent)

	var got map[string]any
	if err := json.Unmarshal(rec.Messages[0].Value, &got); err != nil {
		t.Fatalf("published payload is not JSON: %v", err)
	}
	allowed := map[string]bool{
		"event_id": true, "agent": true, "agent_version": true, "session_id": true,
		"repo_hash": true, "occurred_at": true, "duration_ms": true, "outcome": true,
		"error_kind": true, "model": true, "tokens_in": true, "tokens_out": true,
		"cost_micros": true, "files_touched": true, "lines_added": true,
		"lines_removed": true, "tool_calls": true,
	}
	for k := range got {
		if !allowed[k] {
			t.Errorf("published payload carries %q, which is not in agent_run.v1", k)
		}
	}
}

func TestHealthAndReady(t *testing.T) {
	h, _ := harness(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("%s: want 200, got %d", path, w.Code)
		}
	}
}
