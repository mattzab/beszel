# mattzab/beszel fork

Hub-only fork of [henrygd/beszel](https://github.com/henrygd/beszel). Agents should keep using upstream releases.

## What's added

- **Security Updates** alert: fires when pending security updates reach the threshold (default 1). It notifies again when the count rises and sends a cleared message when it falls back below the threshold. Security counts come from apt, dnf, and zypper.
- **Package Updates** alert: fires when total pending updates reach the threshold.
- **Kernel / OS Change** alert: notifies when an agent reports a different kernel or OS name, which it checks on reconnect.
- The hub self-updates (`beszel update`) from this fork's releases. The UI update check also points here.

Package data comes from stock agents v0.21.0+ on Linux, not in Docker. Agents check once an hour by default.

## Branches and releases

- `mz` is the default branch. It is upstream release tags plus fork commits.
- Tags look like `vX.Y.Z-mz.N`. Pushing one runs `.github/workflows/fork-release.yml` (`.goreleaser.fork.yml`), which publishes hub binaries with upstream asset names.
- `beszel.Version` stays upstream because agents parse it. The fork version is set at build time in `internal/hub.ForkVersion`.

## Weekly sync

```sh
scripts/fork-sync.sh            # merge the newest upstream release, test, push, tag vX.Y.Z-mz.1
scripts/fork-sync.sh --bump     # also release fork-only commits as -mz.N+1
scripts/fork-sync.sh --dry-run  # merge and test only
```

Fork-only files are named `fork_*`, `*_package_updates*`, `*_system_details*`, `*mz_fork*`, or `FORK.md`, which keeps merge conflicts rare.
