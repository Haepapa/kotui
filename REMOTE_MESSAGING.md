# Remote Presence & Messaging Integration

The "Remote Relay" is the primary interface for the "Pocket Boss" when the app runs in Docker.

## 1. Bi-Directional Sync
* **Mobile Boss:** Message the War Room via Telegram, Slack, or WhatsApp.
* **Noise Control:** Only high-level "Narrative" updates are sent to mobile to prevent fatigue.

## 2. Remote Commands
* `/status`: Returns current Heartbeat.
* `/approve [ID]`: Remotely onboard a candidate or approve a skill promotion.
* `/summary`: Request a recap of recent progress.