// Package event decodes and validates agent_run.v1 events.
//
// Validation is a typed struct with strict decoding rather than a JSON Schema
// interpreter. Two reasons, in this order:
//
//  1. This is the ingest hot path. Interpreting a schema document per request
//     costs an order of magnitude more than decoding into a struct, and the
//     whole service exists to keep p99 low.
//  2. The guarantee is not weaker, because it is not on trust: schema_sync_test
//     reads the embedded contract and fails if the struct's fields, required
//     set or enum values drift from it. Drift is a red build, not a surprise
//     in production.
//
// The contract file is vendored here because Go's embed cannot reach outside
// its package. nine-platform holds the canonical copy; the sync test is what
// keeps this one honest.
package event

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"time"
)

// AgentRun mirrors contracts/events/agent_run.v1.json exactly. Every field is a
// pointer or a value chosen so that "absent" and "zero" stay distinguishable
// where that matters.
type AgentRun struct {
	EventID      string `json:"event_id"`
	Agent        string `json:"agent"`
	AgentVersion string `json:"agent_version,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	RepoHash     string `json:"repo_hash,omitempty"`
	OccurredAt   string `json:"occurred_at"`
	DurationMs   int64  `json:"duration_ms"`
	Outcome      string `json:"outcome"`
	ErrorKind    string `json:"error_kind,omitempty"`
	Model        string `json:"model,omitempty"`
	TokensIn     *int64 `json:"tokens_in,omitempty"`
	TokensOut    *int64 `json:"tokens_out,omitempty"`
	CostMicros   *int64 `json:"cost_micros,omitempty"`
	FilesTouched *int64 `json:"files_touched,omitempty"`
	LinesAdded   *int64 `json:"lines_added,omitempty"`
	LinesRemoved *int64 `json:"lines_removed,omitempty"`
	ToolCalls    *int64 `json:"tool_calls,omitempty"`
}

var (
	reEventID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	reHex64   = regexp.MustCompile(`^[a-f0-9]{64}$`)
	reVersion = regexp.MustCompile(`^[0-9A-Za-z.+-]{1,32}$`)
	reModel   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

	agents     = set("claude-code", "cursor", "codex", "copilot", "aider", "other")
	outcomes   = set("success", "error", "cancelled", "timeout")
	errorKinds = set("rate_limit", "context_limit", "tool_failure", "network", "auth", "user_abort", "unknown")
)

func set(vs ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(vs))
	for _, v := range vs {
		m[v] = struct{}{}
	}
	return m
}

// ValidationError names the offending field. It never echoes the offending
// value: an unknown field is the one most likely to hold a secret, and an error
// body is a place values leak into logs.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("field %q: %s", e.Field, e.Reason)
}

const maxBodyBytes = 1 << 20 // 1 MiB per request

// Decode reads one event with unknown fields rejected, which is how
// additionalProperties:false is enforced here.
func Decode(r io.Reader) (*AgentRun, error) {
	dec := json.NewDecoder(io.LimitReader(r, maxBodyBytes))
	dec.DisallowUnknownFields()

	var a AgentRun
	if err := dec.Decode(&a); err != nil {
		// json's message for an unknown field includes the field name but not
		// its value, which is what we want to surface and log.
		return nil, &ValidationError{Field: "body", Reason: sanitizeDecodeError(err)}
	}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return &a, nil
}

func (a *AgentRun) Validate() error {
	if !reEventID.MatchString(a.EventID) {
		return &ValidationError{"event_id", "must be 8 to 64 characters of A-Z a-z 0-9 _ -"}
	}
	if _, ok := agents[a.Agent]; !ok {
		return &ValidationError{"agent", "not one of the known agents"}
	}
	if _, ok := outcomes[a.Outcome]; !ok {
		return &ValidationError{"outcome", "not one of success, error, cancelled, timeout"}
	}
	if a.ErrorKind != "" {
		if _, ok := errorKinds[a.ErrorKind]; !ok {
			return &ValidationError{"error_kind", "not one of the known error kinds"}
		}
	}
	if a.DurationMs < 0 || a.DurationMs > 86_400_000 {
		return &ValidationError{"duration_ms", "must be between 0 and 86400000"}
	}
	if _, err := time.Parse(time.RFC3339, a.OccurredAt); err != nil {
		return &ValidationError{"occurred_at", "must be an RFC 3339 timestamp"}
	}
	if a.SessionID != "" && !reHex64.MatchString(a.SessionID) {
		return &ValidationError{"session_id", "must be a lowercase hex SHA-256 digest"}
	}
	if a.RepoHash != "" && !reHex64.MatchString(a.RepoHash) {
		return &ValidationError{"repo_hash", "must be a lowercase hex SHA-256 digest"}
	}
	if a.AgentVersion != "" && !reVersion.MatchString(a.AgentVersion) {
		return &ValidationError{"agent_version", "contains characters the contract does not allow"}
	}
	if a.Model != "" && !reModel.MatchString(a.Model) {
		return &ValidationError{"model", "contains characters the contract does not allow"}
	}
	for _, f := range []struct {
		name string
		v    *int64
	}{
		{"tokens_in", a.TokensIn}, {"tokens_out", a.TokensOut}, {"cost_micros", a.CostMicros},
		{"files_touched", a.FilesTouched}, {"lines_added", a.LinesAdded},
		{"lines_removed", a.LinesRemoved}, {"tool_calls", a.ToolCalls},
	} {
		if f.v != nil && *f.v < 0 {
			return &ValidationError{f.name, "must not be negative"}
		}
	}
	return nil
}

// sanitizeDecodeError keeps the field name and drops anything that could be a
// value copied out of the request body.
func sanitizeDecodeError(err error) string {
	msg := err.Error()
	if m := regexp.MustCompile(`unknown field "([^"]+)"`).FindStringSubmatch(msg); m != nil {
		return fmt.Sprintf("unknown field %q is not part of agent_run.v1", m[1])
	}
	if regexp.MustCompile(`cannot unmarshal`).MatchString(msg) {
		return "a field has the wrong type"
	}
	return "malformed JSON"
}
