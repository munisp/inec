#!/usr/bin/env bash
# check_image_pins.sh — R4-39d supply-chain gate.
# Fails (exit 1) if any container image reference in compose files or
# Dockerfiles is unpinned: no tag at all, or the mutable `:latest` tag.
# Digest pinning (@sha256:) is the gold standard; a fixed version tag is the
# minimum enforced here.
set -uo pipefail

cd "$(dirname "$0")/.."
fail=0

check_ref() { # $1=file $2=lineno $3=ref
  local file="$1" line="$2" ref="$3"
  # strip variable interpolation suffixes like ${VAR:-...} inside the ref
  case "$ref" in
    *'${'*'}'*) ;; # env-templated refs are enforced by :? guards at deploy
  esac
  if [[ "$ref" == *@sha256:* ]]; then
    return 0 # digest-pinned: best practice
  fi
  if [[ "$ref" != *:* ]]; then
    echo "UNPINNED (no tag): $file:$line -> $ref"
    fail=1
  elif [[ "$ref" == *:latest ]]; then
    echo "MUTABLE TAG (:latest): $file:$line -> $ref"
    fail=1
  fi
}

# 1. compose files: `image:` lines
while IFS= read -r -d '' f; do
  while IFS=: read -r lineno rest; do
    ref=$(echo "$rest" | sed -e 's/^[[:space:]]*image:[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/^"//' -e 's/"$//')
    # skip refs that are purely env-templated (enforced separately via :?)
    case "$ref" in
      \$\{*\}) continue ;;
    esac
    check_ref "$f" "$lineno" "$ref"
  done < <(grep -nE '^[[:space:]]*image:' "$f" | grep -v '^\s*#')
done < <(find . -name .git -prune -o -type f \( -name 'docker-compose*.yml' -o -name 'docker-compose*.yaml' -o -name 'compose*.yml' \) -print0)

# 2. Dockerfiles: FROM lines (build-stage aliases like `FROM x AS builder` are
#    still checked on their image part; `FROM scratch` and stage references are skipped)
while IFS= read -r -d '' f; do
  # collect stage aliases defined in this file
  mapfile -t aliases < <(grep -ioE '^FROM[[:space:]]+[^[:space:]]+([[:space:]]+[Aa][Ss][[:space:]]+[A-Za-z0-9_.-]+)' "$f" | awk '{print tolower($NF)}')
  while IFS=: read -r lineno rest; do
    ref=$(echo "$rest" | sed -e 's/^FROM[[:space:]]*//' -e 's/[[:space:]].*$//')
    case "$ref" in
      scratch|''|'$'{*}*) continue ;;
    esac
    # skip references to earlier build stages
    skip=0
    for a in ${aliases[@]+"${aliases[@]}"}; do
      [ "${ref,,}" = "$a" ] && skip=1 && break
    done
    [ "$skip" -eq 1 ] && continue
    check_ref "$f" "$lineno" "$ref"
  done < <(grep -nE '^FROM ' "$f")
done < <(find . -name .git -prune -o -type f -iname 'Dockerfile*' -print0)

if [ "$fail" -ne 0 ]; then
  echo ""
  echo "FAIL: unpinned or mutable image references found (R4-39d)."
  exit 1
fi
echo "OK: all image references carry a fixed tag or digest."
exit 0
