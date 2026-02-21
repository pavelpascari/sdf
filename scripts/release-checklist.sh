#!/usr/bin/env bash
#
# Release checklist — enforces that every release has accompanying blog content.
#
# Usage:
#   scripts/release-checklist.sh           # validate latest git tag
#   scripts/release-checklist.sh v0.2.0    # validate a specific version
#
# Exit codes:
#   0  All checks passed
#   1  One or more checks failed
#
set -euo pipefail

BLOG_DIR="www/src/content/blog"

# ── Resolve the version to check ────────────────────────────────────
if [[ $# -ge 1 ]]; then
  VERSION="${1#v}"  # strip leading 'v' if present
else
  # Use the latest semver tag from git
  TAG=$(git describe --tags --abbrev=0 2>/dev/null || true)
  if [[ -z "$TAG" ]]; then
    echo "ERROR: No git tags found and no version argument provided."
    echo "Usage: $0 [version]"
    exit 1
  fi
  VERSION="${TAG#v}"
fi

echo "Release checklist for v${VERSION}"
echo "================================="
echo ""

FAILED=0

# ── Check 1: Blog directory exists ──────────────────────────────────
printf "  [check] Blog content directory exists ... "
if [[ -d "$BLOG_DIR" ]]; then
  echo "OK"
else
  echo "FAIL"
  echo "         Missing directory: $BLOG_DIR"
  FAILED=1
fi

# ── Check 2: At least one blog post exists ──────────────────────────
printf "  [check] Blog has at least one post ... "
POST_COUNT=$(find "$BLOG_DIR" -name '*.mdx' -o -name '*.md' 2>/dev/null | wc -l | tr -d ' ')
if [[ "$POST_COUNT" -gt 0 ]]; then
  echo "OK ($POST_COUNT post(s))"
else
  echo "FAIL"
  echo "         No .mdx or .md files found in $BLOG_DIR"
  FAILED=1
fi

# ── Check 3: A release blog post exists for this version ────────────
printf "  [check] Release blog post for v${VERSION} ... "
RELEASE_POST=$(grep -rl "version: ['\"]\\{0,1\\}${VERSION}['\"]\\{0,1\\}" "$BLOG_DIR" 2>/dev/null || true)
if [[ -n "$RELEASE_POST" ]]; then
  echo "OK"
  echo "         Found: $RELEASE_POST"
else
  echo "FAIL"
  echo "         No blog post with 'version: \"${VERSION}\"' found in $BLOG_DIR"
  echo "         Create a blog post with the following frontmatter:"
  echo ""
  echo "           ---"
  echo "           title: \"SDF v${VERSION}\""
  echo "           description: \"...\""
  echo "           date: $(date +%Y-%m-%d)"
  echo "           tags: [release]"
  echo "           version: \"${VERSION}\""
  echo "           ---"
  echo ""
  FAILED=1
fi

# ── Check 4: Release post has the 'release' tag ─────────────────────
if [[ -n "$RELEASE_POST" ]]; then
  printf "  [check] Release post has 'release' tag ... "
  if grep -q "tags:.*release" "$RELEASE_POST" 2>/dev/null; then
    echo "OK"
  else
    echo "FAIL"
    echo "         The blog post for v${VERSION} must include 'release' in its tags."
    FAILED=1
  fi
fi

# ── Check 5: Release post has required frontmatter fields ───────────
if [[ -n "$RELEASE_POST" ]]; then
  printf "  [check] Release post has required frontmatter ... "
  MISSING_FIELDS=""
  for field in title description date; do
    if ! grep -q "^${field}:" "$RELEASE_POST" 2>/dev/null; then
      MISSING_FIELDS="${MISSING_FIELDS} ${field}"
    fi
  done
  if [[ -z "$MISSING_FIELDS" ]]; then
    echo "OK"
  else
    echo "FAIL"
    echo "         Missing frontmatter fields:${MISSING_FIELDS}"
    FAILED=1
  fi
fi

# ── Summary ─────────────────────────────────────────────────────────
echo ""
if [[ "$FAILED" -eq 0 ]]; then
  echo "All release checklist items passed."
  exit 0
else
  echo "RELEASE BLOCKED: One or more checklist items failed."
  echo "Fix the issues above before releasing v${VERSION}."
  exit 1
fi
