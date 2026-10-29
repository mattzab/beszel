package systems

// Fork (mattzab/beszel): detects kernel / OS changes in system details.

import (
	"github.com/henrygd/beszel/internal/alerts"
	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// systemDetailsChange aliases the alert type so system.go needs no new import.
type systemDetailsChange = alerts.SystemDetailsChange

// detailsChangeHandler is implemented by the hub (via the alert manager).
type detailsChangeHandler interface {
	HandleSystemDetailsChange(systemRecord *core.Record, change alerts.SystemDetailsChange) error
}

// readPreviousDetails returns the stored kernel and OS name before they are
// overwritten. Missing rows return an empty change, which never alerts.
func readPreviousDetails(app core.App, systemID string, data *system.Details) alerts.SystemDetailsChange {
	var row struct {
		Kernel string `db:"kernel"`
		OsName string `db:"os_name"`
	}
	_ = app.DB().Select("kernel", "os_name").From("system_details").
		Where(dbx.HashExp{"id": systemID}).One(&row)
	return alerts.SystemDetailsChange{
		OldKernel: row.Kernel, NewKernel: data.Kernel,
		OldOs: row.OsName, NewOs: data.OsName,
	}
}

// notifyDetailsChange forwards a details change to the hub if supported.
func notifyDetailsChange(hub core.App, systemRecord *core.Record, change *alerts.SystemDetailsChange) {
	if change == nil || !change.Changed() {
		return
	}
	handler, ok := hub.(detailsChangeHandler)
	if !ok {
		return
	}
	if err := handler.HandleSystemDetailsChange(systemRecord, *change); err != nil {
		hub.Logger().Error("Error handling system details change", "err", err)
	}
}
