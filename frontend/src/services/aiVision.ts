import { LogEntry } from '../types';
import { apiFetch } from '../api';
import { bumpUsageCount } from '../utils';
import { callGo } from './wailsBridge';

// Robust parsing of AI responses (handles markdown fences & truncated JSON strings)
export function parseAIJSONResponse(raw: string): any[] | null {
  if (!raw) return null;
  let clean = raw.trim();
  if (clean.startsWith('```')) {
    clean = clean.replace(/^```[a-z]*\n?/i, '').replace(/\n?```$/i, '').trim();
  }
  try {
    const res = JSON.parse(clean);
    if (Array.isArray(res)) return res;
  } catch {
    // Attempt auto-repair for truncated JSON array
    const startIdx = clean.indexOf('[');
    if (startIdx !== -1) {
      let arrayStr = clean.substring(startIdx);
      if (!arrayStr.endsWith(']')) {
        // Remove trailing unclosed object/comma and close array
        const lastObjEnd = arrayStr.lastIndexOf('}');
        if (lastObjEnd !== -1) {
          arrayStr = arrayStr.substring(0, lastObjEnd + 1) + ']';
          try {
            const res = JSON.parse(arrayStr);
            if (Array.isArray(res)) return res;
          } catch {}
        }
      }
    }
  }
  return null;
}

// Loads the screenshot at imagePath as a data URL. Wails uses GetImageBase64;
// web mode fetches /api/image and converts the blob via FileReader.
export async function loadImageAsDataUrl(imagePath: string): Promise<string | null> {
  if (!imagePath) return null;
  const w = window as any;
  if (w.go?.main?.App?.GetImageBase64) {
    const dataUrl = await callGo<string>(() => w.go.main.App.GetImageBase64(imagePath));
    return typeof dataUrl === 'string' && dataUrl.startsWith('data:image/') ? dataUrl : null;
  }
  try {
    const res = await apiFetch(`/api/image?path=${encodeURIComponent(imagePath)}`);
    if (!res.ok) return null;
    const blob = await res.blob();
    return await new Promise<string | null>((resolve) => {
      const reader = new FileReader();
      reader.onload = () => {
        const result = typeof reader.result === 'string' ? reader.result : '';
        resolve(result.startsWith('data:image/') ? result : null);
      };
      reader.onerror = () => resolve(null);
      reader.readAsDataURL(blob);
    });
  } catch {
    return null;
  }
}

export interface VisionAnalysisOptions {
  candidateLogs: LogEntry[];
  apiKey: string;
  model: string;
  intervalSec: number;
  userTask: string;
  submittedIds: Set<number>;
  resultCache: Map<number, Partial<LogEntry>>;
  setLogs: (updater: (prev: LogEntry[]) => LogEntry[]) => void;
}

