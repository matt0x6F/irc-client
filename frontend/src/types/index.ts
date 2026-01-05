export interface Server {
  id: number;
  name: string;
  address: string;
  port: number;
  tls: boolean;
  nickname: string;
  username: string;
  realname: string;
  password: string;
  created_at: string;
  updated_at: string;
}

export interface Channel {
  id: number;
  server_id: number;
  name: string;
  auto_join: boolean;
  created_at: string;
}

export interface Message {
  id: number;
  server_id: number;
  channel_id: number | null;
  user: string;
  message: string;
  message_type: string;
  timestamp: string;
  raw_line: string;
}

export interface ServerConfig {
  name: string;
  address: string;
  port: number;
  tls: boolean;
  nickname: string;
  username: string;
  realname: string;
  password: string;
}

export interface PluginInfo {
  name: string;
  version: string;
  description?: string;
  author?: string;
  events?: string[];
  permissions?: string[];
  path: string;
  enabled: boolean;
}

