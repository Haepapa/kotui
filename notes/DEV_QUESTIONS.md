# Development Questions — Specification Gaps

These questions identify missing or ambiguous information in the current specs that need answers before MVP development can begin.

---

## 1. Frontend Framework (Wails v3)

Wails v3 supports multiple frontend frameworks (React, Svelte, Vue, vanilla JS/TS). Which frontend framework should be used for the War Room UI?

## 2. Target Ollama Models

The specs reference "8B" workers and a "30B" Lead Agent. Which specific Ollama model families/names are targeted? (e.g., `llama3`, `qwen2.5`, `mistral`, `command-r`) — this affects prompt formatting and capability assumptions.

## 3. Data Persistence / State Store

The Dispatcher manages a "central State Store" but no storage mechanism is defined. Is this:
- File-based (Markdown/JSON on disk)?
- An embedded database (SQLite, bbolt)?
- In-memory only (rebuilt on restart)?

This is foundational — it determines how conversations, agent state, tasks, and approvals are stored and queried.

## 4. MCP Tools Inventory

Agents are granted "MCP credentials" and use MCP for tool-calling, but the actual tools are never enumerated. What capabilities should agents have? For example:
- File read/write/delete
- Shell command execution
- Web/HTTP requests
- Code analysis / linting
- Git operations
- IoT/LoRa device communication

Which tools are available to the Lead vs. Workers?

## 5. Vector Search for Journal Recall

`AGENT_EVOLUTION.md` states agents search journals using "local vector search." What embedding model and vector storage should be used? (e.g., Ollama embeddings + a Go vector lib, or an external tool like ChromaDB?)

## 6. `handbook.md` — Undefined Reference

`AGENT_EVOLUTION.md` references a `handbook.md` that agents read during orientation, but this file is never defined in any spec. What should it contain? Is it distinct from `COMPANY_IDENTITY.md` and `GOVERNANCE.md`, or is it a composite of them?

## 7. Inter-Agent Communication Mechanism

How do agents exchange messages internally? Options include:
- Go channels / in-process message bus
- A formal event bus / pub-sub system
- Direct function calls within the Dispatcher

The specs describe the *UI* of the Group Chat but not the underlying communication plumbing between agents.

## 8. Frontend ↔ Backend Event Protocol

The specs mention "Summary" and "Raw" event streams. How are these delivered to the Wails frontend?
- Wails v3 native event bindings?
- WebSocket from a local HTTP server?
- Server-Sent Events?

This also affects headless mode where the same events route to Slack/Telegram.

## 9. Error Handling & Resilience

No spec covers failure scenarios:
- What happens when a model OOMs or Ollama becomes unresponsive?
- How are infinite loops or stuck agents detected and handled?
- Is there a retry policy for failed tool calls?
- Does the Boss get notified of system-level errors, and how?

## 10. Agent-Produced Artifacts

Agents generate "assets" linked in the Group Chat. What types of artifacts are expected? (code files, documents, images, data exports?) Where are they stored — in the `/data` volume, a project working directory, or elsewhere?

## 11. Cloud Provider Integration Scope

`INFRASTRUCTURE_SETTINGS.md` mentions OpenAI/Anthropic as "Senior Consultants." Is this MVP-required, or can the MVP be local-only (Ollama)? If included, how is API key management handled?

## 12. Remote Messaging Authentication

The remote relay supports Slack, Telegram, and WhatsApp. Are bot tokens / API keys configured via the Settings UI, environment variables, or config files? Is there an OAuth flow, or just static tokens?

## 13. Project / Workspace Concept

The specs describe a "Company" operating on tasks, but there's no concept of a **project** or **workspace**. Can the Boss run multiple independent projects? Is the War Room scoped to one project at a time, or is it multi-project?
