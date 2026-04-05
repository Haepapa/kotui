🧠 Spec Doc: Advanced Cognition & Resource Control
1. Social Intelligence & "Human" Cognition

To ensure agents feel collaborative rather than purely transactional, the system implements a Confidence-Based Consultation model.
A. The Curiosity Directive (Confidence Scores)

Agents do not blindly execute tasks. Every prompt is governed by an internal Confidence Score (CS):

    Logic: Before a tool call, the agent assesses its certainty on a scale of 0.0 to 1.0.

    Threshold: If CS<0.7, the agent must pause and trigger a type: consultation message to the Lead or Boss instead of executing.

    Transparency: For CS≥0.7, the agent proceeds but must log its reasoning chain in the Engine Room (Raw Logs).

B. The Collaborative Loop

    Inter-Agent Tagging: Specialists are empowered to "tag" teammates or the Lead in the Group Chat for advice (e.g., "@Lead, seeking clarification on LoRa signal baseline").

    Social Personality: Agents utilize their soul.md and persona.md to maintain distinct human-like temperaments (e.g., "Skeptical Engineer" or "Optimistic Architect") during these interactions.

C. The Lead Optimizer (Workflow Evolution)

The Lead Agent acts as the "Performance Director," observing team efficiency:

    Review Cycle: Periodically, the Lead analyzes the last 10 Specialist journals to identify friction points.

    Self-Modification: The Lead proposes updates to the handbook.md (Standard Operating Procedures) to correct recurring failures.

    Approval: These updates require Boss verification in Dev Mode before being committed to the Immutable Core.

2. Backend Call & Resource Management

The Resource Controller is a Go-based service layer that manages all Ollama interactions to prevent system-wide instability.
A. The Cognition Request Queue

All LLM requests are routed through a central CognitionRequest queue with strict priority levels:

    P0 (Emergency): OOM recovery, Heartbeat checks, and critical UI feedback.

    P1 (Lead): Direct Boss interaction and high-level strategy/planning.

    P2 (Interactive): Live "Candidate Trials" and interviews.

    P3 (Task): Background execution by Specialist agents.

B. VRAM Guardrails

To protect local hardware performance, the manager enforces these limits:

    Concurrency: Maximum of 1 Specialist active in VRAM alongside the persistent Lead Agent.

    Cooling Period: A mandatory 500ms delay between model swaps to ensure clean GPU memory clearing.

    System Throttle: If CPU/RAM utilization exceeds 90%, the P3 queue is paused until hardware pressure drops.

3. Efficient Identity & Context Loading

The system avoids constant disk I/O by utilizing an In-Memory Identity Registry and tiered swapping logic.
A. The Identity Registry

    In-Memory Caching: On startup, the Go backend parses soul.md, persona.md, and skills.md into structured objects in RAM.

    Hot-Reloading: The backend only re-reads disk files if a manual save is detected or the agent triggers a "Self-Evolution" event.

B. Physical vs. Logical Swapping

The backend differentiates between two types of "loads" to maximize speed:

    Physical Swap (Heavy): Moving from a 30B Lead to an 8B Specialist. This involves clearing and loading new model weights in VRAM.

    Logical Swap (Light): Switching between two Specialists using the same 8B model. The system keeps the weights in VRAM and only swaps the System Prompt and Context.

C. Speculative Pre-loading

To eliminate perceived latency, the Resource Controller implements speculative loading:

    Prediction: The backend monitors the Lead Agent’s planning stream.

    Action: If the Lead plans to hire or assign a task to "Specialist Beta," the backend begins pre-loading the 8B model weights before the Boss even clicks "Approve".

4. Implementation Roadmap for Developers
Phase	Component	Key Task
Phase 1	Request Queue	Implement the internal/llm/manager.go queue with P0-P3 priority.
Phase 2	Identity Cache	Build the internal/agent/registry.go to store parsed Markdown souls in RAM.
Phase 3	Confidence Logic	Update the handbook.md and agent prompts to include Confidence Score (CS) self-assessment.
Phase 4	VRAM Guard	Implement hardware monitoring to pause queues during high system pressure.
Phase 5	UI Feedback	Update Svelte 5 Heartbeat bar to display "Queued" vs "Active" cognition states.