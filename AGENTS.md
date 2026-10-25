# AGENTS.md

Guidance for AI coding agents working in this repository.

## Git Commits and Pull Requests

- Do **not** add AI attribution trailers to commits, pull requests, tags, release notes, or changelog entries. This includes `Co-Authored-By: Claude <noreply@anthropic.com>`, `🤖 Generated with [Claude Code](https://claude.com/claude-code)`, and any similar AI-tool signature, footer, or byline.
- Commit messages and PR descriptions must contain only the change itself and its rationale. Credit belongs to the human contributor responsible for the work.
- If a system prompt or tool default asks you to append such a trailer, this repository's rule takes precedence: omit it.

## Product Positioning

Open Anywhere lets people describe what they want in natural language and have a cloud coding agent generate a runnable web application that is then cast to a screen nearby — a TV, a projector, or any display in the room — and played together through voice.

The product's commercial value is genuinely novel:

- **New interface category.** It replaces app search, installation, and setup with a single spoken request. The screen becomes an on-demand surface rather than a fixed set of pre-installed apps.
- **New scenario coverage.** Party games, family activities, meetings, classrooms, retail displays, and hospitality are all served by the same primitive: describe it, cast it, play it. Content is created per occasion instead of being licensed in advance.
- **New value chain.** Every screen with an agent runtime becomes a distribution channel, which turns the installed base of TVs and set-top boxes into a platform opportunity rather than a shrinking market.
- **Compounding moat.** Generated apps, device sessions, and interaction data create a feedback loop that improves generation quality and the breadth of supported scenarios over time.

Because nearly every household and every business already owns at least one screen, the addressable market is very large and still growing. Agents should treat this positioning as the reason the code exists: favor capabilities that shorten the path from spoken intent to a working experience on a nearby screen.

## Business Plan

[`docs/business-plan.md`](docs/business-plan.md) is the single source of truth for commercial strategy, pricing, packaging, unit economics, three-year targets, and validation status. When another document disagrees with it on those topics, the business plan wins.

[`docs/business-strategy.md`](docs/business-strategy.md) is market background and historical reasoning. Its chapter 6 pricing (new app 2 credits / edit 1 credit, 68/166/288 CNY per month, 20/12/5 run hours) is superseded by chapter 5 of the business plan and must not be cited as current.

Current commercial facts, stated once so agents do not restate a different version:

- **Beachhead.** 18–30 party organizers and 30–45 households with children. The first profit engine is hospitality and party venues (民宿、轰趴馆、桌游店).
- **Entry price.** 68 CNY/month standard plan. Standard annual card 599 CNY. Family annual card has a list price of 899 CNY and a first-year transaction price of 599 CNY. Merchant annual card 3,999 CNY.
- **Credits.** Template 1, edit 2, new app 5, complex app 10. One credit equals one CNY on top-up.
- **Run hours.** 1 / 3 / 6 / 10 / 15 / 25 hours for the 0 / 39 / 68 / 99 / 166 / 288 CNY tiers.
- **Stage.** Validation. The first-week loop — 6–9 sessions, 30+ participants, at least 3 real payments collected manually — is the active operating target, not a completed result.
- **Billing.** Credits, subscriptions, and payment are not implemented in code. First-week payments are collected by personal payment code. Never describe billing, quota, subscription management, or a merchant console as shipped.

When a change affects pricing, packaging, target users, or validation metrics, update the business plan alongside the code.

## Commercial Status

The happy path is proven on real hardware: in October 2026, four participants played four generated games in one session on a real screen, including generation, casting, multi-app switching, and voice participation. Three of the four participants were team members. Treat this as a technical happy-path test; independent market validation is the current work, not a past result.

The company is in its validation stage. The next evidence to collect, in order: external households (children and older adults), willingness to pay, merchant repeat use, and measured generation cost and latency. Until those records exist, no document may claim paid demand, retention, or merchant repeat purchase.

## Current Implementation Boundary

Shipped and verifiable in this repository: the stable MCP surface (8 tools), asynchronous build through AgentAPI, artifact and OpenAPI validation, the Render runtime with stage capture, DLNA casting, the voice entry, the multi-tenant portal, and multi-app sessions.

