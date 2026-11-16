package migrations

// Fork (mattzab/beszel): alert names for package update and kernel change alerts.

import (
	"slices"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	m "ganigeorgiev.com/pocketbase/pocketbase/migrations"
)

var forkAlertNames = []string{"SecurityUpdates", "PackageUpdates", "KernelChanged"}

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("alerts")
		if err != nil {
			return err
		}
		field := collection.Fields.GetByName("name").(*core.SelectField)
		for _, name := range forkAlertNames {
			if !slices.Contains(field.Values, name) {
				field.Values = append(field.Values, name)
			}
		}
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("alerts")
		if err != nil {
			return err
		}
		field := collection.Fields.GetByName("name").(*core.SelectField)
		field.Values = slices.DeleteFunc(field.Values, func(name string) bool {
			return slices.Contains(forkAlertNames, name)
		})
		return app.Save(collection)
	})
}
