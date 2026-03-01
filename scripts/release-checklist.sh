#!/usr/bin/env bash
#
# Release checklist — validates that a release blog post exists for the given version.
#
# Usage:
#   scripts/release-checklist.sh           # validate latest git tag
#   scripts/release-checklist.sh v0.2.0    # validate a specific version
#
# Exit codes:
#   0  Check passed
#   1  Check failed
#
set -euo pipefail

BLOG_DIR="www/src/content/blog"

# ── Resolve the version to check ────────────────────────────────────
if [[ $# -ge 1 ]]; then
  VERSION="${1#v}"  # strip leading 'v' if present
else
  TAG=$(git describe --tags --abbrev=0 2>/dev/null || true)
  if [[ -z "$TAG" ]]; then
    echo "ERROR: No git tags found and no version argument provided."
    echo "Usage: $0 [version]"
    exit 1
  fi
  VERSION="${TAG#v}"
fi

# ── Skip patch releases (e.g. 1.2.3 where patch > 0) ─────────────────
PATCH=$(echo "$VERSION" | cut -d. -f3 | cut -d- -f1)
if [[ -n "$PATCH" && "$PATCH" -gt 0 ]]; then
  echo "SKIP — v${VERSION} is a patch release; blog post not required."
  exit 0
fi

echo "Checking release blog post for v${VERSION} ..."

# ── Find a release blog post matching this version ──────────────────
RELEASE_POST=$(grep -rl "version: ['\"]\\{0,1\\}${VERSION}['\"]\\{0,1\\}" "$BLOG_DIR" 2>/dev/null || true)

if [[ -z "$RELEASE_POST" ]]; then
  echo "FAIL: No blog post with 'version: \"${VERSION}\"' found in $BLOG_DIR"
  echo ""
  echo "Create a release blog post with the following frontmatter:"
  echo ""
  echo "  ---"
  echo "  title: \"SDF v${VERSION}\""
  echo "  description: \"...\""
  echo "  datePublished: $(date +%Y-%m-%d)"
  echo "  tags: [release]"
  echo "  version: \"${VERSION}\""
  echo "  ---"
  exit 1
fi

# ── Validate the post has the required fields ───────────────────────
MISSING=""
for field in title description datePublished; do
  if ! grep -q "^${field}:" "$RELEASE_POST" 2>/dev/null; then
    MISSING="${MISSING} ${field}"
  fi
done

if ! grep -q "tags:.*release" "$RELEASE_POST" 2>/dev/null; then
  MISSING="${MISSING} tags(release)"
fi

if [[ -n "$MISSING" ]]; then
  echo "FAIL: Release post $(basename "$RELEASE_POST") is missing:${MISSING}"
  exit 1
fi

echo "OK — $(basename "$RELEASE_POST")"
