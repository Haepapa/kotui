# Development Plan — AgentFlow Orchestrator MVP

---

## Guiding Principles

This plan splits the MVP into **three tiers**:

| Tier | Description | Who Builds It |
|------|-------------|---------------|
| 🔒 **Trust Kernel** | Infrastructure the app depends on to reason, execute, and stay safe. Errors here can't be self-corrected. | Human developer, strict guidance |
| 🧪 **Verified Core** | Features built on the Trust Kernel that require human QA but follow established patterns. | Human developer, normal guidance |
| 🤖 **Self-Buildable** | Leaf features that use the working MCP framework. The app can build, test, and iterate on these through the normal Boss → Lead → Worker loop. | The app itself (with Boss review) |

Each phase ends with a **Test Gate** — a point where development pauses for user testing and feedback before proceeding.

---

## Phase 0 — Project Scaffolding 🔒

Establish the skeleton that every subsequent phase depends on.

| Task | Detail |
|------|--------|
| Wails v3 init | `wails3 init` with Svelte 5 frontend template |
| Go module structure | `cmd/`, `internal/dispatcher/`, `internal/ollama/`, `internal/mcp/`, `internal/agent/`, `internal/store/`, `pkg/models/` |
| SQLite bootstrap | Embedded DB via `modernc.org/sqlite` (pure Go). Schema migration system using versioned `.sql` files |
| Config system | TOML config file in `/data/config.toml` covering Ollama endpoint, active project, timezone, model assignments |
| Logging | Structured logging (`slog`) with two tiers: Summary (Group Chat) and Raw (Engine Room) |
| Test harness | Set up Go test infrastructure (`go test ./...`). **All Trust Kernel phases (0–6) require automated unit tests.** The Dispatcher, MCP Permission Gate, and Sandbox boundary are the "Immutable Laws" — if a future self-built update breaks permission logic, only hard-coded tests will catch it. Minimum coverage targets: Dispatcher message routing, MCP permission gate (all 3 clearance tiers), sandbox escape prevention, VRAM budget/swap logic. |

**No Test Gate** — this is scaffolding only.

---

## Phase 1 — Ollama Integration & Single-Agent Loop 🔒

The most critical subsystem. If inference doesn't work, nothing works.

| Task | Detail |
|------|--------|
| Ollama HTTP client | `/api/chat` (streaming), `/api/tags`, `/api/pull`, `/api/embeddings` |
| VRAM budget check | On startup, query available VRAM via Ollama `/api/tags` + system inspection. Calculate whether Lead (30B-Q4) + Worker (8B-Q4) fit simultaneously. Store result as the system's **VRAM Profile** (`dual` or `swap`). |
| VRAM manager | Load/unload via `keep_alive` parameter. **Dual mode:** Lead = `-1` (persistent), Workers = `0` (release after use). **Swap mode:** Before loading a Worker, "Park" the Lead (set `keep_alive: 0`, wait for unload confirmation), load Worker, execute, unload Worker, reload Lead. The Boss should never notice — the swap is transparent. |
| Heartbeat monitor | Periodic `/api/tags` poll. Detect OOM / hang → emit SystemEmergency event |
| Single conversation loop | Send user message → stream tokens → accumulate response → return |
| Error handling | 60s timeout per turn, 3 retries on transient failure, escalation on 3rd failure |

### 🧪 Test Gate 1 — "Can You Hear Me?"
> Run in terminal. Chat with a single agent via stdin/stdout. Verify:
> - Streaming works smoothly
> - Model loads/unloads correctly
> - **VRAM Profile detection is accurate** (test on actual hardware)
> - **Swap mode works on constrained GPU** (Park Lead → load Worker → unload Worker → reload Lead, no OOM)
> - Heartbeat detects a manually killed Ollama process
> - Timeout triggers after 60s of silence
>
> **Feedback needed:** Response quality, latency feel, error messages clarity.

---

## Phase 2 — Dispatcher & Message Bus 🔒

The central nervous system that routes all communication.

| Task | Detail |
|------|--------|
| Message types | Define `AgentMessage`, `BossCommand`, `SystemEvent`, `ToolCall`, `ToolResult`, `Milestone` |
| Go channel bus | Fan-out pub/sub: Dispatcher receives all messages, routes to subscribers (UI, relays, store) |
| SQLite persistence | Tables: `conversations`, `messages`, `tasks`, `agents`, `approvals`, `events` |
| Event classification | Tag each message as `summary` or `raw` for tiered streaming |
| Project scoping | All queries scoped by `project_id`. Workspace switch = reload context |

**No Test Gate** — tested implicitly through Phase 3.

---

## Phase 3 — MCP Protocol Framework 🔒

The tool-calling system that gives agents hands.

