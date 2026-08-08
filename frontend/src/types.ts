// Type declarations for Wails Go bindings
export interface LogEntry {
  id: number;
  timestamp: string;
  image_path: string;
  total_keys: number;
  unique_keys: number;
  entropy_score: number;
  app_name?: string;
  app_category?: string;
  window_title?: string;
  session_id?: number;
  session_title?: string;
  ai_category: string;
  is_productive: boolean;
  productive_score?: number;
  ai_confidence: number;
  ai_reason: string;
}

export interface WorkSession {
  id: number;
  org_id?: number;
  user_id?: number;
  title: string;
  summary: string;
  start_time: string;
  end_time: string;
  log_count: number;
  productive_pct: number;
  top_app_name?: string;
  top_category?: string;
}

export interface ProductivityStats {
  date: string;
  total_minutes: number;
  productive_minutes: number;
  unproductive_minutes: number;
  avg_entropy_score: number;
  top_category: string;
}

export interface AppConfig {
  screenshot_interval_seconds: number;
  data_dir: string;
  ai_configured: boolean;
  backend_endpoint?: string;
}

export interface Organization {
  id: number;
  name: string;
  slug: string;
  created_at: string;
}

export interface User {
  id: number;
  org_id: number;
  email: string;
  full_name: string;
  role: 'owner' | 'admin' | 'member';
  created_at: string;
}

export interface Invitation {
  id: number;
  org_id: number;
  email: string;
  role: 'admin' | 'member';
  token: string;
  status: 'pending' | 'accepted' | 'expired';
  expires_at: string;
  created_at: string;
}

export type Page = 'dashboard' | 'timeline' | 'analytics' | 'organization' | 'accept-invite' | 'auth' | 'settings';