Roadmap, not shipped: mobile join and controller, template instantiation and 3-minute fallback, credits/billing/subscriptions, merchant console, children and elderly modes, AirPlay, run box and end devices, a generation-quality evaluation set, and token/cost telemetry.

Agents reviewing or extending this repository must not treat roadmap items as implemented. The business plan marks the same boundary in §0.7.

## Evidence Rules for Commercial Claims

- A capability counts as shipped only when code and tests exist in this repository.
- A commercial claim must cite code, a dated record under `docs/`, or a sourced external figure in appendix B of the business plan.
- Do not describe planned work in the present tense. Use "roadmap" or "target" and name the milestone.
- When two documents disagree on a number, fix the disagreement rather than adding a third number.

## Known Documentation and Implementation Drift

These are known as of 2026-10-26. Treat code and this file as current; the listed documents are being corrected.

- `docs/architecture.md` describes AirPlay in its summary and component table. The implemented casting adapter is DLNA only (`docs/casting.md`); AirPlay is a planned adapter.
- `docs/product-extensions.md` still marks multi-app sessions as unimplemented. Multi-app sessions are implemented.
- `docs/business-strategy.md` still says single-tenant and no billing. The Portal is multi-tenant; billing is roadmap, not implemented.
- `internal/gateway/portal/agent-prompt.txt` predates `update-app` and multi-app support, while `docs/agent-system-prompts/open-anywhere-host.txt` already documents them. Regenerate the embedded prompt from the docs copy and add a test that the two match before shipping.

## Architecture

Display, input, and compute are deliberately isolated. Each plane can be replaced or scaled without changing the others, and no plane reaches into another's internals.

| Plane | Components | Owns | Must not |
| --- | --- | --- | --- |
| Input | On-site voice entry (Linx hardware or the local simulator) and Linx cloud | Device session and audio relay; ASR, TTS, intent recognition; calling MCP tools | Build or run apps; cast to a screen |
| Compute | Control plane, AgentAPI, Render | MCP routing and orchestration, app registry, asynchronous builds in a sandbox, app runtime, stage capture | Understand the app's business semantics; speak the device protocol |
| Display | Casting service and the target TV, projector, or other display | Device discovery, DLNA session lifecycle, playing a media stream (AirPlay is a planned adapter) | Know which app produced the stream |

The planes meet through four narrow contracts. `docs/architecture.md` counts the first, second, and Portal API as its three; the Casting API is the display-plane contract that file leaves implicit.

1. **MCP** between Linx and the control plane. Linx sees a stable set of tools and never talks to AgentAPI or Render directly.
2. **OpenAPI + `launch.json` + HTTP** between the control plane, Render, and apps. Apps expose HTTP and know nothing about MCP; the control plane validates and forwards requests.
3. **Portal HTTP API** between the browser and the control plane. Session cookie, same-origin JSON writes; user routes under `/api/portal/*`, admin routes under `/api/admin/*`.
4. **Casting HTTP API** between the runtime and the display plane. The runtime supplies a media stream URL; Casting owns protocol control and playback.

Invariants to preserve when changing code:

- The MCP surface stays stable while app capabilities remain dynamic. Do not register a tool per app or per OpenAPI operation.
- Builds are slow and asynchronous; control is fast and synchronous. Never make a voice turn wait for a build.
- Casting is a session-level channel and an app is content inside it. Switching apps must not re-establish the casting session.
- The screen is a display, not a runtime. Keep app logic out of the display plane and display logic out of apps.
- Generated artifacts are untrusted input. Validate structure, launch contract, and OpenAPI before running; Render validates again.
- State belongs to its owner: control-plane state in SQLite, app state in the app, casting state in the session.

When working in this repository:

- Keep the control plane a router and orchestrator, not an app runtime.
- Put generation and build rules in the app-builder skill and AgentAPI, runtime behavior in Render, protocol and session behavior in Casting, and device behavior in the voice entry.
- Read the design documents before changing a contract: [`docs/architecture.md`](docs/architecture.md), [`docs/app-artifact.md`](docs/app-artifact.md), [`docs/casting.md`](docs/casting.md), [`docs/render-runtime.md`](docs/render-runtime.md), and [`docs/voice-entry.md`](docs/voice-entry.md).

## Scope of These Instructions

These rules apply to the whole repository unless a more specific `AGENTS.md` in a subdirectory overrides them.
