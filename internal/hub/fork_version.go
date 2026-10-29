package hub

// Fork (mattzab/beszel): hub self-update source and fork version.

import "github.com/henrygd/beszel"

// ForkOwner is the GitHub owner the hub updates from.
const ForkOwner = "mattzab"

// ForkVersion is the fork release version (for example 0.21.1-mz.1), set at
// build time with -ldflags "-X github.com/henrygd/beszel/internal/hub.ForkVersion=...".
// beszel.Version stays upstream because agents parse it from the SSH handshake.
var ForkVersion = ""

// CurrentForkVersion returns the version the hub updater compares against
// fork releases. Builds without ForkVersion fall back to the upstream version.
func CurrentForkVersion() string {
	if ForkVersion != "" {
		return ForkVersion
	}
	return beszel.Version
}

// DisplayVersion is shown by `beszel --version`.
func DisplayVersion() string {
	if ForkVersion != "" && ForkVersion != beszel.Version {
		return beszel.Version + " (fork " + ForkVersion + ")"
	}
	return beszel.Version
}

// forkReleaseURL is the GitHub API URL for the fork's latest release.
const forkReleaseURL = "https://api.github.com/repos/" + ForkOwner + "/beszel/releases/latest"
