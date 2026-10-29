package alerts

// Fork (mattzab/beszel): pending package update alerts.
//
// Stock agents (v0.21.0+) report pending package updates in Info.PackageUpdates
// as [total] or [total, security]. These alerts read that value from the saved
// system record, so no agent changes are needed.

import (
	"fmt"
	"strings"

	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/pocketbase/pocketbase/core"
)

const (
	// alertNameSecurityUpdates fires when pending security updates >= alert value.
	alertNameSecurityUpdates = "SecurityUpdates"
	// alertNamePackageUpdates fires when total pending updates >= alert value.
	alertNamePackageUpdates = "PackageUpdates"
	// maxListedPackages caps how many package names are listed in a notification.
	maxListedPackages = 10
)

// packageUpdatesFetcher is implemented by the hub to fetch the package list
// from a connected agent. It is optional; notifications omit names without it.
type packageUpdatesFetcher interface {
	FetchPackageUpdates(systemID string) (system.PackageUpdates, error)
}

// readPackageUpdateCounts reads [total, security] from the system record info.
// ok is false if the agent has not reported package updates. securityKnown is
// false if the package manager cannot flag security updates.
func readPackageUpdateCounts(systemRecord *core.Record) (total, security uint16, securityKnown, ok bool) {
	var info system.Info
	if err := systemRecord.UnmarshalJSONField("info", &info); err != nil {
		return 0, 0, false, false
	}
	pu := info.PackageUpdates
	if len(pu) == 0 {
		return 0, 0, false, false
	}
	total = pu[0]
	if len(pu) > 1 {
		security, securityKnown = pu[1], true
	}
	return total, security, securityKnown, true
}

// packageAlertThreshold returns the alert threshold, at least 1.
func packageAlertThreshold(value float64) uint16 {
	if value < 1 {
		return 1
	}
	if value > 65535 {
		return 65535
	}
	return uint16(value)
}

// packageAlertAction describes what to do for one alert after a check.
type packageAlertAction uint8

const (
	packageAlertNone packageAlertAction = iota
	packageAlertTrigger
	packageAlertRise
	packageAlertResolve
)

// evaluatePackageAlert decides the action for an alert given its current
// triggered state, the current count, and the last count seen while triggered.
// Only security alerts re-notify when the count rises.
func evaluatePackageAlert(name string, wasTriggered bool, count, threshold uint16, lastCount uint16, lastKnown bool) packageAlertAction {
	triggered := count >= threshold
	switch {
	case triggered && !wasTriggered:
		return packageAlertTrigger
	case !triggered && wasTriggered:
		return packageAlertResolve
	case triggered && name == alertNameSecurityUpdates && lastKnown && count > lastCount:
		return packageAlertRise
	}
	return packageAlertNone
}

// HandlePackageUpdateAlerts manages SecurityUpdates and PackageUpdates alerts.
func (am *AlertManager) HandlePackageUpdateAlerts(systemRecord *core.Record) error {
	alerts := append(
		am.alertsCache.GetAlertsByName(systemRecord.Id, alertNameSecurityUpdates),
		am.alertsCache.GetAlertsByName(systemRecord.Id, alertNamePackageUpdates)...,
	)
	if len(alerts) == 0 {
		return nil
	}
	total, security, securityKnown, ok := readPackageUpdateCounts(systemRecord)
	if !ok {
		return nil
	}

	systemName := systemRecord.GetString("name")
	var packages *system.PackageUpdates // fetched lazily, at most once

	for _, alertData := range alerts {
		count := total
		if alertData.Name == alertNameSecurityUpdates {
			if !securityKnown {
				continue
			}
			count = security
		}
		threshold := packageAlertThreshold(alertData.Value)

		var lastCount uint16
		lastKnown := false
		if v, found := am.packageUpdateCounts.Load(alertData.Id); found {
			lastCount, lastKnown = v.(uint16), true
		}
		am.packageUpdateCounts.Store(alertData.Id, count)

		action := evaluatePackageAlert(alertData.Name, alertData.Triggered, count, threshold, lastCount, lastKnown)
		if action == packageAlertNone {
			continue
		}
		if action != packageAlertRise {
			if err := am.setAlertTriggered(alertData, action == packageAlertTrigger); err != nil {
				am.hub.Logger().Error("Failed to update alert", "err", err)
				continue
			}
		}
		if action != packageAlertResolve && packages == nil {
			packages = am.fetchPackageUpdates(systemRecord.Id)
		}
		title, message := packageAlertMessage(alertData.Name, action, systemName, count, threshold, packages)
		if err := am.SendAlert(AlertMessageData{
			UserID:   alertData.UserID,
			SystemID: alertData.SystemID,
			Title:    title,
			Message:  message,
			Link:     am.hub.MakeLink("system", alertData.SystemID),
			LinkText: "View " + systemName,
		}); err != nil {
			am.hub.Logger().Error("Failed to send alert", "err", err)
		}
	}
	return nil
}

