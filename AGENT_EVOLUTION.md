# Agent Evolution & Identity

## 1. Memory & Orientation
When an agent is initialized or given a new task, it performs an **Orientation Step**:
* **Corporate Alignment:** It reads `COMPANY_IDENTITY.md` and the `handbook.md` to ensure reasoning matches company values.
* **Recall:** It searches its `/journal` (using local vector search) to see if it has handled similar tasks or received specific Boss feedback in the past.

## 2. The Identity Filesystem (`/identity`)
* **`soul.md`**: Humor, temperament, and ethical leanings.
* **`persona.md`**: Professional role and communication style.
* **`skills.md`**: Mastered tasks and authorized MCP tools.
* **`instruction.md`**: The raw system prompt derived from the above files.

## 3. The Journaling Mechanism
Agents maintain a `journal/` directory for Reflection Entries (`YYYY-MM-DD-HHMM.md`). These logs are used for the agent's long-term growth and the Boss's performance reviews.