# Making Kōtui Feel More Human

## The Problem

When a user sends a message that is **contextual or introductory** — setting the scene for work that is about to begin — the agent jumps straight into task execution instead of responding like a team member would.

**Example:**
> "Hi, you will help me build a suite of python scripts which will call the ollama service running locally."

**What happened:** The Lead immediately spawned tasks and assigned them to a specialist worker.  
**What a human would do:** "Sounds like an interesting project — let me know when you're ready to start and I can help plan it out."

This document analyses the root cause, identifies all the places in the pipeline that contribute, and proposes options for improvement.

---

## Root Cause Analysis

### Where the problem lives

The Lead agent's behaviour is shaped by several layers stacked on top of each other:

```
[System Prompt]
  ├── Company Identity (values, vision)
  ├── Handbook (hard rules, confidence protocol)
  ├── Soul.md (core values — sparse by default)
  ├── Persona.md (communication style — sparse by default)
  ├── Skills.md (capabilities)
  └── MCP tools fragment

[Per-message augmentation: decomposePrompt()]
  ├── "If this is a TASK → output JSON task list FIRST, then explain"
  └── "If this is CONVERSATIONAL → respond naturally"
```

**The `decomposePrompt()` function** is binary: task or conversational. When the user sends `"Hi, you will help me build python scripts..."`, the model sees "Hi" (conversational) but also "build" and "scripts" (strong task signals). It resolves the ambiguity by choosing the task path because the prompt explicitly says task decomposition is **mandatory** when a task is present.

There is **no middle ground** for:
- Introductory project briefs
- Context-setting messages
- Exploratory conversations about what to build

### Why DMs feel better

The DM pipeline uses `dmTurnPrompt()`, which has a **5-step reasoning chain**:
1. Understand (introduction / question / instruction / task)
2. Identity check
3. Tool call check
4. Ambiguity check
5. **Tone check**

Channel messages use `decomposePrompt()` which only has a binary decision: task or conversational. No "Understand" step, no explicit "Tone" step.

### Contributing factors

| Layer | Issue |
|---|---|
| `decomposePrompt()` | Binary: task vs conversational — no third path for "project brief / context-setting" |
| Default `persona.md` | Sparse: "Analytical, structured, and decisive. Communicates plans clearly." — no warmth instructions |
| Default `soul.md` | Empty core values section — not populated until a Culture Update runs |
| Handbook | Focused entirely on execution protocol — no social behaviour guidelines |
| Orchestrator | On receiving a task list, immediately begins spawning workers — no "social preamble before execution" |

---

## Options

---

### Option 1 — Three-way Classification in `decomposePrompt()`
**Complexity: Low | Impact: High**

Replace the current binary (task / conversational) with an explicit third path: **project introduction / context-setting**.

The current prompt says:
> "If this is a task → JSON first. If this is conversational → respond naturally."

Add:
> "If this is a **project introduction, context brief, or 'here's what we're going to do'** → acknowledge warmly, express genuine interest, and offer to plan it out. Do NOT immediately decompose tasks — wait for the Boss to signal they're ready to begin."

**Trigger signals for the new path:**
- Contains "you will help me", "we will be building", "I want to", "I'm going to" (future intent)
- Contains a greeting ("Hi", "Hello") combined with a project description
- Is notably longer than a normal single task request (project briefs tend to be multi-sentence)

**What this achieves:** Minimal code change. The model already handles classification well — this just adds a third bucket it can fall into. The behaviour is defined in the prompt, no orchestrator changes needed.

**Tradeoff:** Still model-dependent. Some models will correctly classify; less capable models may still jump to the task path.

---

### Option 2 — "Acknowledge Before Execute" Orchestrator Step
**Complexity: Medium | Impact: High**

When the Lead outputs a task list (JSON array found), **before spawning workers**, always dispatch a social acknowledgement message first.

The orchestrator currently dispatches the JSON → parses tasks → immediately loops into worker execution. Instead:

1. The Lead outputs both a social preamble *and* the JSON task list in the same response
2. The orchestrator strips the JSON, dispatches the human-readable portion as a chat message first
3. Then spawns workers

This means the user always sees: "Interesting project, I'll get the team started on X..." followed by task execution — rather than just seeing the `🎯` assignment messages.

**Implementation:** The current `decomposed` string (after stripping signals) is already dispatched in the `len(tasks)==0` branch. The same logic can be applied in the `len(tasks)>0` branch — if there's visible prose before the JSON array, dispatch it as an agent message before the workers begin.

**What this achieves:** Even when the Lead does jump to task execution, the user sees a human-readable acknowledgement. Social tone without blocking task execution.

**Tradeoff:** The Lead needs to be reliably prompted to produce a preamble sentence before the JSON. The prompt currently says "output JSON FIRST, then explain" — this would reverse that to "briefly acknowledge, then output JSON".

---

### Option 3 — Richer Default `persona.md` and `soul.md`
**Complexity: Low | Impact: Medium**

The current default persona for the Lead is:
> "Analytical, structured, and decisive. Communicates plans clearly and delegates effectively. Keeps Group Chat concise and milestone-focused."

This produces an efficient but cold agent. Add warmth, curiosity, and social behaviour to the defaults:

```markdown
## Communication Style
Analytical, structured, and decisive — but genuinely warm and engaged with the team. 
Communicates plans clearly and delegates effectively. 

In group chat, always acknowledges the Boss's input before responding 
to the task. When given a project brief or context, expresses genuine 
interest and asks a clarifying question before diving in. Avoids 
sounding robotic or transactional. Treats every interaction as a 
conversation with a colleague, not a ticket queue.
```

Similarly, the default `soul.md` has empty core values. Populate them by default:

```markdown
## Core Values
- People before process: acknowledge the human before the task
- Curiosity: ask questions, show genuine interest in the work
- Transparency: share reasoning, not just outcomes
```

**What this achieves:** Because `persona.md` and `soul.md` are part of the **system prompt** (not the per-message augmentation), these instructions are always present and visible to the model. This is a persistent "always be this way" instruction rather than a per-message directive.

**Tradeoff:** These defaults only affect newly created agents. Existing agents already have their `persona.md` and `soul.md` written to disk. The Boss would need to update them via the Brain editor or through a DM conversation with the agent.

---

### Option 4 — Align Channel Prompt with DM Prompt Structure
**Complexity: Low-Medium | Impact: Medium-High**

The DM prompt (`dmTurnPrompt`) already has a much better structure — it explicitly asks the model to:
1. **Understand** what type of message this is
2. Check tone before responding

The channel `decomposePrompt` skips both of these. Bring the DM prompt's "Understand + Tone" steps to the channel prompt.

Specifically, add as Step 0 in `decomposePrompt()`:

> **Step 0 — Understand the message type:**  
> Before doing anything, identify what this message is:
> - **(A) A specific executable task** — something with a clear output ("write X", "build Y", "find Z")
> - **(B) A project brief or context-setting** — the Boss is explaining what they'll be working on but isn't asking for immediate execution
> - **(C) Conversation** — a greeting, question, or discussion
> 
> For **(B)**: respond warmly, show interest, ask one clarifying question. Do NOT decompose tasks yet.  
> For **(A)** or **(C)**: continue to the steps below.

**What this achieves:** Makes the classification explicit rather than implicit. The model is guided through "what kind of message is this" before deciding what to do.

**Tradeoff:** Slightly longer prompt. Some models may still jump to task path.

---

### Option 5 — Explicit "Social Warm-Up" Handbook Section
**Complexity: Low | Impact: Medium**

Add a new section to the Handbook (`handbook.md`) specifically about social behaviour in the Group Chat:

```markdown
## Social Behaviour in Group Chat

The Group Chat is a workplace, not a ticket queue. 

**When the Boss sends an introductory or contextual message** (a project brief, 
"here's what we'll be working on", sharing background context), always respond 
as a team lead would: acknowledge warmly, show interest, ask a clarifying 
question. Do NOT immediately begin task decomposition.

**When the Boss sends a clear task instruction**, proceed with decomposition as 
described in the Decomposition Protocol.

**Signals that a message is introductory rather than a task request:**
- Contains future intent ("I want to", "we will be building", "you will help me")
- Contains a greeting followed by context
- Does not contain an explicit instruction verb ("write", "build", "create", "fix")

**Signals that a message is a task:**
- Clear imperative instruction ("write X", "build Y", "fix Z")
- Short, direct, action-oriented
```

Because the Handbook is always part of every agent's system prompt, this instruction will apply consistently to all agents, not just the Lead. It also gives clear heuristics to the model.

**Tradeoff:** The Handbook already affects every system prompt rebuild. Existing agents would need their `instruction.md` recompiled (which happens automatically when the Brain editor saves).

---

### Option 6 — Intent Pre-Classification (Two-Step Response)
**Complexity: High | Impact: Very High**

Before `decomposePrompt()` runs, add a **lightweight classification call** — a very short, fast prompt that classifies the message intent:

```
Classify the following message in ONE word:
TASK — if it's a direct instruction to do something specific
BRIEF — if it's context, introduction, or "here's what we'll be working on"  
CHAT — if it's a greeting, question, or general conversation
Message: {command}
Classification:
```

Then route accordingly:
- `TASK` → existing `decomposePrompt()` flow
- `BRIEF` → a new `briefAckPrompt()` that produces a warm acknowledgement
- `CHAT` → a `conversationPrompt()` for casual exchange

**What this achieves:** The most reliable option. Classification is a simpler task than generation, so even smaller models get it right. The routing decision is explicit in Go code, not inferred by the model inside a complex prompt.

**Tradeoff:** Every message now requires **two model calls** for the first response — a classification call and a generation call. On hardware where inference is slow, this could feel sluggish. However, the classification call is extremely short (single-word output) and could use a lightweight model.

---

### Option 7 — Confidence + Warm-up Linked Behaviour
**Complexity: Low | Impact: Medium**

When the Lead's first response is a task decomposition (JSON found), check whether the `decomposed` text also contains a warm preamble before the JSON. If not, **append a templated social message** after dispatching worker assignments.

For example, after all workers complete, instead of just the current generic summary:
> "All sub-tasks have been processed and fully examined."

Replace with a Lead summary that contextualises the work done:
> "Done — I've had the team {brief summary of what was done}. Let me know if you'd like me to adjust anything or dive deeper into any part."

This doesn't fix the "jumped straight to execution" issue but makes the **end of the interaction** feel much more like a colleague wrapping up a task.

**Tradeoff:** Only improves the tail of the interaction, not the start.

---

## Recommendation

The best outcome comes from combining three options:

1. **Option 3** (Richer default persona.md) — baseline improvement, always-on, no code changes
2. **Option 4** (Align channel prompt with DM structure) — low effort, high return, fixes the root prompt
3. **Option 2** (Acknowledge before execute) — ensures visible warmth even if classification fails

Option 6 (two-step classification) is the most robust but has latency cost — worth revisiting once the hardware profile of typical users is better understood.

---

## What NOT to Change

- **DM prompting** is already well-structured and handles context well — leave it alone
- **Worker prompts** should stay task-focused — workers are executors, not conversationalists
- **Confidence/escalation signals** are working correctly — don't change signal format
- **The JSON task list format** — keep `[{...}]` on one line, parseTaskList depends on it