| Task | Detail |
|------|--------|
| Tool registry | Register tools with name, JSON schema, description, clearance level (Lead/Specialist/**Trial**) |
| Execution engine | Validate input against schema → execute handler → capture stdout/stderr/result → return structured response |
| Permission gate | Before execution, verify the calling agent's clearance matches the tool's required level. **Three clearance tiers:** `Lead` (full planning tools), `Specialist` (full execution tools), `Trial` (read-only subset — can inspect files and discuss code, but **blocked** from `shell_executor`, `write_file`, and `delete_file`). Trial is used exclusively during the Hiring interview phase. |
| Sandboxing | All file operations scoped to `/data/projects/{project_name}/`. No `sudo`. Backup before delete. |
| Retry logic | 3 attempts per tool call. On 3rd failure: pause task, emit escalation event |

### 🧪 Test Gate 2 — "Hands On"
> Register a mock tool. Have a single agent call it via structured JSON output. Verify:
> - Tool schema is correctly injected into the system prompt
> - Agent produces valid tool-call JSON
> - Execution runs sandboxed
> - Retry and escalation work
> - **Trial clearance blocks write/execute tools** (attempt a `shell_executor` call from a Trial agent — must be denied)
> - **Automated tests pass** for permission gate (all 3 tiers), sandbox boundary, and retry logic
>
> **Feedback needed:** Is the tool-call format reliable with the target models? Do qwen2.5-coder:32b and llama3.1:8b produce valid MCP JSON consistently?

---

## Phase 4 — Core MCP Tools 🔒

The minimum toolset for the system to do real work. These **must** be hand-built because the app needs them to build anything else.

| Tool | Scope | Detail |
|------|-------|--------|
| `filesystem` | Specialist | Read, write, delete, list files. Scoped to project workspace. Backup-before-delete enforced. |
| `shell_executor` | Specialist | Run shell commands (`go build`, `npm test`, etc.). Capped execution time. No `sudo`. Stdout/stderr captured. |
| `file_manager` | Lead | Directory tree mapping, project structure overview, file metadata. Read-only. |
| `iot_gateway` (read-only) | Specialist | **Human-verified** basic handshake with remote hardware nodes (Raspberry Pi Pico, LoRa gateways) via serial or SSH. Read-only in Phase 4: device discovery, connection test, status polling, sensor data retrieval. Write/command capabilities deferred to Phase 10 as a self-built upgrade. Rationale: hardware communication protocols are safety-critical and must be verified by a human developer before agents can send commands to physical devices. |

### 🧪 Test Gate 3 — "First Real Task"
> In terminal mode, ask a single agent to: "Create a Go hello-world program in the project workspace and run it."
> Verify:
> - Files are created in the correct location
> - Shell command executes and output is captured
> - Agent interprets tool results correctly
> - Sandbox prevents escape (try to read `/etc/passwd`)
>
> **Feedback needed:** Are the tool boundaries right? Too restrictive? Too loose?

---

## Phase 5 — Agent Identity & Lifecycle 🔒

Give agents memory, personality, and growth.

| Task | Detail |
|------|--------|
| Identity filesystem | `/data/agents/{agent_id}/identity/` containing `soul.md`, `persona.md`, `skills.md`, `instruction.md` |
| System prompt composer | Reads identity files + `handbook.md` + `COMPANY_IDENTITY.md` → assembles `instruction.md` |
| Company Identity loader | Parse and inject Vision, Purpose, Values into every agent's context |
| `handbook.md` | Write the initial SOP document: journal format, etiquette rules, hard-fail constraints |
| Agent spawn/teardown | Create agent directory → compose prompt → load model → ready. On teardown: unload model → write journal. |
| Journaling | On task completion: write `journal/YYYY-MM-DD-HHMM.md` with task summary, outcome, and lessons |
| Skill proposals | Agent can append to a `proposed_skills.md`. Requires Boss approval to merge into `skills.md`. |

**No Test Gate** — rolls into Phase 6.

---

## Phase 6 — Multi-Agent Orchestration 🔒

The hierarchy comes alive.

| Task | Detail |
|------|--------|
| Lead Agent init | Spawn with 30B model, `keep_alive: -1`, planning & verification tools |
| Worker spawning | Lead requests a Specialist → Dispatcher spawns with 8B model → grants scoped tools |
| Verify-then-Proceed | Lead assigns sub-task → Worker executes → posts Draft (hidden from Boss) → Lead reviews → Pass/Fail |
| Task Tree | SQLite `tasks` table with parent-child relationships. Lead decomposes Boss request into sub-tasks. |
| Hiring workflow | Lead posts Hiring Proposal → Sandbox spawn with **Trial clearance** (read-only tools) → Boss interview in private chat → Approve/Reject → On approval, promote to Specialist clearance and onboard to Group Chat |
| VRAM coordination | Respects the **VRAM Profile** from Phase 1. In `dual` mode: Lead + 1 Worker simultaneously. In `swap` mode: Park Lead before each Worker execution, reload after. Queue additional workers either way. |

### 🧪 Test Gate 4 — "The War Room Works"
> In terminal mode, give the Lead a multi-step task: "Set up a new Go project with a REST API that has a health endpoint, and write tests for it."
> Verify:
> - Lead decomposes into sub-tasks
> - Workers are spawned and receive correct tools
> - Verify-then-Proceed loop functions (observe a correction cycle if possible)
> - VRAM stays within bounds (only 1 worker loaded at a time)
> - Journals are written on completion
>
> **Feedback needed:** Quality of task decomposition. Does the Lead → Worker handoff feel right? Are correction cycles productive or loops?

---

## Phase 7 — War Room UI Shell 🧪

The desktop experience. Built on top of the now-stable backend.

| Task | Detail |
|------|--------|
| Svelte 5 layout | App shell: sidebar (agent list), main area (Group Chat), bottom bar (Heartbeat) |
| Group Chat | Threaded message view. Messages tagged by agent with avatar. Milestone highlights. |
| Boss Mode / Dev Mode | Global toggle. Boss = clean summaries. Dev = Engine Room console expands with raw logs, tool calls, reasoning |
| Heartbeat bar | Pulse animation + breadcrumbs (`Planning > [Coding] > Testing`) |
| Project selector | Dropdown/sidebar to switch active project. Triggers context reload. |
| Wails event binding | Subscribe to Dispatcher events. Summary → Group Chat. Raw → Engine Room (only when visible). |

### 🧪 Test Gate 5 — "The Feel Test"
> Launch the desktop app. Repeat the Phase 6 task through the UI.
> Verify:
> - Messages render correctly in Group Chat
> - Mode toggle hides/reveals the right information
> - Heartbeat reflects actual system state
> - Project switching works cleanly
>
> **Feedback needed:** UX feel. Information density in each mode. Visual clarity. Performance under streaming.

---

## Phase 8 — War Room UI Interaction Layers 🧪

| Task | Detail |
|------|--------|
| One-on-One Sidebar | Click agent avatar → private channel. Boss feedback is logged as "Boss Feedback" journal entry. |
| Candidate Trial Window | Temporary chat during Hiring. Restricted scope. Accept/Reject buttons. |
| Artifact rendering | File links in chat as clickable pills. Code preview for source files. |
| Boss approval UI | Notification badge for pending approvals (skill promotions, hiring). Approve/Reject inline. |
| Settings view | "Infrastructure Office" — Ollama endpoint, model assignments, timezone, remote messaging tokens |
| Company Identity editor | In-app markdown editor. Save triggers "Culture Update" broadcast to all active agents. **This must force a full Context Reset** — not just a notification. LLMs are "stubborn" and will continue following their previous system prompt unless it is fully re-injected. On Culture Update: (1) recompose `instruction.md` for every active agent incorporating the new values, (2) terminate the current conversation context for each agent, (3) re-initialize with the updated system prompt and a brief "Culture Update: the following values have changed..." preamble so the agent understands why its context was reset. |

### 🧪 Test Gate 6 — "Full Loop"
> End-to-end session:
> 1. Start a project
> 2. Give the Lead a task
> 3. Watch delegation and execution in the UI
> 4. Lead proposes hiring a new Specialist → interview in Trial Window → approve
> 5. Give private feedback to a Worker via One-on-One
> 6. Worker proposes a skill update → approve it
> 7. Edit Company Identity → **verify agents perform a full context reset** (not just acknowledgement — their subsequent responses should reflect the new values, not the old ones)
>
> **Feedback needed:** Does every interaction feel intentional? Are approval flows clear? Any dead ends?

---

## Phase 9 — Agent Memory & Vector Search 🧪

| Task | Detail |
|------|--------|
| Embedding integration | Call Ollama `/api/embeddings` with `nomic-embed-text` model |
| SQLite-VSS setup | Add vector columns to journal entries table. Index on write. |
| Embed-on-write pipeline | After journaling, embed the entry and store the vector |
| Orientation Recall | On agent init / new task: query top-k similar journal entries. Inject as "Past Experience" context. |
| Feedback recall | Boss feedback entries weighted higher in recall ranking |

### 🧪 Test Gate 7 — "Does It Remember?"
> 1. Complete a task. Agent journals the result.
> 2. Give specific private feedback ("Don't use global variables").
> 3. Start a new, similar task.
> 4. Verify the agent's behaviour reflects past feedback.
>
> **Feedback needed:** Is recall relevant or noisy? Does past feedback actually influence behaviour?

---

## 🤖 SELF-BUILD BOUNDARY

**After Phase 9, the system is a functioning multi-agent orchestrator with tools, UI, and memory.** The following phases can be built *by the app itself* — the Boss assigns the task, the Lead delegates, Workers write the code, and the Boss reviews the output through the normal War Room workflow.

---

## Phase 10 — Self-Built MCP Tools 🤖

These tools extend agent capabilities. Each follows the existing MCP tool pattern (JSON schema, handler function, test).

| Tool | Type | Detail |
|------|------|--------|
| `project_critic` | Lead | Static analysis, code review, logical verification. Wraps linters and produces structured feedback. |
| `web_search` | Specialist | HTTP-based documentation retrieval. Sanitised output returned as context. |
| `iot_gateway` (write/command) | Specialist | Extends the read-only Phase 4 `iot_gateway` with command capabilities: firmware upload, configuration writes, actuator control. Boss approval required per-command until the tool is promoted to trusted. |

> **Boss review required** for each tool before it enters the production registry.

---

## Phase 11 — Docker & Headless Mode 🤖 (with human review)

| Task | Detail |
|------|--------|
| `--headless` flag | Suppress Wails window. Backend runs in pure Go mode. |
| Relay Gateway | Intercepts internal Dispatcher events → translates to external API calls |
| Dockerfile | Multi-stage: build Go binary → copy into slim runtime image with MCP tools |
| Docker Compose | Services: `orchestrator`, `ollama`. Volumes: `/data` |
| Filesystem isolation | Verify agents can only access `/data` mount |

> **Human review required** for Dockerfile and security boundaries. The app can draft these, but a developer must verify the isolation model.

### 🧪 Test Gate 8 — "Headless HQ"
> Build and run in Docker. Verify:
> - Container starts cleanly
> - State persists across restart
> - Logs are accessible
> - Ollama sidecar connects correctly

---

## Phase 12 — Remote Messaging Relays 🤖 (with human review)

| Task | Detail |
|------|--------|
| Telegram bot | Bi-directional relay. Summary-only outbound (Noise Control). |
| Slack bot | Same pattern, Slack API. |
| WhatsApp | Same pattern, WhatsApp Business API. |
| Remote commands | `/status`, `/approve [ID]`, `/summary` |
| HMAC verification | Validate incoming webhook signatures |
| Settings UI | Token input fields in Infrastructure Office |

> **Human review required** for authentication flows and webhook security.

### 🧪 Test Gate 9 — "Pocket Boss"
> Run headlessly in Docker. Interact exclusively via Telegram.
> Verify:
> - Messages arrive promptly
> - `/status` and `/summary` return useful info
> - `/approve` works for pending approvals
> - No raw/noisy data leaks to mobile

---

## Summary: Build Order & Ownership

```
STRICT DEV GUIDANCE (Trust Kernel)
├── Phase 0: Scaffolding
├── Phase 1: Ollama Integration          ← Test Gate 1
├── Phase 2: Dispatcher & Message Bus
├── Phase 3: MCP Framework               ← Test Gate 2
├── Phase 4: Core MCP Tools              ← Test Gate 3
├── Phase 5: Agent Identity & Lifecycle
└── Phase 6: Multi-Agent Orchestration   ← Test Gate 4

HUMAN DEV, NORMAL GUIDANCE (Verified Core)
├── Phase 7: War Room UI Shell           ← Test Gate 5
├── Phase 8: UI Interaction Layers       ← Test Gate 6
└── Phase 9: Agent Memory & Vector Search← Test Gate 7

═══════════════════════════════════════════
  SELF-BUILD BOUNDARY — App builds itself
═══════════════════════════════════════════

APP-BUILT (with Boss/Human review)
├── Phase 10: Additional MCP Tools
├── Phase 11: Docker & Headless          ← Test Gate 8
└── Phase 12: Remote Messaging           ← Test Gate 9
```

---

## What "Self-Build" Means in Practice

Once Phase 9 is complete, the Boss can open the War Room and say:

> "Build a new MCP tool called `web_search` that fetches documentation from URLs and returns sanitised markdown."

The Lead Agent will:
1. Decompose the task (design schema, implement handler, write tests)
2. Delegate to a Worker with `filesystem` + `shell_executor` tools
3. Verify the output
4. Present it for Boss approval

The tool then enters the registry and becomes available to all agents. **This is the app building itself.**

The same pattern applies to:
- Writing new agent personas (`soul.md` / `persona.md`)
- Refining `handbook.md` content
- Creating UI components (Svelte files in the frontend)
- Drafting Dockerfiles and compose configurations
- Writing integration tests
- Generating documentation
