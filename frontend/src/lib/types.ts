// Shared TypeScript types mirroring the Go models and warroom service types.

export type MessageKind =
  | 'boss_command'
  | 'agent_message'
  | 'tool_call'
  | 'tool_result'
  | 'milestone'
  | 'system_event'
  | 'draft';

export type LogTier = 'summary' | 'raw';

export interface KotuiMessage {
  id: string;
  project_id: string;
  conversation_id: string;
  agent_id: string;
  kind: MessageKind;
  tier: LogTier;
  content: string;
  metadata: string;
  created_at: string;
}

export interface Project {
  id: string;
  name: string;
  description: string;
  data_path: string;
  active: boolean;
  created_at: string;
}

export interface AgentInfo {
  id: string;
  name: string;
  role: 'lead' | 'specialist' | 'trial';
  status: 'idle' | 'working' | 'parked' | 'offline' | 'onboarded' | 'rejected';
  model: string;
}

export interface HeartbeatState {
  is_healthy: boolean;
  phase: string;
  breadcrumbs: string[];
  active_count: number;
  vram_profile: string;
  updated_at: string;
}

export interface QueueState {
  p0: number;
  p1: number;
  p2: number;
  p3: number;
  active: boolean;
  throttled: boolean;
}

export type ViewMode = 'boss' | 'dev';

export interface Approval {
  id: string;
  project_id: string;
  kind: string;
  subject_id: string;
  description: string;
  status: string;
  created_at: string;
  decided_at?: string;
}

export interface UIConfig {
  ollama_endpoint: string;
  lead_model: string;
  worker_model: string;
  embedder_model: string;
  senior_model: string;
  senior_endpoint: string;
  senior_ssh_host: string;
  senior_ssh_cmd: string;
  timezone: string;
  telegram_bot_token: string;
  telegram_chat_id: string;
  slack_bot_token: string;
  slack_channel_id: string;
  slack_signing_secret: string;
  whatsapp_token: string;
  whatsapp_phone_number_id: string;
  whatsapp_verify_token: string;
  webhook_secret: string;
  webhook_port: number;
}

export type AppView = 'chat' | 'settings' | 'identity' | 'dm' | 'brain';