// Sends a real multimodal bundle of pending captures to Gemini. Only values the
// model actually returned are applied; items whose image cannot be loaded are
// skipped and left pending instead of being fabricated.
export async function runClientVisionAnalysis(options: VisionAnalysisOptions): Promise<void> {
  const { candidateLogs, apiKey, model, intervalSec, userTask, submittedIds, resultCache, setLogs } = options;

  const targetRequestFrequencySec = Math.max(60, Math.ceil(60 / (model.includes('gemini-2.5-pro') || model.includes('gemini-1.5-pro') ? 2 : 15)));
  const targetBundleSize = Math.max(3, Math.floor(targetRequestFrequencySec / Math.max(5, intervalSec)));
  const bundle = candidateLogs.slice(0, Math.min(Math.max(3, targetBundleSize), 6));

  const loadedItems: { item: LogEntry; text: string; inlineData: { mimeType: string; data: string } }[] = [];
  for (const item of bundle) {
    if (!item.image_path) continue;
    const dataUrl = await loadImageAsDataUrl(item.image_path);
    if (!dataUrl || !dataUrl.includes(',')) continue;
    const mimeType = dataUrl.substring(dataUrl.indexOf(':') + 1, dataUrl.indexOf(';')) || 'image/jpeg';
    loadedItems.push({
      item,
      text: `[Item ${loadedItems.length + 1} | Timestamp: ${item.timestamp} | Window Title: ${item.window_title || 'unknown'} | App Name: ${item.app_name || 'unknown'} | App Category: ${item.app_category || 'unknown'} | Total Keys: ${item.total_keys || 0} | Unique Keys: ${item.unique_keys || 0} | Keystroke Entropy: ${(item.entropy_score || 0).toFixed(1)} | Mouse Clicks: ${item.total_clicks || 0}]`,
      inlineData: { mimeType, data: dataUrl.split(',')[1] },
    });
  }

  // Require real images for the batch — never fabricate missing ones.
  if (loadedItems.length < 3) return;

  const taskGoal = userTask.trim()
    ? `CURRENT TARGET USER TASK (GOAL): "${userTask.trim()}"\nEVALUATION RULE: Analyze how relevant the visible desktop content and user input (typing/mouse) are to achieving THIS SPECIFIC TASK. High scores require direct alignment with this task context.`
    : 'CURRENT TARGET USER TASK: General software development and workstation tasks.';

  const promptText = `You are an expert, objective developer productivity analyst inspecting a sequence of ${loadedItems.length} desktop screenshots captured from a Linux workstation in chronological order.

${taskGoal}

CRITICAL INSTRUCTIONS FOR EACH SCREENSHOT ITEM:
1. Multi-Monitor Grid Inspection: Each image may be a composite grid of multiple monitors. Inspect ALL screens visible in the grid.
2. Application & Context Extraction: Identify the primary active application (app_name: e.g., VS Code, Terminal, Chrome, Slack, Spotify), open file paths/code snippet text, documentation title, or window title (window_title) from the image AND the provided per-item metadata.
3. Telemetry & Task Alignment Scoring — use ALL provided signals dynamically:
   - Direct work on stated task: +40 to +60 pts
   - Related docs / technical research: +30 to +45 pts
   - Unrelated browsing / messaging: +10 to +30 pts
   - Leisure / social media / idle: 0 to 15 pts
   - Keystroke Entropy > 20: Active typing/writing (+25 pts)
   - Keystroke Entropy 8-20: Moderate typing (+15 pts)
   - Keystroke Entropy < 8: Reading/idle mode — lowers score unless offset by heavy mouse-click activity
   - Mouse Clicks > 20: Interactive session (+5 pts)
   - Combined low entropy + few clicks + off-task screen = score 0-20 (NEVER default to 100).

Return ONLY a valid JSON array of ${loadedItems.length} objects in exact input order. Each object must contain exactly these fields:
- "item_index": number — 1-based index matching the input item order.
- "app_name": string — the primary active application actually visible, or an empty string if unreadable.
- "app_category": string — application class (e.g., "IDE / Code Editor"), or an empty string.
- "window_title": string — the visible window title or document name, or an empty string.
- "category": string — productivity category (e.g., "Coding", "Browsing", "Communication"), or an empty string.
- "productivity_score": number — 0-100 computed from the screenshot, telemetry, and task alignment.
- "is_productive": boolean.
- "confidence": number — 0-1.
- "reason": string — concise evidence-based justification.
Base every field strictly on the provided screenshot and metadata. Do NOT invent applications, window titles, or values you cannot verify.`;

  const parts: any[] = [{ text: promptText }];
  loadedItems.forEach(({ text, inlineData }) => {
    parts.push({ text });
    parts.push({ inlineData });
  });

  loadedItems.forEach(({ item }) => submittedIds.add(item.id));

  const applyResults = (parsedResults: any[]): number => {
    const patches = new Map<number, Partial<LogEntry>>();
    parsedResults.forEach((resItem: any, index: number) => {
      if (!resItem || typeof resItem.productivity_score !== 'number') return;
      const declaredIndex = typeof resItem.item_index === 'number' ? resItem.item_index - 1 : index;
      const target = loadedItems[declaredIndex] || loadedItems[index];
      if (!target) return;
      const score = resItem.productivity_score;
      const patch: Partial<LogEntry> = {
        productive_score: score,
        is_productive: typeof resItem.is_productive === 'boolean' ? resItem.is_productive : score >= 50,
      };
      if (resItem.app_name) patch.app_name = resItem.app_name;
      if (resItem.app_category) patch.app_category = resItem.app_category;
      if (resItem.window_title) patch.window_title = resItem.window_title;
      if (resItem.category) patch.ai_category = resItem.category;
      if (typeof resItem.confidence === 'number') patch.ai_confidence = resItem.confidence;
      if (resItem.reason) patch.ai_reason = resItem.reason;
      patches.set(target.item.id, patch);
    });
    if (patches.size === 0) return 0;
    // Keep patches in a session cache so later DB refreshes don't lose them
    patches.forEach((patch, id) => resultCache.set(id, patch));
    setLogs((prev) => prev.map((log) => {
      const patch = patches.get(log.id);
      return patch ? { ...log, ...patch } : log;
    }));

    // Persist to backend DB in web mode
    const w = window as any;
    if (!w.go?.main?.App) {
      patches.forEach((patch, id) => {
        if (!patch.ai_category) return;
        void apiFetch('/api/ai/log-result', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            id,
            category: patch.ai_category,
            app_name: patch.app_name || '',
            app_category: patch.app_category || '',
            window_title: patch.window_title || '',
            is_productive: !!patch.is_productive,
            productive_score: patch.productive_score ?? 0,
            ai_confidence: patch.ai_confidence ?? 0,
            ai_reason: patch.ai_reason || '',
          }),
        }).catch(() => null);
      });
    }
    return patches.size;
  };

  const bumpUsage = () => {
    bumpUsageCount(1);
  };

  const requestModel = async (modelName: string): Promise<number> => {
    const res = await fetch(`https://generativelanguage.googleapis.com/v1beta/${modelName}:generateContent?key=${apiKey}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        contents: [{ parts }],
        generationConfig: {
          responseMimeType: 'application/json',
          temperature: 0.1,
        }
      })
    });
    if (!res.ok) return 0;
    const json = await res.json();
    const parsedResults = parseAIJSONResponse(json?.candidates?.[0]?.content?.parts?.[0]?.text);
    if (!parsedResults) return 0;
    const applied = applyResults(parsedResults);
    if (applied > 0) bumpUsage();
    return applied;
  };

  let appliedCount = 0;
  try {
    appliedCount = await requestModel(model);
  } catch {
    appliedCount = 0;
  }
  if (appliedCount === 0) {
    const fallbackModel = 'models/gemini-2.0-flash';
    if (fallbackModel !== model) {
      try {
        appliedCount = await requestModel(fallbackModel);
      } catch {
        appliedCount = 0;
      }
    }
  }
  // Allow a later refresh to retry only when nothing was applied at all.
  if (appliedCount === 0) {
    loadedItems.forEach(({ item }) => submittedIds.delete(item.id));
  }
}
