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
4. **Verification:** Lead (30B) inspects the result.
    * **Pass:** Lead summarizes the win in the Group Chat.
    * **Fail:** Lead sends the task back to the Specialist with a "Correction Note."

## 3. The Daily Standup (Growth & Memory)
At the end of every major task, agents must write a `journal.md` entry. Specialists can propose a change to their `skills.md` based on new lessons learned.