import { readdirSync, readFileSync, writeFileSync } from 'node:fs';

const DOCS_DIR = new URL('../src/content/docs/', import.meta.url);
const OUTPUT_FILE = new URL('../public/llms-full.txt', import.meta.url);
const SITE = 'https://stacked-diffs-flow.com';

function parseFrontmatter(text) {
  const match = text.match(/^---\n([\s\S]*?)\n---\n?/);
  if (!match) return { data: {}, body: text.trim() };
  const data = {};
  const frontmatter = match[1];

  const title = frontmatter.match(/^title:\s*(.+)$/m);
  const description = frontmatter.match(/^description:\s*(.+)$/m);
  const order = frontmatter.match(/^order:\s*(.+)$/m);

  if (title) data.title = title[1].trim().replace(/^['"]|['"]$/g, '');
  if (description) data.description = description[1].trim().replace(/^['"]|['"]$/g, '');
  if (order) data.order = Number(order[1].trim());

  return { data, body: text.slice(match[0].length).trim() };
}

function loadDocs() {
  const docs = [];
  for (const file of readdirSync(DOCS_DIR)) {
    if (!file.endsWith('.md') && !file.endsWith('.mdx')) continue;
    const slug = file.replace(/\.mdx?$/, '');
    const raw = readFileSync(new URL(file, DOCS_DIR), 'utf8');
    const { data, body } = parseFrontmatter(raw);
    docs.push({
      slug,
      title: data.title ?? slug,
      description: data.description ?? '',
      order: Number.isFinite(data.order) ? data.order : 99,
      body,
    });
  }
  return docs.sort((a, b) => a.order - b.order || a.title.localeCompare(b.title));
}

function render(docs) {
  const lines = [
    '# SDF Documentation (Full)',
    '',
    '> Consolidated documentation context for AI agents and search systems.',
    '> Source: https://stacked-diffs-flow.com/docs/',
    '',
    'This file is generated from all `/src/content/docs/*` pages in order.',
    '',
  ];

  for (const doc of docs) {
    lines.push(`## ${doc.title}`);
    lines.push('');
    lines.push(`Source: ${SITE}/docs/${doc.slug}/`);
    if (doc.description) {
      lines.push('');
      lines.push(doc.description);
    }
    lines.push('');
    lines.push(doc.body);
    lines.push('');
    lines.push('---');
    lines.push('');
  }

  return lines.join('\n').trim() + '\n';
}

const docs = loadDocs();
const output = render(docs);
writeFileSync(OUTPUT_FILE, output, 'utf8');
console.log(`Wrote ${docs.length} docs to ${OUTPUT_FILE.pathname}`);
