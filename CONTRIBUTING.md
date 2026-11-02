# Contributing

Contributions are welcome. By submitting one you agree that it is licensed under the Apache License, Version 2.0, as described in section 5 of the license.

## Workflow

1. Open an issue to discuss any change that affects behavior, the protocol, or the architecture.
2. Branch from `main` and keep each commit focused on one logical change.
3. Write commit messages with a conventional prefix such as `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, or `chore:`.
4. Open a pull request that explains what changed and why.

## Standards

- Code is self-explanatory. Add a comment only where the reason for a decision is not evident from the code.
- Behavior changes include tests.
- Changes to the wire protocol update `protocol/PROTOCOL.md` in the same pull request.
- Decisions with long-term consequences are recorded as a new ADR under `docs/adr`.
