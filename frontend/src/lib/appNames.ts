/**
 * Canonical application identities.
 *
 * The tracker stores the app name from the X11 WM_CLASS ("google-chrome",
 * "code", "Gnome-terminal") and the Gemini analysis overwrites it with a
 * human readable name ("Google Chrome", "Visual Studio Code", "Terminal").
 * Both spellings describe the same application, so they are folded into one
 * canonical, lower-cased identity before they are counted, filtered or shown.
 */

const ALIASES: Record<string, string> = {
  'gnome terminal': 'terminal',
  'gnome terminal server': 'terminal',
  'xfce4 terminal': 'terminal',
  code: 'visual studio code',
  'code oss': 'visual studio code',
  vscodium: 'visual studio code',
  'get hike': 'get-hike',
  gethike: 'get-hike',
};

const MAX_NAME_LENGTH = 48;

/**
 * Returns the canonical identity of an application name.
 *
 * The value is trimmed, cut at a window-title separator, lower-cased and its
 * separators are collapsed into single spaces, so "Google Chrome",
 * "google-chrome" and "Google_Chrome" all resolve to the same identity.
 * An empty string is returned when the value does not look like an app name.
 */
export function canonicalAppName(name?: string | null): string {
  let raw = (name || '').trim();
  if (!raw) return '';

  // The AI frequently answers with the window title ("OC | Import session
  // from ..."). Keep the leading identity and drop the title part.
  const pipe = raw.indexOf(' | ');
  if (pipe > 0) raw = raw.slice(0, pipe).trim();

  if (!raw || raw.length > MAX_NAME_LENGTH) return '';
  if (/[\\/]/.test(raw)) return '';
  if (raw.includes(' · ')) return '';
  if (raw.split(/\s+/).length > 4) return '';

  const value = raw
    .toLowerCase()
    .replace(/[-_+,]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
  if (!value) return '';

  return ALIASES[value] || value;
}

/** Canonical app name with a fallback for entries without an application. */
export function displayAppName(name?: string | null, fallback = 'Unknown'): string {
  return canonicalAppName(name) || fallback;
}
