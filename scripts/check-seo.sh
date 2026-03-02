#!/usr/bin/env bash
# check-seo.sh -- Validate SEO and AI-discoverability requirements for stacked-diffs-flow.com.
#
# Expects a built site in www/dist (run `npm run build` in www/ first).
# Returns non-zero if any required check fails.

set -euo pipefail

DIST="www/dist"
ERRORS=0

fail() {
  echo "FAIL: $1"
  ERRORS=$((ERRORS + 1))
}

pass() {
  echo "  ok: $1"
}

# ── Prerequisites ────────────────────────────────────────────────
if [ ! -d "$DIST" ]; then
  echo "ERROR: $DIST not found. Run 'npm run build' in www/ first."
  exit 1
fi

echo "=== SEO & AI-discoverability checks ==="
echo ""

# ── 1. robots.txt ────────────────────────────────────────────────
echo "-- robots.txt"
if [ -f "$DIST/robots.txt" ]; then
  if grep -q "^Sitemap:" "$DIST/robots.txt"; then
    pass "robots.txt exists and references a sitemap"
  else
    fail "robots.txt exists but has no Sitemap directive"
  fi
  if grep -qi "Disallow: /" "$DIST/robots.txt" && ! grep -qi "Disallow: /$" "$DIST/robots.txt"; then
    fail "robots.txt blocks indexing (broad Disallow: /)"
  fi
else
  fail "robots.txt is missing"
fi

# ── 2. sitemap ───────────────────────────────────────────────────
echo "-- sitemap"
SITEMAP_INDEX="$DIST/sitemap-index.xml"
if [ -f "$SITEMAP_INDEX" ]; then
  pass "sitemap-index.xml exists"
  # Check that at least one sitemap is referenced
  if grep -q "<loc>" "$SITEMAP_INDEX"; then
    pass "sitemap-index.xml contains at least one sitemap"
  else
    fail "sitemap-index.xml is empty (no <loc> entries)"
  fi
else
  fail "sitemap-index.xml is missing"
fi

# ── 3. llms.txt ──────────────────────────────────────────────────
echo "-- llms.txt"
if [ -f "$DIST/llms.txt" ]; then
  LLMS_SIZE=$(wc -c < "$DIST/llms.txt" | tr -d ' ')
  if [ "$LLMS_SIZE" -gt 100 ]; then
    pass "llms.txt exists (${LLMS_SIZE} bytes)"
  else
    fail "llms.txt is too small (${LLMS_SIZE} bytes)"
  fi

  # Check that key doc pages are listed
  REQUIRED_DOCS=("getting-started" "installation" "commands" "core-concepts")
  for doc in "${REQUIRED_DOCS[@]}"; do
    if grep -q "$doc" "$DIST/llms.txt"; then
      pass "llms.txt references $doc"
    else
      fail "llms.txt is missing reference to $doc"
    fi
  done
else
  fail "llms.txt is missing"
fi

# ── 4. HTML page checks ─────────────────────────────────────────
echo "-- HTML pages"
HTML_FILES=$(find "$DIST" -name '*.html' -type f)
PAGE_COUNT=0

for page in $HTML_FILES; do
  PAGE_COUNT=$((PAGE_COUNT + 1))
  REL_PATH="${page#$DIST}"

  # <title> tag must exist and not be empty
  if ! grep -q '<title>' "$page"; then
    fail "$REL_PATH: missing <title> tag"
  elif grep -q '<title></title>' "$page"; then
    fail "$REL_PATH: empty <title> tag"
  fi

  # <meta name="description"> must exist
  if ! grep -qi 'name="description"' "$page"; then
    fail "$REL_PATH: missing meta description"
  fi

  # Canonical URL must exist
  if ! grep -qi 'rel="canonical"' "$page"; then
    fail "$REL_PATH: missing canonical URL"
  fi

  # No more than one <h1> per page
  H1_COUNT=$(grep -oc '<h1' "$page" || true)
  if [ "$H1_COUNT" -gt 1 ]; then
    fail "$REL_PATH: multiple h1 tags ($H1_COUNT found)"
  fi
done

if [ "$PAGE_COUNT" -eq 0 ]; then
  fail "No HTML files found in $DIST"
else
  pass "$PAGE_COUNT HTML pages checked"
fi

# ── 5. JSON-LD checks ───────────────────────────────────────────
echo "-- JSON-LD structured data"
HOMEPAGE="$DIST/index.html"
if [ -f "$HOMEPAGE" ]; then
  if grep -q 'application/ld+json' "$HOMEPAGE"; then
    pass "Homepage has JSON-LD structured data"
  else
    fail "Homepage is missing JSON-LD structured data"
  fi

  if grep -q '"SoftwareApplication"' "$HOMEPAGE"; then
    pass "Homepage has SoftwareApplication schema"
  else
    fail "Homepage is missing SoftwareApplication schema"
  fi
fi

# Check docs pages have JSON-LD
DOCS_DIR="$DIST/docs"
if [ -d "$DOCS_DIR" ]; then
  DOCS_WITH_JSONLD=0
  DOCS_TOTAL=0
  for doc_page in $(find "$DOCS_DIR" -name 'index.html' -type f -not -path "$DOCS_DIR/index.html"); do
    DOCS_TOTAL=$((DOCS_TOTAL + 1))
    if grep -q 'application/ld+json' "$doc_page"; then
      DOCS_WITH_JSONLD=$((DOCS_WITH_JSONLD + 1))
    else
      DOC_REL="${doc_page#$DIST}"
      fail "$DOC_REL: missing JSON-LD structured data"
    fi
  done
  if [ "$DOCS_TOTAL" -gt 0 ] && [ "$DOCS_WITH_JSONLD" -eq "$DOCS_TOTAL" ]; then
    pass "All $DOCS_TOTAL docs pages have JSON-LD"
  fi
fi

# ── 6. Open Graph checks ────────────────────────────────────────
echo "-- Open Graph metadata"
for page in $HTML_FILES; do
  REL_PATH="${page#$DIST}"
  if ! grep -qi 'og:title' "$page"; then
    fail "$REL_PATH: missing og:title"
  fi
  if ! grep -qi 'og:description' "$page"; then
    fail "$REL_PATH: missing og:description"
  fi
done
pass "Open Graph tags checked"

# ── Summary ──────────────────────────────────────────────────────
echo ""
if [ "$ERRORS" -eq 0 ]; then
  echo "All SEO checks passed."
  exit 0
else
  echo "FAILED: $ERRORS error(s) found."
  exit 1
fi
