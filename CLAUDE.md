# nine-ingest

The edge: the only service the outside world talks to. Go 1.25, no framework.

## What this repo is for

Accepting or refusing events, quickly, and being the privacy boundary while doing it. Nothing is processed here. If a change adds processing to this service, it belongs in `nine-core` instead.

## The boundary that must hold

`agent_run.v1` accepts counts, durations, hashes and closed enums. It has no field that can carry a file path, a prompt fragment, a repository name or a secret, and `additionalProperties` is `false`. Three properties follow, and each is a test that must keep passing:

**Unknown fields are rejected, not stored.** Adding a permissive field to the contract to "make integration easier" is the failure mode this design exists to prevent.

**Rejections never echo the offending value.** The response and the log name the field, never its content. The field that was rejected is the one most likely to hold a key.

**Only contract fields are published.** The accepted event is re-encoded from the validated struct, not forwarded as received.

## Decisions that are not up for casual revision

**Validation is a typed struct with `DisallowUnknownFields`, not a schema walk.** Interpreting the contract per request costs an order of magnitude more on the hot path. This is only safe because `schema_sync_test.go` fails the build when the struct drifts from the embedded contract. If you remove that test, the design collapses: put the schema interpreter back first.

**`Publish` waits for the broker.** A service that answers `202` has to mean the event is durable. Do not switch to fire-and-forget for latency.

**The tenant is the partition key.** One tenant's events stay ordered on one partition.

**`readyz` is not `healthz`.** Alive is not the same as able to accept events. The broker check belongs in `readyz`.

## Contract changes

The canonical contract lives in `nine-platform/contracts/events/`. The copy here is vendored because Go's embed cannot reach outside its package, and CI fails when the two differ. Change the canonical one first, then vendor it, in the same pull request.

Removing a field, narrowing an enum or tightening a pattern is a new schema version, not an edit. Producers and consumers overlap until every client has moved.

## Testing

`go test ./...` must be green, `gofmt -l .` must be empty, `go vet ./...` clean. The Kafka `Recorder` lets the accept path be tested without a broker; use it rather than skipping the "nothing invalid reaches the log" assertions.

## Rules every Nine repo shares

**Language.** Code, comments, commit messages, docs and UI strings are English. No exceptions, including in files nobody reads yet.

**No em dashes.** Commas and colons instead. The pre-commit hook blocks them.

**Commits.** `type(scope): message`, one line, no `Co-Authored-By`, no generator trailers. Enforced by `githooks/commit-msg`.

**Never `--no-verify`.** The hooks are the control, not a suggestion. If a hook is wrong, fix the hook in the same commit.

**Secrets.** Nothing that authenticates anything is committed, ever: keys, tokens, certificates, `.env`, connection strings with an inline password. `githooks/pre-commit` scans the staged diff and refuses. A documented example that trips the scanner ends its line with `nine:allow-secret`; a real value never does.

**After cloning:** `./githooks/install.sh` once, then `brew install gitleaks`.

**Claims carry numbers.** A README that says something is fast links to the run that measured it. No number, no claim.
