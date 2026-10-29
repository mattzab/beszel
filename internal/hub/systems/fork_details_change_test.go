//go:build testing

package systems

// Fork (mattzab/beszel): kernel / OS change detection on system_details writes.

import (
	"testing"

	"github.com/henrygd/beszel/internal/alerts"
	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/require"
)

type detailsStubHub struct {
	stubHub
	changes []alerts.SystemDetailsChange
}

func (h *detailsStubHub) HandleSystemDetailsChange(_ *core.Record, change alerts.SystemDetailsChange) error {
	h.changes = append(h.changes, change)
	return nil
}

func TestCreateRecordsDetectsKernelChange(t *testing.T) {
	sys, app := newTestSystemWithHub(t)
	hub := &detailsStubHub{stubHub: sys.manager.hub.(stubHub)}
	sys.manager.hub = hub

	details := func(kernel, os string) *system.CombinedData {
		return &system.CombinedData{Details: &system.Details{Hostname: "host", Kernel: kernel, OsName: os}}
	}

	// first observation: no change
	_, err := sys.createRecords(details("6.8.0-45", "Ubuntu 24.04"))
	require.NoError(t, err)
	require.Empty(t, hub.changes)

	// same values: no change
	_, err = sys.createRecords(details("6.8.0-45", "Ubuntu 24.04"))
	require.NoError(t, err)
	require.Empty(t, hub.changes)

	// no details in payload: no change
	_, err = sys.createRecords(&system.CombinedData{})
	require.NoError(t, err)
	require.Empty(t, hub.changes)

	// kernel upgrade
	_, err = sys.createRecords(details("6.8.0-48", "Ubuntu 24.04"))
	require.NoError(t, err)
	require.Len(t, hub.changes, 1)
	require.Equal(t, "6.8.0-45", hub.changes[0].OldKernel)
	require.Equal(t, "6.8.0-48", hub.changes[0].NewKernel)

	record, err := app.FindRecordById("system_details", sys.Id)
	require.NoError(t, err)
	require.Equal(t, "6.8.0-48", record.GetString("kernel"))
}
