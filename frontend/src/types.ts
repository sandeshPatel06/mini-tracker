// Type declarations for Wails Go bindings
export interface LogEntry {
  id: number;
  timestamp: string;
  image_path: string;
  total_keys: number;
  unique_keys: number;
  entropy_score: number;
  ai_category: string;
  is_productive: boolean;
  ai_confidence: number;
  ai_reason: string;
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

export type Page = 'dashboard' | 'timeline' | 'analytics' | 'organization' | 'accept-invite';
