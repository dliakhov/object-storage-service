# AI Assistance Disclosure

For this task I used **Claude (Anthropic)** via the Claude Code CLI as a development assistant.

## How I approached the task

My first step before writing any feature code was setting up a solid foundation: initializing the project with the latest Go version and configuring `golangci-lint` with a strict ruleset. Then I configured GitHub Actions to validate every pull request — running tests with the race detector and the full linter suite before any merge.

Once that was in place, I brought in Claude to help implement the first in-memory storage and in the next iteration it was implemented the file-based storage backend.

## What I used it for

I started by describing what I needed: in-memory and file-backed storage with content deduplication, and I specifically asked it to think through the security implications (what is related to the file-system).

In each iteration I ran a `/code-review` command, which internally spawned multiple AI agents in parallel — each one analysing the diff from a different angle: line-by-line correctness, removed-behaviour auditing (what invariants did deleted lines enforce?), cross-file call-site tracing, reuse opportunities, simplification, efficiency, and whether a fix was at the right abstraction depth.

Nothing got merged automatically.

## What I actually did myself

Setting up the project structure, Go version, and lint config. Defining the requirements and the security constraints I wanted enforced. Reviewing the design before implementation.

A significant portion of the code was written by Claude. But the shape of the project — the quality bar, the security requirements, the configuration model — came from decisions I made and directed.
