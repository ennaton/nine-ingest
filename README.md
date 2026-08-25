# nine-ingest

[![ci](https://github.com/ennaton/nine-ingest/actions/workflows/ci.yml/badge.svg)](https://github.com/ennaton/nine-ingest/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](./LICENSE)

The edge of [Nine](https://github.com/ennaton/nine-docs): the only service the outside world talks to. It authenticates the caller, validates the event against `agent_run.v1`, writes it to Kafka, and returns. Nothing is processed here.

Go 1.25, no framework.

## Status

v0. HTTP intake, API-key auth, contract validation and Kafka publishing work and are covered by tests against a real broker. Not yet done: rate limiting, gRPC, OpenTelemetry.

## Why the edge is this thin

Everything downstream reads the log, so this service's p99 is the broker write and nothing else. An event that gets `202` is already durable: `Publish` waits for the broker to acknowledge rather than returning on a queued batch, because a service that says "accepted" has to mean it.

## The contract is the privacy boundary

`agent_run.v1` accepts counts, durations, hashes and closed enums. There is no field anywhere that can carry a file path, a prompt fragment, a repository name or a secret, and `additionalProperties` is `false`.

Three properties fall out of that, and each one is a test:

**Unknown fields are rejected, not stored.** `file_path`, `prompt`, `error_message`, `stack_trace`: all `422`, none reach Kafka.

**The rejection never echoes the value.** The response and the log name the offending *field* so a client can fix its payload, and never repeat what was in it. The field that was rejected is the one most likely to hold a key.

**Only contract fields are published.** The accepted event is re-encoded from the validated struct rather than forwarded as received, so unknown data cannot survive even if validation were bypassed.

## Validation is a struct, not a schema walk

Requests are decoded into a typed struct with `DisallowUnknownFields`, which is how `additionalProperties: false` is enforced. Interpreting the JSON Schema document per request would cost an order of magnitude more on the hot path.

That would normally trade safety for speed. It does not here, because `schema_sync_test.go` reads the embedded contract and fails the build if the struct's fields, its required set or its enum values drift from it. The test is verified to work: temporarily adding a field and an enum value to the contract makes it fail with both named.

## Run it

```bash
# broker from nine-platform
(cd ../platform && docker compose up -d kafka)

export NINE_INGEST_KEYS="11111111-1111-1111-1111-111111111111:nk_dev_key_123456"
export NINE_KAFKA_BROKERS="localhost:19092"
go run ./cmd/ingest        # :18082
```

```bash
curl -X POST localhost:18082/v1/events \
  -H "X-Api-Key: nk_dev_key_123456" -H 'content-type: application/json' \
  -d '{"event_id":"run-abc12345","agent":"claude-code","occurred_at":"2026-08-25T10:00:00Z",
       "duration_ms":4210,"outcome":"success","tokens_in":18400,"cost_micros":91500,"files_touched":7}'
# 202 {"event_id":"run-abc12345","status":"accepted"}
```

Watch it land:

```bash
(cd ../platform && docker compose exec kafka \
  /opt/kafka/bin/kafka-console-consumer.sh --bootstrap-server localhost:9092 \
  --topic events --from-beginning)
```

## API

| Method | Path | Behaviour |
|---|---|---|
| `POST` | `/v1/events` | `202` accepted and on the log · `401` no or bad key · `422` fails the contract · `503` broker unavailable, with `Retry-After` |
| `GET` | `/healthz` | process is alive |
| `GET` | `/readyz` | able to accept events. Separate from `healthz` on purpose: alive is not the same as ready |

Errors are `application/problem+json`, the same shape as `nine-billing`.

## Layout

```
cmd/ingest/          main: config, server lifecycle, graceful shutdown
internal/event/      AgentRun struct, validation, the contract sync test
internal/auth/       API key to tenant, digests only, constant-time compare
internal/kafka/      Producer interface, franz-go client, test recorder
internal/httpapi/    routes, problem+json, the accept path
```

The contract is vendored under `internal/event/schema/` because Go's embed cannot reach outside its package. `nine-platform/contracts` holds the canonical copy and CI fails if the two differ.

## License

MIT. See [LICENSE](./LICENSE).
