# UI/UX Specification: The War Room

## 1. Tiered Observability: The Mode Toggle
The interface features a global toggle to control information density:
* **Boss Mode (Toggle OFF):** A clean, high-level Group Chat. You see milestones, the "Lead" summarizing progress, and simple pulse animations for activity.
* **Dev Mode (Toggle ON):** The "Engine Room" expands. Detailed tool calls, raw LLM reasoning, and verification logs become visible in a terminal-like console.

## 2. Interaction Layers
* **The Group Chat:** Threaded conversation where the team coordinates. Workers post "milestone" updates and links to generated assets.
* **The One-on-One Sidebar:** Clicking an agent's avatar opens a private channel for direct feedback. This is used for "Performance Reviews" where the Boss can correct an agent's tone or logic privately.
* **The Candidate Trial Window:** A temporary, restricted chat used during the "Hiring" phase to interview new specialists before they join the War Room.

## 3. The Heartbeat (Activity Bar)
* **Location:** Persistent status bar at the bottom of the UI.
* **Visuals:** A subtle "pulse" animation indicating the engine is alive.
* **Breadcrumbs:** Displays the current high-level state (e.g., `Planning > [Coding] > Testing`).