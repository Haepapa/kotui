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
} from '../lib/warroom';
import type { AgentInfo, HeartbeatState, KotuiMessage, Project, ViewMode } from '../lib/types';

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
});

// --- Derived helpers (functions because .svelte.ts can't use $derived at module scope with runes) ---

export function visibleMessages(): KotuiMessage[] {
  return wr.viewMode === 'boss'
    ? wr.messages.filter((m) => m.tier === 'summary')
    : wr.messages;
}

export function engineRoomMessages(): KotuiMessage[] {
  return wr.messages.filter((m) => m.tier === 'raw');
}

// --- Lifecycle ---

let unsubMessage: (() => void) | null = null;
let unsubHeartbeat: (() => void) | null = null;
let unsubError: (() => void) | null = null;

export async function initWarRoom() {
  unsubMessage = onMessage((msg) => {
    wr.messages.push(msg);
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
}

export function destroyWarRoom() {
  unsubMessage?.();
  unsubHeartbeat?.();
  unsubError?.();
}

export function toggleMode() {
  wr.viewMode = wr.viewMode === 'boss' ? 'dev' : 'boss';
}

