import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const distDir = path.resolve(__dirname, '../dist');

const htmlFiles = [
  'index.html',
  'download/index.html',
  'features/index.html',
  'get-started/index.html',
  'privacy/index.html',
  'changelog/index.html',
  '404.html',
];

let totalErrors = 0;

console.log('🔍 Starting Comprehensive SEO & Metadata Audit...\n');

for (const file of htmlFiles) {
  const filePath = path.join(distDir, file);
  if (!fs.existsSync(filePath)) {
    console.error(`❌ File missing: ${file}`);
    totalErrors++;
    continue;
  }

  const html = fs.readFileSync(filePath, 'utf-8');
  console.log(`📄 Checking: /${file}`);

  // 1. Title tag
  const titleMatch = html.match(/<title>([^<]+)<\/title>/i);
  if (!titleMatch) {
    console.error(`  ❌ Missing <title> tag`);
    totalErrors++;
  } else {
    const title = titleMatch[1];
    if (title.length > 70) {
      console.warn(`  ⚠️ Title slightly long (${title.length} chars): "${title}"`);
    } else {
      console.log(`  ✓ Title (${title.length} chars): "${title}"`);
    }
  }

  // 2. Meta description
  const descMatch = html.match(/<meta[^>]*name=["']description["'][^>]*content=["']([^"']+)["']/i);
  if (!descMatch) {
    console.error(`  ❌ Missing meta description`);
    totalErrors++;
  } else {
    const desc = descMatch[1];
    if (desc.length > 165) {
      console.warn(`  ⚠️ Description slightly long (${desc.length} chars)`);
    } else {
      console.log(`  ✓ Description (${desc.length} chars): "${desc.substring(0, 50)}..."`);
    }
  }

  // 3. Viewport tag
  if (!html.includes('name="viewport"')) {
    console.error(`  ❌ Missing viewport tag`);
    totalErrors++;
  } else {
    console.log(`  ✓ Viewport meta configured`);
  }

  // 4. Canonical URL
  if (!html.includes('rel="canonical"')) {
    console.error(`  ❌ Missing canonical tag`);
    totalErrors++;
  } else {
    console.log(`  ✓ Canonical link present`);
  }

  // 5. OpenGraph Tags
  const ogTags = ['og:title', 'og:description', 'og:image', 'og:url', 'og:type'];
  for (const tag of ogTags) {
    if (!html.includes(`property="${tag}"`)) {
      console.error(`  ❌ Missing ${tag}`);
      totalErrors++;
    }
  }
  console.log(`  ✓ Open Graph tags verified`);

  // 6. Twitter Tags
  const twTags = ['twitter:card', 'twitter:title', 'twitter:description', 'twitter:image'];
  for (const tag of twTags) {
    if (!html.includes(`name="${tag}"`)) {
      console.error(`  ❌ Missing ${tag}`);
      totalErrors++;
    }
  }
  console.log(`  ✓ Twitter Card tags verified`);

  // 7. Structured Data (JSON-LD)
  const jsonLdMatch = html.match(/<script type="application\/ld\+json">([\s\S]*?)<\/script>/i);
  if (jsonLdMatch) {
    try {
      JSON.parse(jsonLdMatch[1]);
      console.log(`  ✓ Valid JSON-LD structured data detected`);
    } catch (e) {
      console.error(`  ❌ Invalid JSON in JSON-LD script:`, e.message);
      totalErrors++;
    }
  }

  console.log('');
}

if (totalErrors === 0) {
  console.log('🎉 Audit PASSED: 100% of SEO, Open Graph, Twitter cards, and structured data checks succeeded!\n');
} else {
  console.error(`❌ Audit finished with ${totalErrors} issue(s).\n`);
  process.exit(1);
}
