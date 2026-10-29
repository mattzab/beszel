#!/usr/bin/env bash
# Fork (mattzab/beszel): weekly upstream sync and release tagging.
#
# Usage:
#   scripts/fork-sync.sh            Merge the latest upstream release into mz if
#                                   it is newer, test, push mz, tag vX.Y.Z-mz.1.
#   scripts/fork-sync.sh --bump     Same, but if upstream has nothing new, tag
#                                   fork-only commits as the next -mz.N.
#   scripts/fork-sync.sh --dry-run  Merge and test locally; push and tag nothing.
#
# Exit codes: 0 ok / nothing to do, 1 usage or setup error, 2 merge conflict,
# 3 test or build failure. Nothing is pushed on any failure.
#
# Pushing the tag triggers .github/workflows/fork-release.yml, which publishes
# the hub release the VPS hub updater pulls from.
set -euo pipefail

BRANCH=mz
UPSTREAM_REMOTE=upstream
UPSTREAM_URL=https://github.com/henrygd/beszel.git
ORIGIN_REMOTE=origin
UPSTREAM_REPO=henrygd/beszel
FORK_REPO=mattzab/beszel

BUMP=0
DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --bump) BUMP=1 ;;
    --dry-run) DRY_RUN=1 ;;
    -h|--help) sed -n '2,15p' "$0"; exit 0 ;;
    *) echo "Unknown argument: $arg" >&2; exit 1 ;;
  esac
done

log() { printf '\n==> %s\n' "$*"; }
die() { local code=$1; shift; printf '\nERROR: %s\n' "$*" >&2; exit "$code"; }

cd "$(git rev-parse --show-toplevel)"

[ -z "$(git status --porcelain --untracked-files=no)" ] || die 1 "Working tree has uncommitted changes. Commit or stash them first."

if ! git remote get-url "$UPSTREAM_REMOTE" >/dev/null 2>&1; then
  git remote add "$UPSTREAM_REMOTE" "$UPSTREAM_URL"
fi

log "Fetching $ORIGIN_REMOTE and $UPSTREAM_REMOTE"
git fetch --quiet "$ORIGIN_REMOTE" --tags
git fetch --quiet "$UPSTREAM_REMOTE" --tags

git checkout --quiet "$BRANCH"
git merge --quiet --ff-only "$ORIGIN_REMOTE/$BRANCH" || die 1 "Local $BRANCH has diverged from $ORIGIN_REMOTE/$BRANCH. Fix that first."

# Latest published upstream release. Drafts are not "latest", so this skips
# tags whose release Henry hasn't published yet.
UPSTREAM_TAG=""
if command -v gh >/dev/null 2>&1; then
  UPSTREAM_TAG=$(gh release view --repo "$UPSTREAM_REPO" --json tagName --jq .tagName 2>/dev/null || true)
fi
if [ -z "$UPSTREAM_TAG" ]; then
  UPSTREAM_TAG=$(git tag -l 'v*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1)
fi
[[ "$UPSTREAM_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die 1 "Could not determine the latest upstream release tag (got '$UPSTREAM_TAG')."
git rev-parse --quiet --verify "refs/tags/$UPSTREAM_TAG" >/dev/null || die 1 "Upstream tag $UPSTREAM_TAG is not fetched locally."

LAST_FORK_TAG=$(git tag -l 'v*-mz.*' --merged "$BRANCH" | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+-mz\.[0-9]+$' | sort -V | tail -1 || true)
[ -n "$LAST_FORK_TAG" ] || die 1 "No vX.Y.Z-mz.N tag found on $BRANCH."
LAST_BASE=${LAST_FORK_TAG%-mz.*}
LAST_N=${LAST_FORK_TAG##*-mz.}

echo "Latest upstream release: $UPSTREAM_TAG"
echo "Last fork release:       $LAST_FORK_TAG (based on $LAST_BASE)"

NEW_TAG=""
MERGED=0
PRE_MERGE=$(git rev-parse HEAD)

newest=$(printf '%s\n%s\n' "$LAST_BASE" "$UPSTREAM_TAG" | sort -V | tail -1)
if [ "$UPSTREAM_TAG" != "$LAST_BASE" ] && [ "$newest" = "$UPSTREAM_TAG" ]; then
  log "Merging upstream $UPSTREAM_TAG into $BRANCH"
  if ! git merge --no-ff --no-edit -m "Merge upstream $UPSTREAM_TAG into $BRANCH" "$UPSTREAM_TAG"; then
    echo
    git diff --name-only --diff-filter=U | sed 's/^/  conflict: /'
    git merge --abort
    die 2 "Merge conflict with upstream $UPSTREAM_TAG. Merge aborted, nothing pushed. Resolve manually: git merge $UPSTREAM_TAG"
  fi
  MERGED=1
  NEW_TAG="$UPSTREAM_TAG-mz.1"
elif [ "$BUMP" = 1 ]; then
  if [ "$(git rev-parse "$LAST_FORK_TAG^{commit}")" = "$(git rev-parse HEAD)" ]; then
    echo "No upstream release and no new fork commits since $LAST_FORK_TAG. Nothing to do."
    exit 0
  fi
  NEW_TAG="$LAST_BASE-mz.$((LAST_N + 1))"
else
  echo "Already up to date with upstream $UPSTREAM_TAG."
  if [ "$(git rev-parse "$LAST_FORK_TAG^{commit}")" != "$(git rev-parse HEAD)" ]; then
    echo "Note: $BRANCH has commits after $LAST_FORK_TAG. Run with --bump to release them."
  fi
  exit 0
fi

git rev-parse --quiet --verify "refs/tags/$NEW_TAG" >/dev/null && die 1 "Tag $NEW_TAG already exists."

undo_merge() {
  if [ "$MERGED" = 1 ]; then
    git reset --quiet --hard "$PRE_MERGE"
    echo "Local merge undone ($BRANCH reset to $PRE_MERGE)."
  fi
}

log "Building site (needed for hub embed)"
if ! { bun install --no-save --cwd ./internal/site && bun run --cwd ./internal/site build; } >/tmp/fork-sync-site.log 2>&1; then
  tail -30 /tmp/fork-sync-site.log
  undo_merge
  die 3 "Site build failed (log: /tmp/fork-sync-site.log). Nothing pushed."
fi

log "Running tests"
if ! go vet ./internal/alerts/... ./internal/ghupdate/... ./internal/hub/... ./internal/migrations/...; then
  undo_merge
  die 3 "go vet failed. Nothing pushed."
fi
if ! go test -tags testing -count=1 ./internal/alerts/... ./internal/ghupdate/... ./internal/hub/... ./internal/migrations/...; then
  undo_merge
  die 3 "Tests failed. Nothing pushed."
fi
if ! CGO_ENABLED=0 go build -o /dev/null ./internal/cmd/hub; then
  undo_merge
  die 3 "Hub build failed. Nothing pushed."
fi

if [ "$DRY_RUN" = 1 ]; then
  echo
  echo "Dry run OK. Would push $BRANCH and tag $NEW_TAG."
  undo_merge
  exit 0
fi

log "Pushing $BRANCH and tag $NEW_TAG"
git push "$ORIGIN_REMOTE" "$BRANCH"
git tag -a "$NEW_TAG" -m "Fork release $NEW_TAG"
git push "$ORIGIN_REMOTE" "refs/tags/$NEW_TAG"

echo
echo "Done. Release workflow: https://github.com/$FORK_REPO/actions/workflows/fork-release.yml"
echo "Release (when built):   https://github.com/$FORK_REPO/releases/tag/$NEW_TAG"
