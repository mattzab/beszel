package hub

// Fork (mattzab/beszel): lets package update alerts list package names.

import "github.com/henrygd/beszel/internal/entities/system"

// FetchPackageUpdates fetches the pending package list from a connected agent.
func (h *Hub) FetchPackageUpdates(systemID string) (system.PackageUpdates, error) {
	sys, err := h.sm.GetSystem(systemID)
	if err != nil {
		return system.PackageUpdates{}, err
	}
	return sys.FetchPackageUpdatesFromAgent()
}
