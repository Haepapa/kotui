// warroom.svelte.ts — Svelte 5 reactive state for the War Room.
//
// All state is contained in a single exported object so that mutations
// go through property assignment (which Svelte 5 allows) rather than
// reassigning exported primitives (which it forbids).

import {
  getProjects,
  getAgents,
  getHeartbeat,
  getActiveConversation,
  getMessages,
  onMessage,
  onHeartbeat,
  onError,
  onApproval,
  getPendingApprovals,
  getOrCreateDirectConversation,
  renameProject,
  archiveProject,
} from '../lib/warroom';
import { Events } from '@wailsio/runtime';
import type { AgentInfo, AppView, Approval, HeartbeatState, KotuiMessage, Project, ViewMode } from '../lib/types';

// --- Reactive state object ---

export const wr = $state({
  projects: [] as Project[],
  activeProjectID: '',
  activeConvID: '',
  messages: [] as KotuiMessage[],
  agents: [] as AgentInfo[],
  heartbeat: {
    is_healthy: true,
    phase: 'Idle',
    breadcrumbs: ['Idle'],
    active_count: 0,
    vram_profile: '',
    updated_at: new Date().toISOString(),
  } as HeartbeatState,
  viewMode: 'boss' as ViewMode,
  errorBanner: '',
  isBusy: false,
  approvals: [] as Approval[],
  activeView: 'chat' as AppView,
  activeDMAgentID: '',
  activeDMConvID: '',

  // Per-conversation DM state — keyed by convID.
  // This allows multiple DMs to run independently in the background:
  // navigating away does not reset in-progress state for another conv.
  dmConvMsgs:   {} as Record<string, KotuiMessage[]>, // summary messages
  dmConvRaw:    {} as Record<string, KotuiMessage[]>, // raw/dev messages
  dmConvBusy:   {} as Record<string, boolean>,         // typing indicator
  dmConvStream: {} as Record<string, string>,           // live streaming content
});

// --- Derived helpers (functions because .svelte.ts can't use $derived at module scope with runes) ---

export function visibleMessages(): KotuiMessage[] {
  return wr.viewMode === 'boss'
    ? wr.messages.filter((m) => m.tier === 'summary')
    : wr.messages;
}

export function engineRoomMessages(): KotuiMessage[] {
  if (wr.activeView === 'dm') {
    return wr.dmConvRaw[wr.activeDMConvID] ?? [];
  }
  return wr.messages.filter((m) => m.tier === 'raw');
}

// --- Helpers ---

/** Returns true if convID belongs to a known DM conversation. */
function isDMConv(convID: string): boolean {
  return convID in wr.dmConvMsgs || convID === wr.activeDMConvID;
}

/** Ensure convID is registered in the DM maps (idempotent). */
function ensureDMConv(convID: string) {
  if (!(convID in wr.dmConvMsgs))   wr.dmConvMsgs[convID]   = [];
  if (!(convID in wr.dmConvRaw))    wr.dmConvRaw[convID]    = [];
  if (!(convID in wr.dmConvBusy))   wr.dmConvBusy[convID]   = false;
  if (!(convID in wr.dmConvStream)) wr.dmConvStream[convID] = '';
}

// --- Lifecycle ---

let unsubMessage: (() => void) | null = null;
let unsubHeartbeat: (() => void) | null = null;
let unsubError: (() => void) | null = null;
let unsubApproval: (() => void) | null = null;
let unsubProjects: (() => void) | null = null;
let unsubDMBusy: (() => void) | null = null;
let unsubDMStream: (() => void) | null = null;

