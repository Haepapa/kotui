# MVP Review & Evolution Specs (v0.1)

## 1. Executive Summary: Codebase vs. Vision
The current implementation of **Kōtui** is a high-fidelity realization of the `TECHNICAL_ARCH.md` and `DEV_PLAN.md`. The "Trust Kernel" (Phases 0–6) is exceptionally stable, with a clear separation of concerns between the Dispatcher, the MCP Tool Engine, and the Orchestrator. The project has already matured into Phase 10 (Self-Built Tools), demonstrating a high level of autonomy.

### Key Successes
*   **Resource Intelligence:** The VRAM Coordinator (`internal/ollama/vram.go`) effectively manages model swapping, making the app viable on 8GB–16GB RAM machines.
*   **Security Posture:** The `PermissionGate` and `Sandbox` implementation provide a "hard" safety boundary that is rare in multi-agent systems.
*   **Cognition Scheduling:** The `CogQueue` successfully prevents GPU contention by prioritizing Boss commands (P1) over background tasks (P3).

---

## 2. Detailed Specs for Improvements

### Spec A: Granular Tool Recovery (The "Resilience" Update)
**Goal:** Prevent task failure when a tool call returns a recoverable error.
*   **Mechanism:** Introduce `MCPError` structures that include `IsRecoverable` and `Suggestion` fields.
*   **Agent Logic:** When a tool fails, the Specialist agent receives the error and a prompt: *"The tool failed with [ERROR]. Can you attempt a fix (e.g., check file path, adjust parameters) or do you need to escalate to the Lead?"*
*   **Implementation:**
    *   Modify `internal/mcp/executor.go` to catch common shell/FS errors and wrap them in the new `MCPError` type.
    *   Update the `specialist_task_prompt` to include a "Recovery Protocol" section.

### Spec B: Proactive Curiosity (The "Insight" Engine)
**Goal:** Shift agents from "reactive tools" to "proactive teammates."
*   **Mechanism:** A low-priority (P3) background loop that triggers a `ProjectCritic` agent to scan the codebase for "smells" or inconsistencies.
*   **Trigger:** After every 5 successful tasks, or after a significant `git commit`.
*   **Output:** A `KindAgentMessage` in the Group Chat: *"@Boss, while Alfred was working on the API, I noticed the database migrations are missing a rollback script. Would you like me to draft one?"*
*   **Implementation:** 
    *   Add `CuriosityLoop` to `internal/orchestrator/orchestrator.go`.
    *   Create a new specialist role: `The Watchman` with a focus on architectural consistency.

### Spec C: Emotional Persistence (The "Soul Evolution" Hook)
**Goal:** Make agents "remember" their relationship with the Boss.
*   **Mechanism:** At the end of a DM session, the agent performs a "Self-Reflection" turn (P3).
*   **Logic:** The agent analyzes the tone of the Boss's feedback. If the Boss says "You're being too wordy," the agent calls `update_self` to modify its `soul.md` or `persona.md` to reflect "Prefers extreme brevity."
*   **Implementation:**
    *   Add a `Reflect()` method to `internal/agent/agent.go` that triggers a specific reflection prompt.
    *   Hook this into `HandleDirectMessage` completion in `orchestrator.go`.

### Spec D: The "Human" Pause (UI/UX)
**Goal:** Reduce the "black box" feeling during long LLM generations.
*   **Mechanism:** Stream "Thinking" content (if the model supports it, e.g., DeepSeek/Llama-3-Think) to the `HeartbeatBar`.
*   **Visuals:** Instead of a generic "Processing," show the actual sub-step: *"Analyzing go.mod dependencies..."* or *"Drafting unit tests for relay/slack.go..."*
*   **Implementation:**
    *   Update `StreamChunk` to include a `SubTask` field.
    *   Modify the Svelte `HeartbeatBar.svelte` to bind to this live string.

### Spec E: Social Handoffs in Group Chat
**Goal:** Enhance the "War Room" atmosphere through contextual delegation.
*   **Mechanism:** When the Lead spawns a Specialist, it shouldn't just be a system event. The Lead should post a "Hiring/Assignment" message.
*   **Example:** *"I'm assigning Alfred to the Slack Relay task. His prior experience with the Telegram gateway will ensure we keep the HMAC verification logic consistent."*
*   **Implementation:**
    *   Update `HandleBossCommand` to require the Lead to provide a "Justification" for every task assignment in the generated plan.

---

## 3. Missing Critical Features (Roadmap)
1.  **Project-Wide RAG:** Currently, memory is limited to agent journals. We need a `KnowledgeBase` tool that indexes the entire project folder into the vector store.
2.  **Sudo-Approval Workflow:** For commands that require `sudo` or touch sensitive files (outside the sandbox), the app should pause and show an "Authorize" button in the UI, rather than just failing.
3.  **Visual Artifacts:** Add support for rendering Mermaid diagrams or charts in the Group Chat for architectural proposals.
