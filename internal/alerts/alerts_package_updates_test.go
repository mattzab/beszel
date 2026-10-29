//go:build testing

package alerts_test

// Fork (mattzab/beszel): tests for package update and kernel change alerts.

import (
	"errors"
	"fmt"
	"testing"

	"github.com/henrygd/beszel/internal/alerts"
	systemEntity "github.com/henrygd/beszel/internal/entities/system"
	beszelTests "github.com/henrygd/beszel/internal/tests"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePackageHub overrides FetchPackageUpdates so tests don't need an agent.
type fakePackageHub struct {
	*beszelTests.TestHub
	updates systemEntity.PackageUpdates
	err     error
	calls   int
}

func (f *fakePackageHub) FetchPackageUpdates(string) (systemEntity.PackageUpdates, error) {
	f.calls++
	return f.updates, f.err
}

func packageTestSetup(t *testing.T, alertName string, value float64) (*beszelTests.TestHub, *core.Record, *core.Record) {
	t.Helper()
	hub, user := beszelTests.GetHubWithUser(t)
	userSettings, err := hub.FindFirstRecordByFilter("user_settings", "user={:user}", map[string]any{"user": user.Id})
	require.NoError(t, err)
	userSettings.Set("settings", `{"emails":["test@example.com"],"webhooks":[]}`)
	require.NoError(t, hub.Save(userSettings))

	systems, err := beszelTests.CreateSystems(hub, 1, user.Id, "paused")
	require.NoError(t, err)
	alert, err := beszelTests.CreateRecord(hub, "alerts", map[string]any{
		"name":   alertName,
		"system": systems[0].Id,
		"user":   user.Id,
		"value":  value,
	})
	require.NoError(t, err)
	return hub, systems[0], alert
}

func setPackageUpdates(system *core.Record, pu ...uint16) {
	system.Set("info", systemEntity.Info{PackageUpdates: pu})
}

func lastMessage(t *testing.T, hub *beszelTests.TestHub) (subject, text string) {
	t.Helper()
	messages := hub.TestMailer.Messages()
	require.NotEmpty(t, messages)
	last := messages[len(messages)-1]
	return last.Subject, last.Text
}

func isTriggered(t *testing.T, hub *beszelTests.TestHub, alertID string) bool {
	t.Helper()
	record, err := hub.FindRecordById("alerts", alertID)
	require.NoError(t, err)
	return record.GetBool("triggered")
}

func TestSecurityUpdatesAlertLifecycle(t *testing.T) {
	hub, system, alert := packageTestSetup(t, "SecurityUpdates", 1)
	defer hub.Cleanup()

	fake := &fakePackageHub{TestHub: hub, updates: systemEntity.PackageUpdates{
		Manager: "apt", SecurityKnown: true,
		Packages: []systemEntity.PackageUpdate{
			{Name: "openssl", Current: "3.0.2-0ubuntu1.15", Available: "3.0.2-0ubuntu1.16", Security: true},
			{Name: "vim", Current: "1", Available: "2"},
		},
	}}
	am := alerts.NewTestAlertManagerWithoutWorker(fake)
	sent := hub.TestMailer.TotalSend()

	// below threshold: nothing
	setPackageUpdates(system, 5, 0)
	require.NoError(t, am.HandlePackageUpdateAlerts(system))
	assert.Equal(t, sent, hub.TestMailer.TotalSend())
	assert.False(t, isTriggered(t, hub, alert.Id))

	// trigger, lists only security packages
	setPackageUpdates(system, 6, 1)
	require.NoError(t, am.HandlePackageUpdateAlerts(system))
	require.Equal(t, sent+1, hub.TestMailer.TotalSend())
	subject, text := lastMessage(t, hub)
	assert.Contains(t, subject, "Security updates on")
	assert.Contains(t, text, "1 security update pending")
	assert.Contains(t, text, "openssl (3.0.2-0ubuntu1.15 → 3.0.2-0ubuntu1.16)")
	assert.NotContains(t, text, "vim")
	assert.True(t, isTriggered(t, hub, alert.Id))

	historyCount, err := hub.CountRecords("alerts_history", dbx.HashExp{"resolved": ""})
	require.NoError(t, err)
	assert.EqualValues(t, 1, historyCount)

	// same count: no resend
	require.NoError(t, am.HandlePackageUpdateAlerts(system))
	assert.Equal(t, sent+1, hub.TestMailer.TotalSend())

	// count rises: re-notify
	setPackageUpdates(system, 8, 3)
	require.NoError(t, am.HandlePackageUpdateAlerts(system))
	require.Equal(t, sent+2, hub.TestMailer.TotalSend())
	subject, _ = lastMessage(t, hub)
	assert.Contains(t, subject, "More security updates")

	// count drops but still triggered: no message
	setPackageUpdates(system, 7, 2)
	require.NoError(t, am.HandlePackageUpdateAlerts(system))
	assert.Equal(t, sent+2, hub.TestMailer.TotalSend())

	// cleared: resolved message
	setPackageUpdates(system, 0, 0)
	require.NoError(t, am.HandlePackageUpdateAlerts(system))
	require.Equal(t, sent+3, hub.TestMailer.TotalSend())
	subject, text = lastMessage(t, hub)
	assert.Contains(t, subject, "Security updates cleared")
	assert.Contains(t, text, "No security updates are pending")
	assert.False(t, isTriggered(t, hub, alert.Id))

	historyCount, err = hub.CountRecords("alerts_history", dbx.HashExp{"resolved": ""})
	require.NoError(t, err)
	assert.EqualValues(t, 0, historyCount)
}

func TestSecurityUpdatesAlertUnknownSecurity(t *testing.T) {
	hub, system, alert := packageTestSetup(t, "SecurityUpdates", 1)
	defer hub.Cleanup()
	am := alerts.NewTestAlertManagerWithoutWorker(hub)
	sent := hub.TestMailer.TotalSend()

	// pacman-style: only a total
	setPackageUpdates(system, 40)
	require.NoError(t, am.HandlePackageUpdateAlerts(system))
	assert.Equal(t, sent, hub.TestMailer.TotalSend())
	assert.False(t, isTriggered(t, hub, alert.Id))

	// no package data at all
	setPackageUpdates(system)
	require.NoError(t, am.HandlePackageUpdateAlerts(system))
	assert.Equal(t, sent, hub.TestMailer.TotalSend())
}

func TestPackageUpdatesAlertThresholdAndList(t *testing.T) {
	hub, system, alert := packageTestSetup(t, "PackageUpdates", 10)
	defer hub.Cleanup()

	var pkgs []systemEntity.PackageUpdate
	for i := range 14 {
		pkgs = append(pkgs, systemEntity.PackageUpdate{Name: fmt.Sprintf("pkg%02d", i), Available: "1.0"})
	}
	fake := &fakePackageHub{TestHub: hub, updates: systemEntity.PackageUpdates{Packages: pkgs}}
	am := alerts.NewTestAlertManagerWithoutWorker(fake)
	sent := hub.TestMailer.TotalSend()

	setPackageUpdates(system, 9)
	require.NoError(t, am.HandlePackageUpdateAlerts(system))
	assert.Equal(t, sent, hub.TestMailer.TotalSend())

	setPackageUpdates(system, 14)
	require.NoError(t, am.HandlePackageUpdateAlerts(system))
	require.Equal(t, sent+1, hub.TestMailer.TotalSend())
	subject, text := lastMessage(t, hub)
	assert.Contains(t, subject, "Package updates on")
	assert.Contains(t, text, "14 package updates pending")
	assert.Contains(t, text, "pkg09 (1.0)")
	assert.NotContains(t, text, "pkg10")
	assert.Contains(t, text, "and 4 more")
	assert.True(t, isTriggered(t, hub, alert.Id))

	// total alerts don't re-notify on rise
	setPackageUpdates(system, 20)
	require.NoError(t, am.HandlePackageUpdateAlerts(system))
	assert.Equal(t, sent+1, hub.TestMailer.TotalSend())

	// below threshold resolves with a count
	setPackageUpdates(system, 3)
	require.NoError(t, am.HandlePackageUpdateAlerts(system))
	require.Equal(t, sent+2, hub.TestMailer.TotalSend())
	_, text = lastMessage(t, hub)
	assert.Contains(t, text, "3 package updates on")
	assert.Contains(t, text, "below the alert threshold of 10")
	assert.Equal(t, 1, fake.calls, "package list fetched once per notification, never for resolve")
}

func TestPackageUpdatesAlertFetchFailure(t *testing.T) {
	hub, system, _ := packageTestSetup(t, "PackageUpdates", 1)
	defer hub.Cleanup()
	fake := &fakePackageHub{TestHub: hub, err: errors.New("agent offline")}
	am := alerts.NewTestAlertManagerWithoutWorker(fake)
	sent := hub.TestMailer.TotalSend()

	setPackageUpdates(system, 2)
	require.NoError(t, am.HandlePackageUpdateAlerts(system))
	require.Equal(t, sent+1, hub.TestMailer.TotalSend())
	_, text := lastMessage(t, hub)
	assert.Contains(t, text, "2 package updates pending")
}

func TestKernelChangedAlert(t *testing.T) {
	hub, system, alert := packageTestSetup(t, "KernelChanged", 0)
	defer hub.Cleanup()
	am := alerts.NewTestAlertManagerWithoutWorker(hub)
	sent := hub.TestMailer.TotalSend()

	// first observation and unchanged values don't notify
	require.NoError(t, am.HandleSystemDetailsChange(system, alerts.SystemDetailsChange{NewKernel: "6.8.0-45", NewOs: "Ubuntu 24.04"}))
	require.NoError(t, am.HandleSystemDetailsChange(system, alerts.SystemDetailsChange{
		OldKernel: "6.8.0-45", NewKernel: "6.8.0-45", OldOs: "Ubuntu 24.04", NewOs: "Ubuntu 24.04",
	}))
	assert.Equal(t, sent, hub.TestMailer.TotalSend())

	require.NoError(t, am.HandleSystemDetailsChange(system, alerts.SystemDetailsChange{
		OldKernel: "6.8.0-45", NewKernel: "6.8.0-48", OldOs: "Ubuntu 24.04", NewOs: "Ubuntu 24.04",
	}))
	require.Equal(t, sent+1, hub.TestMailer.TotalSend())
	subject, text := lastMessage(t, hub)
	assert.Contains(t, subject, "Kernel changed on")
	assert.Contains(t, text, "Kernel: 6.8.0-45 → 6.8.0-48")
	assert.False(t, isTriggered(t, hub, alert.Id), "event alert never stays triggered")

	require.NoError(t, am.HandleSystemDetailsChange(system, alerts.SystemDetailsChange{
		OldKernel: "6.8.0-48", NewKernel: "6.14.0-1", OldOs: "Ubuntu 24.04", NewOs: "Ubuntu 26.04",
	}))
	require.Equal(t, sent+2, hub.TestMailer.TotalSend())
	subject, text = lastMessage(t, hub)
	assert.Contains(t, subject, "OS and kernel changed on")
	assert.Contains(t, text, "OS: Ubuntu 24.04 → Ubuntu 26.04")
}

func TestForkAlertsExcludedFromThresholdAlerts(t *testing.T) {
	hub, system, alert := packageTestSetup(t, "PackageUpdates", 1)
	defer hub.Cleanup()
	am := alerts.NewTestAlertManagerWithoutWorker(hub)

	// HandleSystemAlerts must not treat fork alerts as metric alerts.
	setPackageUpdates(system, 0)
	require.NoError(t, am.HandleSystemAlerts(system, &systemEntity.CombinedData{Info: systemEntity.Info{PackageUpdates: []uint16{0}}}))
	assert.False(t, isTriggered(t, hub, alert.Id))
}
