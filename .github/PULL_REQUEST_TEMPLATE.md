<!-- Thank you for contributing. Keep the change set focused - one logical change per PR. -->

## What

<!-- What does this PR change? -->

## Why

<!-- Link the issue, or describe the motivation. -->

## How it was tested

<!-- Real verification, not just "it compiles": which tests ran, what you exercised manually (runtime, platform), and any protocol impact (capability negotiation, PROTOCOL.md updates). -->

## Checklist

- [ ] `gofmt` clean (`test -z "$(gofmt -l .)"`)
- [ ] `go vet ./...` and `go test -race ./...` pass
- [ ] Docs updated where behaviour changed (README, docs/, protocol/PROTOCOL.md)
- [ ] CHANGELOG.md has an entry under Unreleased
- [ ] New protocol messages bump capability advertisement and are documented
