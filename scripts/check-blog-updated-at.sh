#!/usr/bin/env bash
# Validates that modified blog posts have a `dateModified` frontmatter field.
#
# Usage:
#   scripts/check-blog-updated-at.sh [base-ref]
#
# base-ref defaults to origin/main. New (added) files are skipped — only
# modifications require dateModified.

set -euo pipefail

BASE="${1:-origin/main}"
BLOG_DIR="www/src/content/blog"
errors=0

# Get blog files that were Modified (not Added) relative to base
changed_files=$(git diff --name-only --diff-filter=M "$BASE" -- "$BLOG_DIR" 2>/dev/null || true)

if [ -z "$changed_files" ]; then
  echo "No modified blog posts — nothing to check."
  exit 0
fi

for file in $changed_files; do
  if [ ! -f "$file" ]; then
    continue
  fi

  # Extract frontmatter (between first two --- lines)
  frontmatter=$(sed -n '/^---$/,/^---$/p' "$file" | sed '1d;$d')

  if ! echo "$frontmatter" | grep -qE '^dateModified:'; then
    echo "ERROR: $file was modified but is missing 'dateModified' in frontmatter."
    errors=$((errors + 1))
    continue
  fi

  # Verify dateModified is not empty
  value=$(echo "$frontmatter" | grep -E '^dateModified:' | sed 's/^dateModified:\s*//')
  if [ -z "$value" ]; then
    echo "ERROR: $file has an empty 'dateModified' field."
    errors=$((errors + 1))
  fi
done

if [ "$errors" -gt 0 ]; then
  echo ""
  echo "$errors blog post(s) modified without setting 'dateModified'."
  echo "Add 'dateModified: YYYY-MM-DD' to the frontmatter of each modified post."
  exit 1
fi

echo "All modified blog posts have dateModified set."
