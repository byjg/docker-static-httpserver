#!/usr/bin/env bash
#
# Refuses to build anything publishable when a change was made without the
# matching version bump.
#
# Two failures this guards against:
#   - a chart version that did not move, which makes the published .tgz
#     overwrite the previous one and drop it from the repo index entirely;
#   - an appVersion that disagrees with the release tag, which points the chart
#     at an image tag that was never built.
#
set -euo pipefail

CHART_FILE="helm/static-httpserver/Chart.yaml"

# Everything that ends up inside the image. Touching it has to move appVersion.
APP_PATHS="main.go go.mod Makefile Dockerfile html/"
# The chart itself. Touching it has to move the chart version. Chart.yaml is
# excluded: bumping either version edits it, and that must not count as a chart
# change or an appVersion bump would demand a chart bump alongside it.
CHART_PATHS="helm/"

usage() {
  cat <<'EOF'
Usage:
  scripts/check-version.sh pr  <base-ref>    compare against the PR base branch
  scripts/check-version.sh tag <tag-name>    compare against the previous tag

Examples:
  scripts/check-version.sh pr origin/master
  scripts/check-version.sh tag v0.6.0
EOF
}

# field_at <git-ref, or empty for the working tree> <field name>
field_at() {
  local ref="$1" field="$2" content
  if [ -z "$ref" ]; then
    content=$(cat "$CHART_FILE")
  else
    content=$(git show "$ref:$CHART_FILE")
  fi
  printf '%s\n' "$content" |
    sed -nE "s/^${field}:[[:space:]]*\"?([^\"[:space:]]+)\"?.*/\1/p" |
    head -1
}

# changed_under <newline separated file list> <space separated path prefixes>
changed_under() {
  local files="$1" prefix
  for prefix in $2; do
    if printf '%s\n' "$files" | grep -qE "^${prefix}"; then
      return 0
    fi
  done
  return 1
}

STATUS=0
fail() {
  echo "FAIL: $*" >&2
  STATUS=1
}

MODE="${1:-}"
REF="${2:-}"
if [ -z "$MODE" ] || [ -z "$REF" ]; then
  usage
  exit 1
fi

case "$MODE" in
  pr)
    CHANGED=$(git diff --name-only "$REF...HEAD")
    echo "Files changed against $REF:"
    printf '%s\n' "$CHANGED" | sed 's/^/  /'
    echo

    CHART_CHANGES=$(printf '%s\n' "$CHANGED" | grep -E "^${CHART_PATHS}" | grep -vxF "$CHART_FILE" || true)
    APP_CHANGED=no
    changed_under "$CHANGED" "$APP_PATHS" && APP_CHANGED=yes

    # An application change moves appVersion, appVersion lives in the chart, and
    # the chart is republished on every merge to master. So it lands as a new
    # .tgz and needs a chart version of its own, exactly as a template change
    # does: whenever either side moves, the chart version moves with it.
    if [ -n "$CHART_CHANGES" ] || [ "$APP_CHANGED" = yes ]; then
      OLD=$(field_at "$REF" version)
      NEW=$(field_at "" version)
      if [ "$OLD" = "$NEW" ]; then
        if [ -n "$CHART_CHANGES" ]; then
          WHY="the chart changed"
        else
          WHY="the application changed, which republishes the chart"
        fi
        fail "$WHY, but the chart version is still $NEW. Bump 'version' in $CHART_FILE."
      else
        echo "OK: chart version $OLD -> $NEW"
      fi
    else
      echo "SKIP: neither the chart nor the application changed, chart version not required to move"
    fi

    if [ "$APP_CHANGED" = yes ]; then
      OLD=$(field_at "$REF" appVersion)
      NEW=$(field_at "" appVersion)
      if [ "$OLD" = "$NEW" ]; then
        fail "the application changed but appVersion is still $NEW. Bump 'appVersion' in $CHART_FILE."
      else
        echo "OK: appVersion $OLD -> $NEW"
      fi
    else
      echo "SKIP: application untouched, appVersion not required to move"
    fi
    ;;

  tag)
    WANT="${REF#v}"

    APP=$(field_at "" appVersion)
    if [ "$APP" != "$WANT" ]; then
      fail "tag $REF expects appVersion $WANT, but $CHART_FILE says $APP."
    else
      echo "OK: appVersion $APP matches tag $REF"
    fi

    # The tag is already checked out, so the previous tag is the one before it.
    PREV=$(git describe --abbrev=0 --tags HEAD^ 2>/dev/null || true)
    if [ -z "$PREV" ]; then
      echo "SKIP: no previous tag to compare the chart version against"
    else
      OLD=$(field_at "$PREV" version)
      NEW=$(field_at "" version)
      if [ "$OLD" = "$NEW" ]; then
        fail "chart version is still $NEW since $PREV. A published chart version is immutable: bump 'version' in $CHART_FILE or the new .tgz replaces the old one and $OLD disappears from the repo index."
      else
        echo "OK: chart version $OLD ($PREV) -> $NEW"
      fi
    fi
    ;;

  *)
    usage
    exit 1
    ;;
esac

exit "$STATUS"
