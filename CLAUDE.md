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

**Language.** Code, comments, commit messages, docs, artifacts and UI strings are English. No exceptions, including in files nobody reads yet, and including an artifact whose subject was discussed in another language: it is written in English at the moment it is written, not translated afterwards. `githooks/pre-commit` blocks the added lines and the `house-style` CI job scans the whole tree, so a file that predates the rule fails the build until it is rewritten.

**No em dashes.** Commas and colons instead. The pre-commit hook blocks them.

**Commits.** `type(scope): message`, one line, no generator trailers. Enforced by `githooks/commit-msg`.

**Co-authorship is for people.** A commit two of you wrote carries `Co-Authored-By` for the other one. A ping-pong group hands a file back and forth and the commit lands under whoever happened to be holding it, so without the trailer half the work is invisible in the only place it gets counted. The trailer goes in its own block at the end, after a blank line, and the address has to be one GitHub already knows for that person or no credit is applied. A tool is not an author: the same hook that allows the human trailer refuses one naming Claude, Copilot or a bot.

**Never `--no-verify`.** The hooks are the control, not a suggestion. If a hook is wrong, fix the hook in the same commit.

**Secrets.** Nothing that authenticates anything is committed, ever: keys, tokens, certificates, `.env`, connection strings with an inline password. `githooks/pre-commit` scans the staged diff and refuses. A documented example that trips the scanner ends its line with `nine:allow-secret`; a real value never does.

**After cloning:** `./githooks/install.sh` once, then `brew install gitleaks`.

**Claims carry numbers.** A README that says something is fast links to the run that measured it. No number, no claim.

**Prose carries its mechanism.** A sentence explaining why something works is a claim, and it carries the same burden as a number. A comment reading "this handler wins because it is declared first" was wrong: Spring ranks candidates with `ExceptionDepthComparator` and position in the file means nothing. Checking produced the stronger sentence, that nobody can break the distinction by reordering the methods. If the mechanism was not checked, the sentence does not get written.

**Artifacts.** Generated output, a report, a dashboard, an analysis, a plan, a diagram, lands in `docs/artifacts/` inside a repo, and in `artifacts/` in `nine-docs`, where the repo is already docs. Never a repo root, and never the parent `nine/` folder, which is not a repository and therefore not version control. An artifact about one repo lives in that repo. An artifact about more than one lives in `nine-platform/docs/artifacts/`. Files are named `YYYY-MM-DD-subject.ext`, lowercase and hyphenated.

**Write the artifact where it belongs, on the first write.** The path is chosen before the file exists. No scratchpad draft, no `/tmp` staging, no repo root copy that gets moved later. This overrides any assistant default about putting generated files in a temporary directory: here the artifact directory is the working directory, it is version controlled, and an artifact nobody committed did not happen.

**An artifact is not a document.** `nine-docs` holds decisions and measurement reports: authored, reviewed, permanent, and bound by the rules above. An artifact is dated working output that nothing else is allowed to cite. When one earns permanence it is rewritten as an ADR or a report in `nine-docs` and the artifact is deleted, not copied. The same content living in two paths is the failure this rule exists to prevent.

**A change is finished when what it unblocks is stated.** The acceptance criterion is where the work stops, not where the thinking stops. `findAccount` selected on `(tenant, code)` and took the first row with no `ORDER BY`, which was correct until the change that widened that exact key, written by the same hand in the same week. Before closing anything, answer in writing: what is now true that was not, and who is waiting on it. If nothing is unblocked, say so, because silence reads the same as not having looked. This rule and the two around it came from a run of defects with one shape in common: every statement behind them was individually true, and none was carried to its consequence.

**The edit is not the boundary of what you read.** Read the whole method you are touching, not the line you came for. Two defects found in one review sat within five lines of a change that was itself correct: a balance response took its number from the computed money and its label from the request string, and the two agreed only because nothing had yet made them disagree.
