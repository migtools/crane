# Crane CLI logging and I/O standards

This document defines **engineering conventions** for logging and terminal output in the [crane](https://github.com/konveyor/crane) repository (`cmd/`, `internal/`). It complements user-facing observability work (for example [issue #199](https://github.com/konveyor/crane/issues/199)) by keeping **streams**, **levels**, and **logger usage** consistent so phased progress, summaries, and optional machine-readable output can be added without fighting the codebase.

## Scope

- **In scope:** Crane CLI commands, shared `internal/` helpers, how we integrate libraries that need **logr** or custom formatters.
- **Related repo [crane-lib](https://github.com/konveyor/crane-lib):** accept an injected `logrus.FieldLogger` or `*logrus.Logger`. **Log message text is not a public API**—downstream must not rely on exact strings.
- **Non-goal:** This document does not mandate OpenTelemetry or a specific vendor stack; those remain optional future work.

## Relationship to CLI observability refactors

Planned UX improvements include **phase banners**, **end-of-run summaries**, **throttled or single-line progress on TTY**, and optionally **NDJSON** for automation. Those features should:

- Respect the **stdout vs stderr contract** below.
- Prefer **reducing default `Info` noise** by moving per-item lines to `**Debug`** (enabled with global `--debug` from `[internal/flags/global_flags.go](../internal/flags/global_flags.go)`).
- Implement **presentation** either by logging through the same logrus logger or via a small UI helper writing to `**IOStreams.ErrOut`**—not ad hoc `fmt.Print` to arbitrary streams.

Adding `--progress=json` (or similar) **does not require replacing logrus**; use an additional hook or `io.Writer` as long as machine output stays **opt-in** and documented.

## Stdout vs stderr


| Stream     | Use for                                                                                                                                                                      |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **stderr** | Diagnostics, progress, and all **logrus** output by default. The name *standard error* is misleading: it is the normal channel for human-oriented status, not only failures. |
| **stdout** | **Pipeable or explicitly machine-primary** output only (e.g. a future stable JSON document or stream users are told to redirect).                                            |


**Rules:**

- Do not mix unstructured status logs into stdout if stdout might be parsed or piped.
- **Exception (today):** `crane transfer-pvc` drives an **rsync progress block** on stdout with ANSI updates in `[cmd/transfer-pvc/progress.go](../cmd/transfer-pvc/progress.go)`. Treat this as **legacy behavior** to reconcile when aligning transfer-pvc with these standards—not as a pattern to copy for new commands.

**Future NDJSON / progress events:** Prefer **stderr** or a **dedicated file**; if stdout is ever used for progress JSON, it must be **opt-in** and documented.

## How to obtain and pass loggers

1. **Root / global flags:** `[GlobalFlags](../internal/flags/global_flags.go)` provides `ApplyFlags`, `GetLogger()`, and `--debug`. Subcommands wired from `[main.go](../main.go)` should receive the same `*flags.GlobalFlags` instance (after viper merge in `PreRun`) where possible.
2. **In command `Run`:** `log := o.globalFlags.GetLogger()` (or equivalent after unmarshaling).
3. **In helpers:** take `logrus.FieldLogger` (or `*logrus.Logger` when hooks are needed) and pass it from the command—see `[cmd/export/discover.go](../cmd/export/discover.go)` and `[cmd/export/cluster.go](../cmd/export/cluster.go)`.
4. **Transform + crane-lib:** pass the command logger into `transform.Runner` (e.g. `Log: log.WithField("command", "transform").Logger` in `[cmd/transform/transform.go](../cmd/transform/transform.go)`).

**Avoid** for new user-facing messages:

- Standard library `log` / `log.Printf` / `log.Fatal` except during deliberate migration or minimal bootstrap.
- `fmt.Print*` / `fmt.Fprintf(os.Stdout, …)` for routine status (debug dumps in tests or truly diagnostic tools are separate).

`**transfer-pvc` today** uses `log`, `log.Fatal`, and a **logr → logrus JSON** bridge for pvc-transfer internals; converging this command to `GlobalFlags` and shared formatting is expected work aligned with observability, not a new exception.

## Log levels


| Level     | When to use                                                                                                                                                                                     |
| --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Error** | Failure that should surface clearly; combine with returning an error from `RunE` where appropriate. Reserve process-killing `Fatal` for top-level unrecoverable cases if used at all.           |
| **Warn**  | Continued execution with degradation (partial discovery, ignored write error, permission issue on one type).                                                                                    |
| **Info**  | High-signal steps a normal user should see at default verbosity: milestones, counts, completion. **Avoid** per-resource spam at Info when hundreds of lines are expected—use **Debug** instead. |
| **Debug** | Per-type / per-file enumeration, skipped kinds, patch/plugin conflict detail, byte counts—anything gated by `--debug`.                                                                          |


Refactors that move chatty **Info** → **Debug** are **encouraged** and match the observability roadmap.

## Structured fields

Use `WithField` / `WithFields` for stable dimensions, for example:

- `command` (subcommand name)
- `phase` or `step` (when phase-based UX exists)
- `namespace`, `export-dir`, or other repeated context

This keeps text output readable today and eases a future JSON formatter or centralized scrubbing without changing call sites.

## Message style

- Prefer **lowercase** after the logrus prefix for new messages (match surrounding code when editing a file).
- Prefer **no embedded `\n`** in format strings for new code; let the logger add line breaks.
- **Tests and scripts must not assert** on full human log lines; assert on **returned errors**, **exit codes**, or **file artifacts**.

## Libraries that require logr

When a dependency expects **logr**:

- Bridge **once** at the command boundary (e.g. logrusr + logrus).
- On an interactive TTY, prefer **human-readable** logrus text for the user-visible logger; JSON formatting is acceptable for **non-TTY** or **explicit** machine mode if documented.
- Keep library JSON logs from **interleaving awkwardly** with human progress (ordering and formatter choice are part of transfer-pvc alignment work).

## Progress UI (ANSI, carriage return)

- Restrict **in-place** terminal updates to **TTY** sessions (detect via `isatty` or equivalent when implemented).
- Coordinate with log lines: either serialize writes, use stderr for logs and isolate progress, or document a single writer mutex—avoid corrupted interleaved output.

## Checklist for new or heavily touched commands

- Command receives `**GlobalFlags`** (or documented reason why not) and uses `**GetLogger()**`.
- Helpers receive `**logrus.FieldLogger**` (or logger interface) instead of creating ad hoc loggers.
- **Info** is not used for high-volume per-item lines; those are **Debug**.
- **Stdout** is not used for unstructured logs; **stderr** / logrus for status.
- No new dependency on **exact log text** from tests or docs.

## References

- Global flags and logger: `[internal/flags/global_flags.go](../internal/flags/global_flags.go)`
- Root command wiring: `[main.go](../main.go)`
- Observability UX direction: [issue #199](https://github.com/konveyor/crane/issues/199) (and any in-repo `docs/crane-observability.md` once added)

