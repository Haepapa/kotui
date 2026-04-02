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
  dmMessages: [] as KotuiMessage[],
  dmRawMessages: [] as KotuiMessage[],
  // DM streaming state
  isDMBusy: false,
  dmStreamContent: '',   // accumulates streamed tokens; cleared when final message arrives
});

// --- Derived helpers (functions because .svelte.ts can't use $derived at module scope with runes) ---

export function visibleMessages(): KotuiMessage[] {
  return wr.viewMode === 'boss'
    ? wr.messages.filter((m) => m.tier === 'summary')
    : wr.messages;
}

export function engineRoomMessages(): KotuiMessage[] {
  if (wr.activeView === 'dm') {
    return wr.dmRawMessages;
  }
  return wr.messages.filter((m) => m.tier === 'raw');
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
    // Route messages to the right conversation buffer.
    if (msg.conversation_id && msg.conversation_id === wr.activeDMConvID) {
      // Message belongs to the active DM — split by tier.
      if (msg.tier === 'raw') {
        wr.dmRawMessages.push(msg);
      } else {
        // Final summary response: clear streaming content so the bubble is replaced.
        wr.dmStreamContent = '';
        wr.dmMessages.push(msg);
      }
    } else {
      // Message belongs to the war-room channel.
      wr.messages.push(msg);
    }
    if (msg.kind === 'boss_command' || msg.kind === 'agent_message') {
      wr.isBusy = true;
    }
    if (msg.kind === 'milestone' || msg.kind === 'system_event') {
      wr.isBusy = false;
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

  // DM agent busy state — controls typing indicator.
  unsubDMBusy = Events.On('kotui:dm_busy', (event: any) => {
    const payload = event?.data as { conversation_id: string; busy: boolean };
    if (payload?.conversation_id === wr.activeDMConvID) {
      wr.isDMBusy = payload.busy;
      if (!payload.busy) {
        // Agent finished — ensure streaming content is cleared (belt-and-suspenders).
        wr.dmStreamContent = '';
      }
    }
  });

  // Streaming token chunks — build up the live preview bubble.
  unsubDMStream = Events.On('kotui:dm_stream', (event: any) => {
    const payload = event?.data as { conversation_id: string; chunk: string };
    if (payload?.conversation_id === wr.activeDMConvID) {
      wr.dmStreamContent += payload.chunk;
    }
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
    wr.isDMBusy = false;
    wr.dmStreamContent = '';
    const allMsgs = (await getMessages(convID, 200)) ?? [];
    wr.dmMessages = allMsgs.filter((m) => m.tier !== 'raw');
    wr.dmRawMessages = allMsgs.filter((m) => m.tier === 'raw');
    wr.activeView = 'dm';
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