export async function initWarRoom() {
  unsubMessage = onMessage((msg) => {
    const cid = msg.conversation_id;

    if (cid && isDMConv(cid)) {
      // Route to the appropriate DM conversation buffer.
      ensureDMConv(cid);
      if (msg.tier === 'raw') {
        wr.dmConvRaw[cid] = [...(wr.dmConvRaw[cid] ?? []), msg];
      } else {
        // Final summary response — clear live stream so the bubble is replaced.
        wr.dmConvStream[cid] = '';
        wr.dmConvMsgs[cid] = [...(wr.dmConvMsgs[cid] ?? []), msg];
      }
    } else {
      // War-room channel message.
      wr.messages.push(msg);
      if (msg.kind === 'boss_command' || msg.kind === 'agent_message') {
        wr.isBusy = true;
      }
      if (msg.kind === 'milestone' || msg.kind === 'system_event') {
        wr.isBusy = false;
      }
    }
  });

  unsubHeartbeat = onHeartbeat((hb) => {
    Object.assign(wr.heartbeat, hb);
  });

  unsubError = onError((e) => {
    wr.errorBanner = e.error;
    wr.isBusy = false;
    setTimeout(() => (wr.errorBanner = ''), 8000);
  });

  unsubApproval = onApproval((approvals) => {
    wr.approvals = approvals ?? [];
  });

  // Refresh project list whenever the backend signals a change.
  unsubProjects = Events.On('kotui:projects', (event: any) => {
    const projects: Project[] = (event?.data as Project[]) ?? [];
    wr.projects = projects;
    const active = projects.find((p) => p.active);
    if (active && active.id !== wr.activeProjectID) {
      wr.activeProjectID = active.id;
    }
  });

  // DM agent busy state — register convID and control typing indicator.
  // Fired before inference starts (busy:true) and after it ends (busy:false).
  unsubDMBusy = Events.On('kotui:dm_busy', (event: any) => {
    const payload = event?.data as { conversation_id: string; busy: boolean };
    if (!payload?.conversation_id) return;
    const cid = payload.conversation_id;
    ensureDMConv(cid); // register as a known DM conv so messages route here
    wr.dmConvBusy[cid] = payload.busy;
    if (!payload.busy) {
      wr.dmConvStream[cid] = ''; // belt-and-suspenders clear
    }
  });

  // Streaming token chunks — accumulate per conversation.
  unsubDMStream = Events.On('kotui:dm_stream', (event: any) => {
    const payload = event?.data as { conversation_id: string; chunk: string };
    if (!payload?.conversation_id) return;
    const cid = payload.conversation_id;
    ensureDMConv(cid);
    wr.dmConvStream[cid] = (wr.dmConvStream[cid] ?? '') + payload.chunk;
  });

  try {
    const [projects, agents, hb] = await Promise.all([
      getProjects(),
      getAgents(),
      getHeartbeat(),
    ]);
    wr.projects = projects ?? [];
    wr.agents = agents ?? [];
    Object.assign(wr.heartbeat, hb);

    const active = wr.projects.find((p) => p.active);
    if (active) {
      wr.activeProjectID = active.id;
      wr.activeConvID = (await getActiveConversation()) ?? '';
      if (wr.activeConvID) {
        wr.messages = (await getMessages(wr.activeConvID, 200)) ?? [];
      }
    }
  } catch (e) {
    console.warn('warroom init:', e);
  }

  await refreshApprovals();
}

export function destroyWarRoom() {
  unsubMessage?.();
  unsubHeartbeat?.();
  unsubError?.();
  unsubApproval?.();
  unsubProjects?.();
  unsubDMBusy?.();
  unsubDMStream?.();
}

export function toggleMode() {
  wr.viewMode = wr.viewMode === 'boss' ? 'dev' : 'boss';
}

export function switchToChat() {
  wr.activeView = 'chat';
}

export function switchToSettings() {
  wr.activeView = 'settings';
}

export function switchToIdentity() {
  wr.activeView = 'identity';
}

export async function openDM(agentID: string) {
  if (!wr.activeProjectID) {
    wr.errorBanner = 'Select a channel before opening a direct message.';
    setTimeout(() => (wr.errorBanner = ''), 5000);
    return;
  }
  try {
    const convID = await getOrCreateDirectConversation(agentID);
    if (!convID) throw new Error('No conversation ID returned');

    wr.activeDMAgentID = agentID;
    wr.activeDMConvID = convID;
    wr.activeView = 'dm';

    // Only load from DB if we don't already have messages in memory.
    // If the DM is currently in-flight (busy), preserve streaming state.
    if (!(convID in wr.dmConvMsgs)) {
      ensureDMConv(convID);
      const allMsgs = (await getMessages(convID, 200)) ?? [];
      wr.dmConvMsgs[convID] = allMsgs.filter((m) => m.tier !== 'raw');
      wr.dmConvRaw[convID]  = allMsgs.filter((m) => m.tier === 'raw');
    }
  } catch (e) {
    console.error('openDM:', e);
    wr.errorBanner = `Could not open DM: ${e instanceof Error ? e.message : String(e)}`;
    setTimeout(() => (wr.errorBanner = ''), 6000);
  }
}

export async function refreshApprovals() {
  try {
    const approvals = await getPendingApprovals();
    wr.approvals = approvals ?? [];
  } catch (e) {
    console.warn('refreshApprovals:', e);
  }
}


export async function renameChannel(id: string, name: string, description: string): Promise<void> {
  await renameProject(id, name, description);
}

export async function archiveChannel(id: string): Promise<void> {
  await archiveProject(id);
}

