import type { APIRoute } from 'astro';
import { getCollection } from 'astro:content';

export const GET: APIRoute = async () => {
  const posts = await getCollection('blog', (entry) => {
    return entry.data.published && entry.data.tags.includes('release') && !!entry.data.version;
  });

  // Sort by datePublished descending to find the latest release.
  posts.sort((a, b) => b.data.datePublished.getTime() - a.data.datePublished.getTime());

  const latest = posts[0];
  if (!latest) {
    return new Response(JSON.stringify({ error: 'no releases found' }), {
      status: 404,
      headers: { 'Content-Type': 'application/json' },
    });
  }

  const body = {
    version: latest.data.version,
    changelog: `https://sdf-tool.com/blog/${latest.slug}`,
  };

  return new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
  });
};
