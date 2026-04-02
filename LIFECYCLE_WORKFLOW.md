# Agent Lifecycle & Workflow

## 1. The Hiring & Interview Phase
When the Lead Agent identifies a skill gap, the following workflow is triggered:
* **The Request:** Lead posts a "Hiring Proposal" in the Group Chat.
* **The Vibe Check:** The candidate is spawned in a **Sandbox Mode**. The Boss conducts an interview in a private chat to test technical logic and "Company Fit."
* **Onboarding:** Upon approval, the agent is granted MCP credentials and introduced to the Group Chat.

## 2. The Verification Loop (Verify-then-Proceed)
1. **Assignment:** Lead sends a sub-task and specific MCP tools to the Specialist.
2. **Work Phase:** Specialist executes tool calls. Every action is logged to the "Engine Room" (Console) in real-time.
3. **Drafting:** Specialist posts a "Draft Result" to the Lead (not yet visible to the Boss).
4. **Verification:** Lead inspects the result.
    * **Pass:** Lead summarizes the win in the Group Chat.
    * **Fail:** Lead sends the task back to the Specialist with a "Correction Note."

## 3. Capability Escalation (Senior Consultant)
The Primary Lead — regardless of its parameter size — includes a **self-assessment step** before accepting a task. If it determines the task exceeds its reliable reasoning capacity, it emits a `capability_escalation` signal instead of attempting the task directly:

1. **Signal:** Lead posts an escalation notice to the Group Chat ("This task requires a Senior Consultant").
2. **Routing:** The Dispatcher checks whether a Senior Consultant is configured.
   * **If available:** The Dispatcher connects to the Senior Consultant's endpoint (waking the remote host if needed), routes the sub-task, and returns the result to the Lead.
   * **If not configured:** The Dispatcher notifies the Boss and pauses the task pending configuration of a Senior Consultant endpoint.
3. **Synthesis:** The Lead receives the Senior Consultant's output and uses it to continue orchestration.
4. **Release:** The Senior Consultant is unloaded/disconnected after the task to free resources.

This allows an 8B Primary Lead running 24/7 on a low-power device to still tackle complex projects — it simply calls in extra capacity when needed rather than hallucinating through tasks it can't reliably complete.

## 4. The Daily Standup (Growth & Memory)
At the end of every major task, agents must write a `journal.md` entry. Specialists can propose a change to their `skills.md` based on new lessons learned.