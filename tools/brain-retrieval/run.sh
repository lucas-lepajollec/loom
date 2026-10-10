#!/bin/sh
# Repository-root entry point for the audit's two offline go test commands.
set -eu
RETRIEVAL_ROOT=$(pwd)
RETRIEVAL_TMP=$(mktemp -d)
trap 'rm -rf "$RETRIEVAL_TMP"' EXIT HUP INT TERM
RETRIEVAL_OUT=${LOOM_BRAIN_RETRIEVAL_REPORT_DIR:-"$RETRIEVAL_ROOT/.project-local/brain-retrieval/results"}
mkdir -p "$RETRIEVAL_TMP/home" "$RETRIEVAL_TMP/data" "$RETRIEVAL_OUT"
RETRIEVAL_GOCACHE=$(go env GOCACHE)
RETRIEVAL_MODCACHE=$(go env GOMODCACHE)
for RETRIEVAL_STAGE in evidence final-context; do
  if [ "$RETRIEVAL_STAGE" = evidence ]; then
    RETRIEVAL_PACKAGE=./internal/loom/brain
    RETRIEVAL_TEST='^TestRetrievalEvidence$'
  else
    RETRIEVAL_PACKAGE=./internal/loom
    RETRIEVAL_TEST='^TestBrainRetrievalFinalContext$'
  fi
  env HOME="$RETRIEVAL_TMP/home" USERPROFILE="$RETRIEVAL_TMP/home" \
    XDG_CONFIG_HOME="$RETRIEVAL_TMP/home/.config" XDG_DATA_HOME="$RETRIEVAL_TMP/home/.local/share" \
    CODEX_HOME="$RETRIEVAL_TMP/home/.codex" PI_CODING_AGENT_DIR="$RETRIEVAL_TMP/home/.pi" \
    HERMES_HOME="$RETRIEVAL_TMP/home/.hermes" DSH_HOME="$RETRIEVAL_TMP/home/.dsh" \
    LOOM_HOME="$RETRIEVAL_TMP/data" GOCACHE="$RETRIEVAL_GOCACHE" GOMODCACHE="$RETRIEVAL_MODCACHE" \
    GOTOOLCHAIN=go1.26.9 LOOM_BRAIN_RETRIEVAL_MODE=lexical \
    LOOM_BRAIN_RETRIEVAL_OUT="$RETRIEVAL_OUT/$RETRIEVAL_STAGE.json" \
    go test "$RETRIEVAL_PACKAGE" -run "$RETRIEVAL_TEST" -count=1 -v
done
printf 'Offline Brain reports: %s\n' "$RETRIEVAL_OUT"
