// Desktop Wails runtime interop and bindings
import { LogEntry, ProductivityStats, AppConfig } from '../types';

declare const window: Window & {
  go?: {
    main: {
      App: {
        GetTodayLogs: () => Promise<LogEntry[]>;
        GetLogsByDate: (date: string) => Promise<LogEntry[]>;
        GetStats: (date: string) => Promise<ProductivityStats>;
        GetConfig: () => Promise<AppConfig>;
        GetTodayTrackedSeconds: () => Promise<number>;
        GetTrackingStatus: () => Promise<{ active?: boolean; elapsed_seconds?: number } | null>;
        ToggleTracking: () => Promise<{ active?: boolean; elapsed_seconds?: number } | null>;
        SetActiveTask: (task: string) => Promise<string>;
        GetActiveTask: () => Promise<string>;
        RecordInputActivity: (totalKeys: number, uniqueKeys: number) => Promise<void>;
        RecordMouseActivity: (clicks: number) => Promise<void>;
        ClearAllLocalData: () => Promise<boolean>;
        GetImageBase64: (imagePath: string) => Promise<string>;
        UpdateGeminiAPIKey: (apiKey: string) => Promise<boolean>;
        UpdateAIModel: (modelName: string) => Promise<boolean>;
        UpdateScreenshotInterval: (seconds: number) => Promise<boolean>;
        SetAuthSession: (token: string, isGuest: boolean) => Promise<void>;
        OpenTrackerWizard?: (token: string) => Promise<void>;
        TriggerSyncNow?: () => Promise<void>;
      };
    };
  };
};

export const isDesktopApp = (): boolean => {
  return typeof window !== 'undefined' && Boolean(window.go?.main?.App);
};

export function callGo<T>(fn: () => Promise<T>): Promise<T | null> {
  try {
    return fn().catch((err) => {
      console.warn('[Wails] Desktop binding call failed:', err);
      return null;
    });
  } catch (err) {
    console.warn('[Wails] Synchronous desktop binding exception:', err);
    return Promise.resolve(null);
  }
}

export function syncBackendAuthSession(token: string, isGuest: boolean): void {
  if (window.go?.main?.App?.SetAuthSession) {
    callGo(() => window.go!.main.App.SetAuthSession(token, isGuest));
  }
}

export function openTrackerWizard(token: string): void {
  if (window.go?.main?.App?.OpenTrackerWizard) {
    callGo(() => window.go!.main.App.OpenTrackerWizard!(token));
  }
}
