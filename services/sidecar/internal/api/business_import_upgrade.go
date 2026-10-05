package api

import (
	"net/http"
)

// A package exported by an older schema is brought to the current portable
// surface in memory before any validation. Every change that a migration made
// to the columns of a portable table is registered here as one step;
// TestBusinessImportPortableSurfaceChangesAreRegistered builds a database at
// every schema version with the real migrator and fails when a change has no
// step.

// businessImportUpgradeStep is one registered change, as reported in a preview.
type businessImportUpgradeStep struct {
	SchemaVersion int    `json:"schema_version"`
	Table         string `json:"table"`
	Change        string `json:"change"`
	Column        string `json:"column"`
}

type businessImportSurfaceChange struct {
	step  businessImportUpgradeStep
	apply func(table *businessExportTable) error
}

var businessImportSurfaceChanges = []businessImportSurfaceChange{
	{
		step: businessImportUpgradeStep{
			SchemaVersion: businessImportSchema71, Table: "actors", Change: "add_column", Column: "agent_adapter_id",
		},
		apply: upgradeActorsAddAgentAdapterID,
	},
}

// upgradeBusinessImportPackage returns the package converted to the portable
// surface of targetSchema, together with the steps that were applied. The
// input is not modified. A package that cannot have been produced by its
// declared schema is rejected.
func upgradeBusinessImportPackage(packageData businessExportPackage, targetSchema int) (businessExportPackage, []businessImportUpgradeStep, error) {
	applied := make([]businessImportUpgradeStep, 0)
	if packageData.Source.SchemaVersion >= targetSchema {
		return packageData, applied, nil
	}
	tables := append([]businessExportTable(nil), packageData.Tables...)
	for _, change := range businessImportSurfaceChanges {
		if packageData.Source.SchemaVersion >= change.step.SchemaVersion || change.step.SchemaVersion > targetSchema {
			continue
		}
		index := -1
		for candidate := range tables {
			if tables[candidate].Name == change.step.Table {
				index = candidate
				break
			}
		}
		if index < 0 {
			return businessExportPackage{}, nil, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_MANIFEST_INVALID", "The import table manifest is incomplete"}
		}
		// Copy the table before changing it; rows are rebuilt by the step.
		table := tables[index]
		table.Columns = append([]string(nil), table.Columns...)
		if err := change.apply(&table); err != nil {
			return businessExportPackage{}, nil, err
		}
		tables[index] = table
		applied = append(applied, change.step)
	}
	packageData.Tables = tables
	return packageData, applied, nil
}

// Schema 71 added actors.agent_adapter_id as the last column. Agent actors
// could not exist before it, so every earlier row gets NULL.
func upgradeActorsAddAgentAdapterID(table *businessExportTable) error {
	if columnIndex(table.Columns, "agent_adapter_id") >= 0 {
		return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_SCHEMA_MISMATCH", "The import table columns do not match the declared source schema"}
	}
	typeIndex := columnIndex(table.Columns, "type")
	if typeIndex < 0 {
		return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_SCHEMA_MISMATCH", "The import table columns do not match the declared source schema"}
	}
	rows := make([][]any, len(table.Rows))
	for index, row := range table.Rows {
		if typeIndex >= len(row) {
			return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_ROW_INVALID", "An import row does not match its table columns"}
		}
		if actorType, ok := row[typeIndex].(string); !ok || actorType == "agent" {
			return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_ROW_INVALID", "An Actor row is not valid for the declared source schema"}
		}
		rows[index] = append(append(make([]any, 0, len(row)+1), row...), nil)
	}
	table.Columns = append(table.Columns, "agent_adapter_id")
	table.Rows = rows
	return nil
}

// businessImportRequiresUpgradeConfirmation reports whether applying the
// previewed package crosses schema versions and therefore needs the separate
// confirmation phrase.
func businessImportRequiresUpgradeConfirmation(preview businessImportPreview) bool {
	return preview.SchemaVersion < preview.TargetSchemaVersion
}
