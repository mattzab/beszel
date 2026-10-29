package alerts

// Fork (mattzab/beszel): kernel / OS change alerts.

import (
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// alertNameKernelChanged fires when a system's kernel or OS name changes.
const alertNameKernelChanged = "KernelChanged"

// SystemDetailsChange describes a change in the details reported by an agent.
type SystemDetailsChange struct {
	OldKernel, NewKernel string
	OldOs, NewOs         string
}

// Changed reports whether the kernel or OS name changed. Empty values are
// treated as unknown and never count as a change.
func (c SystemDetailsChange) Changed() bool {
	return c.kernelChanged() || c.osChanged()
}

func (c SystemDetailsChange) kernelChanged() bool {
	return c.OldKernel != "" && c.NewKernel != "" && c.OldKernel != c.NewKernel
}

func (c SystemDetailsChange) osChanged() bool {
	return c.OldOs != "" && c.NewOs != "" && c.OldOs != c.NewOs
}

// HandleSystemDetailsChange sends KernelChanged alerts. It is an event alert:
// it notifies once per change and never stays triggered.
func (am *AlertManager) HandleSystemDetailsChange(systemRecord *core.Record, change SystemDetailsChange) error {
	if !change.Changed() {
		return nil
	}
	alerts := am.alertsCache.GetAlertsByName(systemRecord.Id, alertNameKernelChanged)
	if len(alerts) == 0 {
		return nil
	}
	systemName := systemRecord.GetString("name")
	title, message := systemDetailsChangeMessage(systemName, change)
	for _, alertData := range alerts {
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

func systemDetailsChangeMessage(systemName string, change SystemDetailsChange) (title, message string) {
	var parts, lines []string
	if change.osChanged() {
		parts = append(parts, "OS")
		lines = append(lines, fmt.Sprintf("OS: %s → %s", change.OldOs, change.NewOs))
	}
	if change.kernelChanged() {
		parts = append(parts, "kernel")
		lines = append(lines, fmt.Sprintf("Kernel: %s → %s", change.OldKernel, change.NewKernel))
	}
	title = fmt.Sprintf("%s changed on %s %v", capitalize(strings.Join(parts, " and ")), systemName, "\U0001F504")
	message = strings.Join(lines, "\n")
	return title, message
}
