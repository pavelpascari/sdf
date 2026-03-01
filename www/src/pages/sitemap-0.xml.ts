import type { APIRoute } from 'astro';
import { getCollection } from 'astro:content';

const SITE = 'https://stacked-diffs-flow.com';

function toW3CDate(date: Date): string {
  return date.toISOString().split('T')[0];
}

function urlEntry(loc: string, lastmod?: Date): string {
  const lines = ['  <url>', `    <loc>${loc}</loc>`];
  if (lastmod) {
    lines.push(`    <lastmod>${toW3CDate(lastmod)}</lastmod>`);
  }
  lines.push('  </url>');
  return lines.join('\n');
}

export const GET: APIRoute = async () => {
  const blogPosts = await getCollection('blog', ({ data }) => data.published);
  const docs = await getCollection('docs');

  // Build a map of blog slug -> most recent date (dateModified or datePublished).
  const blogDates = new Map<string, Date>();
  for (const post of blogPosts) {
    blogDates.set(post.slug, post.data.dateModified ?? post.data.datePublished);
  }

  // Collect tag pages from published posts.
  const tags = [...new Set(blogPosts.flatMap((p) => p.data.tags))];

  const entries: string[] = [];

  // Homepage
  entries.push(urlEntry(`${SITE}/`));

  // Blog index -- use the most recent post date as lastmod.
  const latestBlogDate = blogPosts
    .map((p) => p.data.dateModified ?? p.data.datePublished)
    .sort((a, b) => b.getTime() - a.getTime())[0];
  entries.push(urlEntry(`${SITE}/blog/`, latestBlogDate));

  // Individual blog posts
  for (const post of blogPosts.sort((a, b) =>
    a.slug.localeCompare(b.slug),
  )) {
    entries.push(
      urlEntry(`${SITE}/blog/${post.slug}/`, blogDates.get(post.slug)),
    );
  }

  // Blog tag pages
  for (const tag of tags.sort()) {
    entries.push(urlEntry(`${SITE}/blog/tag/${tag}/`));
  }

  // Docs index
  entries.push(urlEntry(`${SITE}/docs/`));

  // Individual doc pages
  for (const doc of docs.sort((a, b) => a.slug.localeCompare(b.slug))) {
    entries.push(urlEntry(`${SITE}/docs/${doc.slug}/`));
  }

  const xml = [
    '<?xml version="1.0" encoding="UTF-8"?>',
    '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">',
    ...entries,
    '</urlset>',
    '',
  ].join('\n');

  return new Response(xml, {
    headers: { 'Content-Type': 'application/xml' },
  });
};
