# SEO Operations Checklist

This checklist covers SEO tasks that cannot be completed by code changes alone.

## 1. Register property in Google Search Console

1. Open Google Search Console and add a new property for `https://stacked-diffs-flow.com/`.
2. Verify ownership with your preferred method (DNS TXT record recommended).
3. Confirm verification succeeds before continuing.

## 2. Submit sitemap

1. In Search Console, open `Indexing -> Sitemaps`.
2. Submit: `https://stacked-diffs-flow.com/sitemap-index.xml`.
3. Confirm sitemap status is `Success`.
4. Re-check after deployment of major docs/blog updates.

## 3. Validate mobile usability

1. Run Google Mobile-Friendly Test on:
   - homepage (`/`)
   - docs index (`/docs/`)
   - a representative long article (`/blog/testing-cli-tools-that-shell-out/`)
2. Confirm no tap-target, viewport, or text-size issues.
3. If issues appear, fix in CSS/layout and re-test.

## 4. Monitor indexing and coverage

1. Review Search Console `Pages` report weekly.
2. Investigate `Crawled - currently not indexed` and `Discovered - currently not indexed`.
3. Use URL Inspection for key pages:
   - `/docs/getting-started/`
   - `/docs/what-are-stacked-diffs/`
   - `/blog/v0-3-0-machine-friendly/`

## 5. Monitor performance

1. Review Search Console `Performance -> Search results`.
2. Track impressions/clicks for:
   - `stacked diffs`
   - `stacked pull requests`
   - `what are stacked diffs`
3. Expand content if impressions rise but CTR remains low.
