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
  onAgents,
  onChannelBusy,
  onChannelStream,
  onBrainUpdate,
  getPendingApprovals,
  getOrCreateDirectConversation,
  renameProject,
  archiveProject,
  createProject,
  initFirstRun,
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
  activeBrainAgentID: '',

  // Per-conversation DM state — keyed by convID.
  // This allows multiple DMs to run independently in the background:
  // navigating away does not reset in-progress state for another conv.
  dmConvMsgs:   {} as Record<string, KotuiMessage[]>, // summary messages
  dmConvRaw:    {} as Record<string, KotuiMessage[]>, // raw/dev messages
  dmConvBusy:   {} as Record<string, boolean>,         // typing indicator
  dmConvStream: {} as Record<string, string>,           // live streaming content

  // Unread notification counts — frontend-only, reset on app restart.
  // Keyed by agentID for DMs, and projectID for channels.
  unreadDM:      {} as Record<string, number>,
  unreadChannel: {} as Record<string, number>,
  // Reverse-lookup: convID → agentID, used to route incoming messages to the right badge.
  dmAgentByConv: {} as Record<string, string>,

  // Live streaming content for channel chat — cleared when the final message arrives.
  channelStream: '',
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

/**
 * Returns the display name for an agentID by looking up wr.agents.
 * Falls back to the raw agentID if no match is found.
 */
export function agentName(agentID: string): string {
  if (!agentID) return '';
  return wr.agents.find((a) => a.id === agentID)?.name ?? agentID;
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
let unsubAgents: (() => void) | null = null;
let unsubChannelBusy: (() => void) | null = null;
let unsubChannelStream: (() => void) | null = null;
let unsubBrainUpdate: (() => void) | null = null;
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
        // Increment unread badge when the user isn't actively viewing this DM.
        if (msg.agent_id !== 'boss' && (wr.activeView !== 'dm' || wr.activeDMConvID !== cid)) {
          const agentID = wr.dmAgentByConv[cid];
          if (agentID) {
            wr.unreadDM[agentID] = (wr.unreadDM[agentID] ?? 0) + 1;
          }
        }
      }
    } else {
      // War-room channel message — use array replacement for reliable reactivity.
      // Clear the live stream when a final summary message arrives so the stream
      // bubble is replaced by the real message bubble (same as DM behaviour).
      if (msg.tier !== 'raw' && (msg.kind === 'agent_message' || msg.kind === 'milestone')) {
        wr.channelStream = '';
      }
      wr.messages = [...wr.messages, msg];
      // Increment channel unread when the user isn't watching this channel's chat.
      if (msg.tier !== 'raw' && msg.agent_id !== 'boss' &&
          (wr.activeView !== 'chat') && msg.project_id) {
        wr.unreadChannel[msg.project_id] = (wr.unreadChannel[msg.project_id] ?? 0) + 1;
      }
    }
  });

  // Channel busy state — driven by kotui:channel_busy, NOT by message kinds.
  // This prevents raw system_event log messages from clearing the typing indicator.
  unsubChannelBusy = onChannelBusy((busy) => {
    wr.isBusy = busy;
    // Belt-and-suspenders: clear any residual stream when the channel goes idle.
    if (!busy) wr.channelStream = '';
  });

  // Channel streaming chunks — accumulate per response, cleared on message arrival.
  unsubChannelStream = onChannelStream((payload) => {
    wr.channelStream = (wr.channelStream ?? '') + payload.chunk;
  });

  // Brain file update notifications — adds a system_event to the agent's DM
  // conversation and increments the unread badge.
  unsubBrainUpdate = onBrainUpdate((payload) => {
    const { agent_id, conv_id, message } = payload;
    if (conv_id) {
      if (!wr.dmConvMsgs[conv_id]) wr.dmConvMsgs[conv_id] = [];
      wr.dmConvMsgs[conv_id] = [...wr.dmConvMsgs[conv_id], message];
      // Show unread badge unless the user is actively viewing this DM.
      if (wr.activeView !== 'dm' || wr.activeDMConvID !== conv_id) {
        wr.unreadDM[agent_id] = (wr.unreadDM[agent_id] ?? 0) + 1;
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

  unsubAgents = onAgents((agents) => {
    wr.agents = agents ?? [];
  });

  // Refresh project list whenever the backend signals a change.
  unsubProjects = Events.On('kotui:projects', async (event: any) => {
    const projects: Project[] = (event?.data as Project[]) ?? [];
    wr.projects = projects;
    const active = projects.find((p) => p.active);
    if (active && active.id !== wr.activeProjectID) {
      wr.activeProjectID = active.id;
      // Only switch to chat if the user isn't currently in a DM — we don't
      // want a background project change to yank them out of a conversation.
      if (wr.activeView !== 'dm') {
        wr.activeView = 'chat';
        wr.messages = [];
        wr.activeConvID = (await getActiveConversation()) ?? '';
        if (wr.activeConvID) {
          wr.messages = (await getMessages(wr.activeConvID, 200)) ?? [];
        }
      }
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

    // First-run bootstrap: if no projects exist, create the default workspace and
    // seed the Lead Agent's DM with a greeting so the user has a clear starting point.
    if (wr.projects.length === 0) {
      const result = await initFirstRun();
      if (result?.is_new && result?.conv_id) {
        // Re-load projects so activeProjectID is set correctly.
        wr.projects = (await getProjects()) ?? [];
        const newActive = wr.projects.find((p) => p.active);
        if (newActive) wr.activeProjectID = newActive.id;

        // Register the lead DM conv so messages route to the right buffer.
        const convID = result.conv_id;
        ensureDMConv(convID);
        wr.dmAgentByConv[convID] = 'lead';

        // Load the greeting from the DB and mark it as unread.
        const msgs = (await getMessages(convID, 10)) ?? [];
        wr.dmConvMsgs[convID] = msgs.filter((m) => m.tier !== 'raw');
        if (wr.dmConvMsgs[convID].length > 0) {
          wr.unreadDM['lead'] = wr.dmConvMsgs[convID].length;
        }
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
  unsubAgents?.();
  unsubChannelBusy?.();
  unsubChannelStream?.();
  unsubBrainUpdate?.();
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

export function switchToBrain(agentID: string) {
  wr.activeBrainAgentID = agentID;
  wr.activeView = 'brain';
}

export function switchFromBrain() {
  // Return to the DM that was open before entering the brain view.
  if (wr.activeDMAgentID) {
    wr.activeView = 'dm';
  } else {
    wr.activeView = 'chat';
  }
}

export async function openDM(agentID: string) {
  // On a fresh install there's no active project yet. Auto-create a default
  // workspace so the user can immediately talk to the Lead Agent without having
  // to manually create a channel first.
  if (!wr.activeProjectID) {
    try {
      const proj = await createProject('General', 'Default workspace');
      if (proj) {
        // Set activeProjectID immediately so the kotui:projects event handler
        // (which also sets it) won't trigger a view switch to chat.
        wr.activeProjectID = proj.id;
      }
    } catch (e) {
      wr.errorBanner = 'Could not create a workspace. Please create a channel first.';
      setTimeout(() => (wr.errorBanner = ''), 6000);
      return;
    }
    if (!wr.activeProjectID) return;
  }
  try {
    const convID = await getOrCreateDirectConversation(agentID);
    if (!convID) throw new Error('No conversation ID returned');

    // Register the conv→agent mapping for unread routing, then clear the badge.
    wr.dmAgentByConv[convID] = agentID;
    wr.unreadDM[agentID] = 0;

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