// fetchPackageUpdates returns the package list from the agent, or an empty
// result if the hub cannot fetch it.
func (am *AlertManager) fetchPackageUpdates(systemID string) *system.PackageUpdates {
	result := &system.PackageUpdates{}
	fetcher, ok := am.hub.(packageUpdatesFetcher)
	if !ok {
		return result
	}
	updates, err := fetcher.FetchPackageUpdates(systemID)
	if err != nil {
		am.hub.Logger().Debug("Failed to fetch package updates for alert", "err", err, "system", systemID)
		return result
	}
	return &updates
}

// packageAlertMessage builds the notification title and body.
func packageAlertMessage(name string, action packageAlertAction, systemName string, count, threshold uint16, packages *system.PackageUpdates) (title, message string) {
	security := name == alertNameSecurityUpdates
	kind := "package"
	if security {
		kind = "security"
	}
	if action == packageAlertResolve {
		title = fmt.Sprintf("%s updates cleared on %s %v", capitalize(kind), systemName, "✅")
		if count == 0 {
			message = fmt.Sprintf("No %s updates are pending on %s.", kind, systemName)
		} else {
			message = fmt.Sprintf("%s on %s, below the alert threshold of %d.", pluralizeUpdates(count, kind), systemName, threshold)
		}
		return title, message
	}

	emoji := "\U0001F7E1" // yellow circle
	if security {
		emoji = "\U0001F534" // red circle
	}
	if action == packageAlertRise {
		title = fmt.Sprintf("More %s updates on %s %v", kind, systemName, emoji)
	} else {
		title = fmt.Sprintf("%s updates on %s %v", capitalize(kind), systemName, emoji)
	}
	message = fmt.Sprintf("%s pending on %s.", pluralizeUpdates(count, kind), systemName)
	if packages != nil {
		var names []string
		for _, pkg := range packages.Packages {
			if security && !pkg.Security {
				continue
			}
			names = append(names, formatPackage(pkg))
		}
		if len(names) > 0 {
			message += "\n" + formatPackageList(names)
		}
	}
	return title, message
}

func formatPackage(pkg system.PackageUpdate) string {
	if pkg.Current != "" && pkg.Available != "" {
		return fmt.Sprintf("%s (%s → %s)", pkg.Name, pkg.Current, pkg.Available)
	}
	if pkg.Available != "" {
		return fmt.Sprintf("%s (%s)", pkg.Name, pkg.Available)
	}
	return pkg.Name
}

// formatPackageList joins package names one per line, truncating long lists.
func formatPackageList(names []string) string {
	if len(names) <= maxListedPackages {
		return strings.Join(names, "\n")
	}
	return fmt.Sprintf("%s\nand %d more", strings.Join(names[:maxListedPackages], "\n"), len(names)-maxListedPackages)
}

func pluralizeUpdates(count uint16, kind string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s update", kind)
	}
	return fmt.Sprintf("%d %s updates", count, kind)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
