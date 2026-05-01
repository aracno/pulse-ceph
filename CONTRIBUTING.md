# Contributing to Pulse

Pulse is maintained as a single-maintainer project.

I am not accepting unsolicited external pull requests for this repository.
If you have found a bug, want to propose a feature, or have a concrete
improvement idea, please open an issue instead.

## What To Open

- Bug reports: use the bug report issue form and include exact reproduction
  steps, Pulse version, installation type, and any relevant logs or diagnostics.
- Feature requests: open an issue describing the problem you want solved, the
  workflow you are trying to improve, and any constraints that matter.
- Questions and support requests: use GitHub Discussions when you need help,
  troubleshooting, or general guidance rather than a tracked defect.
- Security issues: follow [SECURITY.md](SECURITY.md) instead of opening a public
  report for sensitive problems.

## Pull Request Policy

- External pull requests are not part of the normal contribution flow for this
  repository.
- Unsolicited pull requests may be closed without detailed review, even when the
  underlying idea is valid.
- If I want code help on a specific issue, I will explicitly ask for it there.
- Opening an issue first is the right path; it lets me confirm whether the
  change fits the product direction before anyone spends time building a patch.

## How To Make An Issue Useful

- Search existing issues before opening a new one.
- Keep reproduction steps minimal and exact.
- State the Pulse version and image or package you are actually running.
- Include screenshots, logs, API output, or diagnostics when they clarify the
  problem.
- Separate bug reports from feature requests; avoid mixing both into one issue.

## Local Development Notes

This repository still contains the development tooling needed to reproduce and
debug problems locally. If you are investigating a bug before opening an issue,
these commands are the main entry points:

```bash
git clone https://github.com/rcourtman/Pulse.git
cd Pulse

brew install go node npm

cd frontend-modern
npm install
cd ..
```

Backend workflow:

- Build: `go build ./cmd/pulse`
- Tests: `go test ./...`
- Lint: `golangci-lint run ./...`
- Format: `gofmt -w ./cmd ./internal ./pkg`

Frontend workflow:

- Dev server: `cd frontend-modern && npm run dev`
- Tests: `npm run test`
- Lint: `npm run lint`
- Format: `npm run format`
- Production build: `npm run build`

Those notes are here to help with investigation and reproduction. They are not
an invitation to submit external pull requests.
