import { readdirSync, readFileSync, statSync } from 'node:fs';

const BLOG_DIR = new URL('../content/blog/', import.meta.url);
const DOCS_DIR = new URL('../content/docs/', import.meta.url);

/** Returns a map of blog URL paths to their most recent date (dateModified or datePublished). */
export function blogDateMap(): Map<string, Date> {
  const dates = new Map<string, Date>();
  for (const file of readdirSync(BLOG_DIR)) {
    if (!file.endsWith('.mdx') && !file.endsWith('.md')) continue;
    const text = readFileSync(new URL(file, BLOG_DIR), 'utf-8');
    const fm = text.match(/^---\n([\s\S]*?)\n---/);
    if (!fm) continue;
    const mod = fm[1].match(/dateModified:\s*(.+)/);
    const pub = fm[1].match(/datePublished:\s*(.+)/);
    const date = mod ? new Date(mod[1].trim()) : pub ? new Date(pub[1].trim()) : null;
    if (date) {
      const slug = file.replace(/\.mdx?$/, '');
      dates.set(`/blog/${slug}/`, date);
    }
  }
  return dates;
}

/** Returns a map of docs URL paths to file modified dates. */
export function docsDateMap(): Map<string, Date> {
  const dates = new Map<string, Date>();
  for (const file of readdirSync(DOCS_DIR)) {
    if (!file.endsWith('.mdx') && !file.endsWith('.md')) continue;
    const slug = file.replace(/\.mdx?$/, '');
    const stats = statSync(new URL(file, DOCS_DIR));
    dates.set(`/docs/${slug}/`, stats.mtime);
  }
  return dates;
}
