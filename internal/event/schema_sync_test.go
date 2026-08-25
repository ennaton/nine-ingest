package event

import (
	_ "embed"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

//go:embed schema/agent_run.v1.json
var contractJSON []byte

type contract struct {
	AdditionalProperties bool     `json:"additionalProperties"`
	Required             []string `json:"required"`
	Properties           map[string]struct {
		Type    string   `json:"type"`
		Enum    []string `json:"enum"`
		Pattern string   `json:"pattern"`
		Minimum *int64   `json:"minimum"`
		Maximum *int64   `json:"maximum"`
	} `json:"properties"`
}

func load(t *testing.T) contract {
	t.Helper()
	var c contract
	if err := json.Unmarshal(contractJSON, &c); err != nil {
		t.Fatalf("embedded contract is not valid JSON: %v", err)
	}
	return c
}

// The struct is the fast path; this test is why trusting it is not naive.
// If someone adds a field to the schema and forgets the struct, or the other
// way round, the build goes red here instead of silently dropping data.
func TestStructMatchesContractFields(t *testing.T) {
	c := load(t)

	inStruct := map[string]bool{}
	rt := reflect.TypeOf(AgentRun{})
	for i := 0; i < rt.NumField(); i++ {
		name := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			inStruct[name] = true
		}
	}

	for field := range c.Properties {
		if !inStruct[field] {
			t.Errorf("contract has %q but AgentRun does not: incoming data for it would be rejected", field)
		}
	}
	for field := range inStruct {
		if _, ok := c.Properties[field]; !ok {
			t.Errorf("AgentRun has %q but the contract does not: the struct is accepting more than the contract allows", field)
		}
	}
}

func TestRequiredFieldsMatch(t *testing.T) {
	c := load(t)
	want := append([]string(nil), c.Required...)
	sort.Strings(want)

	// Every required field must fail validation when absent.
	for _, field := range want {
		a := valid()
		clear(&a, field)
		if err := a.Validate(); err == nil {
			t.Errorf("contract requires %q but Validate accepts an event without it", field)
		}
	}
}

func TestEnumsMatchContract(t *testing.T) {
	c := load(t)
	cases := []struct {
		field string
		got   map[string]struct{}
	}{
		{"agent", agents},
		{"outcome", outcomes},
		{"error_kind", errorKinds},
	}
	for _, tc := range cases {
		want := c.Properties[tc.field].Enum
		if len(want) != len(tc.got) {
			t.Errorf("%s: contract has %d values, code has %d", tc.field, len(want), len(tc.got))
		}
		for _, v := range want {
			if _, ok := tc.got[v]; !ok {
				t.Errorf("%s: contract allows %q, code rejects it", tc.field, v)
			}
		}
	}
}

// additionalProperties:false is enforced by DisallowUnknownFields, not by a
// schema walk. This asserts the contract still says what the code assumes.
func TestContractStillForbidsUnknownFields(t *testing.T) {
	if c := load(t); c.AdditionalProperties {
		t.Fatal("contract now allows additional properties; DisallowUnknownFields is no longer the right enforcement")
	}
}

func valid() AgentRun {
	return AgentRun{
		EventID:    "run-abc12345",
		Agent:      "claude-code",
		OccurredAt: "2026-08-25T10:00:00Z",
		DurationMs: 1200,
		Outcome:    "success",
	}
}

func clear(a *AgentRun, field string) {
	switch field {
	case "event_id":
		a.EventID = ""
	case "agent":
		a.Agent = ""
	case "occurred_at":
		a.OccurredAt = ""
	case "duration_ms":
		a.DurationMs = -1 // absent is indistinguishable from 0 here; out of range stands in
	case "outcome":
		a.Outcome = ""
	}
}
