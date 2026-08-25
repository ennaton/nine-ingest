package event

import (
	"strings"
	"testing"
)

func decode(t *testing.T, body string) (*AgentRun, error) {
	t.Helper()
	return Decode(strings.NewReader(body))
}

const good = `{
  "event_id": "run-abc12345",
  "agent": "claude-code",
  "occurred_at": "2026-08-25T10:00:00Z",
  "duration_ms": 1200,
  "outcome": "success",
  "tokens_in": 900,
  "cost_micros": 41230,
  "files_touched": 3
}`

func TestValidEventIsAccepted(t *testing.T) {
	a, err := decode(t, good)
	if err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	if a.EventID != "run-abc12345" || *a.CostMicros != 41230 {
		t.Fatalf("decoded wrong: %+v", a)
	}
}

// The privacy boundary. Each of these is a field someone would plausibly add,
// and each one is exactly the kind of field that carries a path, a prompt or a
// key. None of them may be accepted, and the error may not echo the value.
func TestUnknownFieldsAreRejectedAndNeverEchoed(t *testing.T) {
	secrets := map[string]string{
		"file_path":     "/Users/can/Developer/secret-client/src/keys.ts",
		"prompt":        "here is my api key sk-abcdefghijklmnop",
		"repo_name":     "acme-corp/private-billing",
		"error_message": "failed to auth with token ghp_realtokenvalue",
		"stack_trace":   "at /Users/can/.ssh/id_rsa",
	}
	for field, value := range secrets {
		body := `{"event_id":"run-abc12345","agent":"cursor","occurred_at":"2026-08-25T10:00:00Z",` +
			`"duration_ms":10,"outcome":"success","` + field + `":"` + value + `"}`

		_, err := decode(t, body)
		if err == nil {
			t.Fatalf("%s was accepted; the schema boundary is not holding", field)
		}
		if strings.Contains(err.Error(), value) {
			t.Fatalf("%s: the error echoed the value, which puts it in the logs: %v", field, err)
		}
		if !strings.Contains(err.Error(), field) {
			t.Errorf("%s: the error should name the field so a client can fix it, got %v", field, err)
		}
	}
}

func TestEnumsAreClosed(t *testing.T) {
	cases := []struct{ name, body string }{
		{"unknown agent", `{"event_id":"run-abc12345","agent":"my-own-tool","occurred_at":"2026-08-25T10:00:00Z","duration_ms":5,"outcome":"success"}`},
		{"unknown outcome", `{"event_id":"run-abc12345","agent":"codex","occurred_at":"2026-08-25T10:00:00Z","duration_ms":5,"outcome":"exploded"}`},
		{"unknown error_kind", `{"event_id":"run-abc12345","agent":"codex","occurred_at":"2026-08-25T10:00:00Z","duration_ms":5,"outcome":"error","error_kind":"disk_on_fire"}`},
	}
	for _, tc := range cases {
		if _, err := decode(t, tc.body); err == nil {
			t.Errorf("%s: accepted, should be rejected", tc.name)
		}
	}
}

func TestHashFieldsMustBeHashes(t *testing.T) {
	// A client that sends the repository name instead of its digest is caught
	// by the pattern, which is the second line of defence after the SDK.
	body := `{"event_id":"run-abc12345","agent":"cursor","occurred_at":"2026-08-25T10:00:00Z",` +
		`"duration_ms":5,"outcome":"success","repo_hash":"acme-corp/private-billing"}`
	if _, err := decode(t, body); err == nil {
		t.Fatal("a plain repository name passed as repo_hash was accepted")
	}
}

func TestBoundsAreEnforced(t *testing.T) {
	cases := []string{
		`{"event_id":"run-abc12345","agent":"cursor","occurred_at":"2026-08-25T10:00:00Z","duration_ms":-5,"outcome":"success"}`,
		`{"event_id":"run-abc12345","agent":"cursor","occurred_at":"2026-08-25T10:00:00Z","duration_ms":99999999,"outcome":"success"}`,
		`{"event_id":"short","agent":"cursor","occurred_at":"2026-08-25T10:00:00Z","duration_ms":5,"outcome":"success"}`,
		`{"event_id":"run-abc12345","agent":"cursor","occurred_at":"not-a-time","duration_ms":5,"outcome":"success"}`,
		`{"event_id":"run-abc12345","agent":"cursor","occurred_at":"2026-08-25T10:00:00Z","duration_ms":5,"outcome":"success","tokens_in":-1}`,
	}
	for i, body := range cases {
		if _, err := decode(t, body); err == nil {
			t.Errorf("case %d accepted, should be rejected", i)
		}
	}
}

func TestOversizedBodyIsCut(t *testing.T) {
	// A 2 MiB body must not be buffered whole; the limit reader truncates and
	// the decode fails rather than the process growing.
	body := `{"event_id":"run-abc12345","agent":"cursor","model":"` + strings.Repeat("x", 2<<20) + `"}`
	if _, err := decode(t, body); err == nil {
		t.Fatal("a 2 MiB body was accepted")
	}
}
