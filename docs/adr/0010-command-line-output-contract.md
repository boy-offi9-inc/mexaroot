# 0010. Command-line output contract

Status: Accepted

## Context

The client is used interactively on a terminal, inside shell scripts, and by automation that parses its output. Each audience needs different output from the same commands.

## Decision

Data goes to standard output, while diagnostics, prompts, and menus go to standard error. Styling and tables appear only when the destination is a terminal, color follows the `NO_COLOR` and `FORCE_COLOR` conventions, and piped output is plain, with records separated by tabs where a command lists them. The `-json` flag switches supported commands to a JSON object on standard output and reports errors as a JSON object on standard error. `exec` exits with the exit code of the remote command, and every other failure exits non-zero.

## Consequences

- Scripts and tools can rely on stable output without scraping styled text.
- Human-oriented formatting can change without breaking automation.
- Each command that prints data needs a JSON shape that is maintained alongside its text form.
