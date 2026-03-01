import type { APIRoute } from 'astro';

const SITE = 'https://stacked-diffs-flow.com';

export const GET: APIRoute = () => {
  const xml = [
    '<?xml version="1.0" encoding="UTF-8"?>',
    '<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">',
    '  <sitemap>',
    `    <loc>${SITE}/sitemap-0.xml</loc>`,
    '  </sitemap>',
    '</sitemapindex>',
    '',
  ].join('\n');

  return new Response(xml, {
    headers: { 'Content-Type': 'application/xml' },
  });
};
