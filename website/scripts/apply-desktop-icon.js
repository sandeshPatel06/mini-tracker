import sharp from 'sharp';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const rootDir = path.resolve(__dirname, '../..');
const websitePublicDir = path.resolve(__dirname, '../public');
const appIconSource = path.join(rootDir, 'build/appicon.png');

async function main() {
  if (!fs.existsSync(appIconSource)) {
    console.error('App icon source not found at:', appIconSource);
    process.exit(1);
  }

  // Copy original high-res app icon
  const destLogo = path.join(websitePublicDir, 'app-logo.png');
  fs.copyFileSync(appIconSource, destLogo);
  console.log('Copied build/appicon.png to website/public/app-logo.png');

  // Generate 32x32 favicon
  await sharp(appIconSource)
    .resize(32, 32)
    .png()
    .toFile(path.join(websitePublicDir, 'favicon.png'));

  // Generate 192x192 icon
  await sharp(appIconSource)
    .resize(192, 192)
    .png()
    .toFile(path.join(websitePublicDir, 'icon-192.png'));

  // Generate 512x512 icon
  await sharp(appIconSource)
    .resize(512, 512)
    .png()
    .toFile(path.join(websitePublicDir, 'icon-512.png'));

  // Generate apple-touch-icon
  await sharp(appIconSource)
    .resize(180, 180)
    .png()
    .toFile(path.join(websitePublicDir, 'apple-touch-icon.png'));

  // Also compose the real app icon into og-default.png!
  const ogBackground = `
  <svg width="1200" height="630" viewBox="0 0 1200 630" xmlns="http://www.w3.org/2000/svg">
    <defs>
      <radialGradient id="bgGlow" cx="20%" cy="25%" r="65%">
        <stop offset="0%" stop-color="#10b981" stop-opacity="0.22" />
        <stop offset="60%" stop-color="#080e11" stop-opacity="0.95" />
        <stop offset="100%" stop-color="#040608" stop-opacity="1" />
      </radialGradient>
      <linearGradient id="emeraldGrad" x1="0%" y1="0%" x2="100%" y2="100%">
        <stop offset="0%" stop-color="#34d399" />
        <stop offset="100%" stop-color="#059669" />
      </linearGradient>
      <linearGradient id="cardGrad" x1="0%" y1="0%" x2="100%" y2="100%">
        <stop offset="0%" stop-color="#13222a" stop-opacity="0.85" />
        <stop offset="100%" stop-color="#0d171c" stop-opacity="0.92" />
      </linearGradient>
    </defs>

    <rect width="1200" height="630" fill="#080e11" />
    <rect width="1200" height="630" fill="url(#bgGlow)" />

    <!-- Grid lines -->
    <g opacity="0.08" stroke="#10b981" stroke-width="1">
      <line x1="0" y1="105" x2="1200" y2="105" />
      <line x1="0" y1="210" x2="1200" y2="210" />
      <line x1="0" y1="315" x2="1200" y2="315" />
      <line x1="0" y1="420" x2="1200" y2="420" />
      <line x1="0" y1="525" x2="1200" y2="525" />
      <line x1="200" y1="0" x2="200" y2="630" />
      <line x1="400" y1="0" x2="400" y2="630" />
      <line x1="600" y1="0" x2="600" y2="630" />
      <line x1="800" y1="0" x2="800" y2="630" />
      <line x1="1000" y1="0" x2="1000" y2="630" />
    </g>

    <!-- Right Side Dashboard Card -->
    <g transform="translate(730, 140)">
      <rect width="400" height="360" rx="18" fill="url(#cardGrad)" stroke="#10b981" stroke-width="1.5" stroke-opacity="0.35" />
      <circle cx="28" cy="28" r="6" fill="#ef4444" opacity="0.75" />
      <circle cx="48" cy="28" r="6" fill="#f59e0b" opacity="0.75" />
      <circle cx="68" cy="28" r="6" fill="#10b981" opacity="0.75" />
      <line x1="0" y1="52" x2="400" y2="52" stroke="#10b981" stroke-opacity="0.15" stroke-width="1" />

      <text x="28" y="92" font-family="monospace" font-size="14" fill="#6ee7b7">$ get-hike status</text>
      <text x="28" y="128" font-family="monospace" font-size="13" fill="#cbd5e1">Active Session: 02h 44m (PID 14280)</text>
      <text x="28" y="156" font-family="monospace" font-size="13" fill="#cbd5e1">Focus Entropy: 8.7 / 10 (Deep Focus)</text>
      <text x="28" y="184" font-family="monospace" font-size="13" fill="#cbd5e1">AI Model: gemini-3.5-flash-lite (94%)</text>
      <text x="28" y="212" font-family="monospace" font-size="13" fill="#cbd5e1">SQLite DB: ~/.local/share/get-hike/</text>
      <text x="28" y="250" font-family="monospace" font-size="13" fill="#34d399">✓ Zero character keylogging</text>
      <text x="28" y="278" font-family="monospace" font-size="13" fill="#34d399">✓ Local storage &amp; auto-pruning</text>
      <text x="28" y="318" font-family="monospace" font-size="12" fill="#64748b">Linux • Windows • macOS • MIT</text>
    </g>

    <!-- Top badge -->
    <g transform="translate(90, 105)">
      <rect width="280" height="34" rx="17" fill="#10b981" fill-opacity="0.12" stroke="#10b981" stroke-opacity="0.35" />
      <circle cx="20" cy="17" r="4.5" fill="#10b981" />
      <text x="34" y="22" font-family="sans-serif" font-weight="600" font-size="13" fill="#34d399" letter-spacing="0.5">OPEN SOURCE PRODUCTIVITY</text>
    </g>

    <!-- Title and Subtitle text (Logo will be composited via sharp) -->
    <g transform="translate(90, 290)">
      <text x="0" y="0" font-family="system-ui, -apple-system, sans-serif" font-weight="800" font-size="52" fill="#ffffff" letter-spacing="-1">
        get-Hike
      </text>
      <text x="0" y="58" font-family="system-ui, -apple-system, sans-serif" font-weight="700" font-size="38" fill="url(#emeraldGrad)" letter-spacing="-0.5">
        Own your focus. Keep your data.
      </text>
      <text x="0" y="115" font-family="system-ui, -apple-system, sans-serif" font-size="19" fill="#94a3b8">
        Privacy-first tracker with screenshots, entropy telemetry,
      </text>
      <text x="0" y="145" font-family="system-ui, -apple-system, sans-serif" font-size="19" fill="#94a3b8">
        and optional Gemini AI batch analysis. 100% Free &amp; MIT.
      </text>
      <text x="0" y="220" font-family="system-ui, -apple-system, sans-serif" font-size="14" font-weight="500" fill="#64748b">
        github.com/sandeshPatel06/mini-tracker • © REAK INFOTECH LLP
      </text>
    </g>
  </svg>
  `;

  // Resize real desktop app icon to 90x90 for OG overlay
  const resizedIconBuffer = await sharp(appIconSource)
    .resize(96, 96)
    .png()
    .toBuffer();

  const ogBase = await sharp(Buffer.from(ogBackground))
    .composite([
      {
        input: resizedIconBuffer,
        top: 170,
        left: 90,
      },
    ])
    .png({ quality: 95 })
    .toFile(path.join(websitePublicDir, 'og-default.png'));

  console.log('Real desktop app icon processed and OG image updated!');
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
