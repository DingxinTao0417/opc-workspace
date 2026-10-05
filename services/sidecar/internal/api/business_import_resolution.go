package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// What happens to incoming rows that meet a non-empty target (ADR-032):
//
//   - keep_target: the row belongs to a closed set whose key is the same in
//     every workspace (settings and avatar, code-owned Agent identities). The
//     target's row stays and the incoming one is not written.
//   - skip_identical: the primary key already exists and every column is the
//     same, so the row has been imported before.
//   - anything else with an existing primary key is an unresolved conflict,
//     and a row that would break a UNIQUE index is a semantic conflict. Both
//     block the import before any backup is made.

const (
	importResolutionKeepTarget     = "keep_target"
	importResolutionSkipIdentical  = "skip_identical"
	importBlockerSemanticConflicts = "target_semantic_conflicts"
)

type businessImportResolution struct {
	Table string `json:"table"`
	Rule  string `json:"rule"`
	Rows  int    `json:"rows"`
}

type businessImportSemanticConflict struct {
	Table     string `json:"table"`
	Index     string `json:"index"`
	Conflicts int    `json:"conflicts"`
}

// businessImportUntouchedRow is a target row that an import decided not to
// write over. It has to be exactly the same after the apply.
type businessImportUntouchedRow struct {
	table     string
	columns   []string
	keys      []string
	keyValues []any
	canonical string
}

type businessImportPlan struct {
	// apply holds the rows that will be written.
	apply       businessExportPackage
	resolutions []businessImportResolution
	semantic    []businessImportSemanticConflict
	untouched   []businessImportUntouchedRow
	report      businessImportConflictReport
}

func (p *businessImportPlan) appliedRows() int {
	total := 0
	for _, table := range p.apply.Tables {
		total += len(table.Rows)
	}
	return total
}

func (p *businessImportPlan) semanticConflicts() int {
	total := 0
	for _, item := range p.semantic {
		total += item.Conflicts
	}
	return total
}

// verifyUntouched runs inside the apply transaction. A trigger that changed a
// row the plan promised to leave alone rolls the whole import back.
func (p *businessImportPlan) verifyUntouched(tx *gorm.DB) error {
	for _, row := range p.untouched {
		quoted := make([]string, len(row.columns))
		for index, column := range row.columns {
			quoted[index] = quoteIdentifier(column)
		}
		conditions := make([]string, len(row.keys))
		for index, key := range row.keys {
			conditions[index] = quoteIdentifier(key) + " IS ?"
		}
		actual := make([]any, len(row.columns))
		destinations := make([]any, len(actual))
		for index := range actual {
			destinations[index] = &actual[index]
		}
		query := "SELECT " + strings.Join(quoted, ",") + " FROM " + quoteIdentifier(row.table) + " WHERE " + strings.Join(conditions, " AND ")
		if err := tx.Raw(query, row.keyValues...).Row().Scan(destinations...); err != nil {
			return fmt.Errorf("re-read kept import row %s: %w", row.table, err)
		}
		canonical, err := canonicalBusinessImportKey(actual)
		if err != nil {
			return err
		}
		if canonical != row.canonical {
			return fmt.Errorf("import changed a row of %s that it was meant to leave alone", row.table)
		}
	}
	return nil
}

// digest names the plan a user confirmed. It changes when the package, the
// apply mode or any resolution changes.
func businessImportPlanDigest(preview businessImportPreview) string {
	payload, _ := json.Marshal(struct {
		SchemaVersion int                        `json:"schema_version"`
		TargetSchema  int                        `json:"target_schema_version"`
		ExportedAt    string                     `json:"exported_at"`
		TableCounts   map[string]int             `json:"table_counts"`
		ApplyMode     string                     `json:"apply_mode"`
		Resolutions   []businessImportResolution `json:"resolutions"`
	}{
		preview.SchemaVersion, preview.TargetSchemaVersion, preview.ExportedAt,
		preview.TableCounts, preview.ApplyMode, preview.Resolutions,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// keepTargetSettings reports whether the target already owns settings or an
// avatar. They are kept as a unit: a workspace profile refers to its avatar.
func keepTargetSettings(tx *gorm.DB) (bool, error) {
	var count int64
	err := tx.Raw(`
		SELECT (SELECT COUNT(*) FROM app_settings) +
		       (SELECT COUNT(*) FROM workspace_avatars WHERE deleted_at IS NULL)
	`).Row().Scan(&count)
	return count != 0, err
}

func codeOwnedAgentAdapterID(id string) bool {
	for _, preset := range agentAdapterPresets {
		if preset.ID == id {
			return true
		}
	}
	return false
}

// planBusinessImport classifies every incoming row against the target. It
// reads only; the single temporary table it uses lives in the connection's
// temp schema and is dropped before it returns.
func planBusinessImport(tx *gorm.DB, packageData businessExportPackage) (*businessImportPlan, error) {
	plan := &businessImportPlan{
		apply:       packageData,
		resolutions: make([]businessImportResolution, 0),
		semantic:    make([]businessImportSemanticConflict, 0),
		report:      businessImportConflictReport{Tables: make([]businessImportTableConflict, 0)},
	}
	plan.apply.Tables = append([]businessExportTable(nil), packageData.Tables...)
	tables := make(map[string]int, len(packageData.Tables))
	for index, table := range packageData.Tables {
		tables[table.Name] = index
	}
	settingsKept, err := keepTargetSettings(tx)
	if err != nil {
		return nil, err
	}
	// The events of a kept Adapter are dropped with it, and events come before
	// Adapters in table order, so the kept Adapters are found first.
	keptAdapters := make(map[string]struct{})
	if index, ok := tables["agent_adapters"]; ok {
		incoming := packageData.Tables[index]
		for _, row := range incoming.Rows {
			id := stringColumn(incoming, row, "id")
			if !codeOwnedAgentAdapterID(id) {
				continue
			}
			var existing int64
			if err := tx.Raw("SELECT COUNT(*) FROM agent_adapters WHERE id = ?", id).Row().Scan(&existing); err != nil {
				return nil, err
			}
			if existing != 0 {
				keptAdapters[id] = struct{}{}
			}
		}
	}
	resolved := make(map[string]map[string]int)
	note := func(table, rule string) {
		if resolved[table] == nil {
			resolved[table] = make(map[string]int)
		}
		resolved[table][rule]++
	}

	for _, spec := range businessExportTables {
		primaryKeys, err := tablePrimaryKeyColumns(tx, spec.Name)
		if err != nil || len(primaryKeys) == 0 {
			return nil, fmt.Errorf("read import conflict key %s: %w", spec.Name, err)
		}
		incoming := packageData.Tables[tables[spec.Name]]
		keyIndexes := make([]int, len(primaryKeys))
		for index, column := range primaryKeys {
			keyIndexes[index] = columnIndex(incoming.Columns, column)
			if keyIndexes[index] < 0 {
				return nil, fmt.Errorf("import conflict key %s.%s is missing", spec.Name, column)
			}
		}
		incomingKeys := make(map[string]struct{}, len(incoming.Rows))
		rowKeys := make([]string, len(incoming.Rows))
		for index, row := range incoming.Rows {
			values := make([]any, len(keyIndexes))
			for position, rowIndex := range keyIndexes {
				values[position] = row[rowIndex]
			}
			key, err := canonicalBusinessImportKey(values)
			if err != nil {
				return nil, err
			}
			if _, duplicate := incomingKeys[key]; duplicate {
				return nil, &businessImportError{422, "IMPORT_ROW_INVALID", "Import rows contain a duplicate primary key"}
			}
			incomingKeys[key] = struct{}{}
			rowKeys[index] = key
		}
		lookupKeys := incomingKeys
		if spec.Name == "projects" {
			// A deleted Project that only survives as an Inbox source must not
			// come back under an identifier the target already uses.
			lookupKeys = make(map[string]struct{}, len(incomingKeys))
			for key := range incomingKeys {
				lookupKeys[key] = struct{}{}
			}
			for _, sourceID := range projectCompletionImportTombstoneSourceIDs(packageData.Tables[tables["inbox_items"]]) {
				key, err := canonicalBusinessImportKey([]any{sourceID})
				if err != nil {
					return nil, err
				}
				lookupKeys[key] = struct{}{}
			}
		}
		targetRows, overlapping, err := scanBusinessImportTargetRows(tx, spec.Name, incoming.Columns, primaryKeys, lookupKeys)
		if err != nil {
			return nil, err
		}

		unresolved := 0
		kept := make([][]any, 0, len(incoming.Rows))
		for index, row := range incoming.Rows {
			key := rowKeys[index]
			target, exists := overlapping[key]
			switch {
			case settingsKept && (spec.Name == "app_settings" || spec.Name == "workspace_avatars"):
				note(spec.Name, importResolutionKeepTarget)
				if exists {
					plan.untouched = append(plan.untouched, target.untouched(spec.Name, incoming.Columns, primaryKeys))
				}
			case settingsKept && spec.Name == "workflow_events" && stringColumn(incoming, row, "aggregate_type") == "setting":
				note(spec.Name, importResolutionKeepTarget)
			case spec.Name == "workflow_events" && stringColumn(incoming, row, "aggregate_type") == "agent_adapter" && hasKey(keptAdapters, stringColumn(incoming, row, "aggregate_id")):
				note(spec.Name, importResolutionKeepTarget)
			case !exists:
				kept = append(kept, row)
			case spec.Name == "agent_adapters" && hasKey(keptAdapters, stringColumn(incoming, row, "id")):
				note(spec.Name, importResolutionKeepTarget)
				plan.untouched = append(plan.untouched, target.untouched(spec.Name, incoming.Columns, primaryKeys))
			case spec.Name == "actors" && stringColumn(incoming, row, "id") == agentAdapterBuiltinActorID:
				note(spec.Name, importResolutionKeepTarget)
				plan.untouched = append(plan.untouched, target.untouched(spec.Name, incoming.Columns, primaryKeys))
			default:
				canonical, err := canonicalBusinessImportKey(append([]any(nil), row...))
				if err != nil {
					return nil, err
				}
				if canonical == target.canonical {
					note(spec.Name, importResolutionSkipIdentical)
					plan.untouched = append(plan.untouched, target.untouched(spec.Name, incoming.Columns, primaryKeys))
				} else {
					unresolved++
					kept = append(kept, row)
				}
			}
		}
		if spec.Name == "projects" {
			// Tombstone identifiers have no incoming row of their own.
			for key := range lookupKeys {
				if _, ordinary := incomingKeys[key]; ordinary {
					continue
				}
				if _, exists := overlapping[key]; exists {
					unresolved++
				}
			}
		}
		incoming.Rows = kept
		plan.apply.Tables[tables[spec.Name]] = incoming
		if targetRows == 0 {
			continue
		}
		plan.report.TargetRows += targetRows
		plan.report.KeyConflicts += unresolved
		plan.report.Tables = append(plan.report.Tables, businessImportTableConflict{
			Table: spec.Name, IncomingRows: len(packageData.Tables[tables[spec.Name]].Rows), TargetRows: targetRows, KeyConflicts: unresolved,
		})
	}

	for _, spec := range businessExportTables {
		for _, rule := range []string{importResolutionKeepTarget, importResolutionSkipIdentical} {
			if rows := resolved[spec.Name][rule]; rows != 0 {
				plan.resolutions = append(plan.resolutions, businessImportResolution{Table: spec.Name, Rule: rule, Rows: rows})
			}
		}
	}
	if plan.report.KeyConflicts != 0 {
		return plan, nil
	}
	for _, spec := range businessExportTables {
		conflicts, err := businessImportSemanticConflicts(tx, plan.apply.Tables[tables[spec.Name]])
		if err != nil {
			return nil, err
		}
		plan.semantic = append(plan.semantic, conflicts...)
	}
	return plan, nil
}

func hasKey(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}

func stringColumn(table businessExportTable, row []any, column string) string {
	index := columnIndex(table.Columns, column)
	if index < 0 || index >= len(row) {
		return ""
	}
	value, _ := row[index].(string)
	return value
}

type businessImportTargetRow struct {
	values    []any
	canonical string
}

func (r businessImportTargetRow) untouched(table string, columns, keys []string) businessImportUntouchedRow {
	keyValues := make([]any, len(keys))
	for index, key := range keys {
		keyValues[index] = r.values[columnIndex(columns, key)]
	}
	return businessImportUntouchedRow{
		table: table, columns: columns, keys: keys, keyValues: keyValues, canonical: r.canonical,
	}
}

// scanBusinessImportTargetRows counts the target rows that take part in
// conflict detection and returns the ones whose primary key is also incoming.
func scanBusinessImportTargetRows(tx *gorm.DB, table string, columns, primaryKeys []string, incomingKeys map[string]struct{}) (int, map[string]businessImportTargetRow, error) {
	quoted := make([]string, len(columns))
	for index, column := range columns {
		quoted[index] = quoteIdentifier(column)
	}
	query := "SELECT " + strings.Join(quoted, ",") + " FROM " + quoteIdentifier(table)
	if filter := businessImportTargetFilter(table); filter != "" {
		query += " WHERE " + filter
	}
	rows, err := tx.Raw(query).Rows()
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	keyIndexes := make([]int, len(primaryKeys))
	for index, key := range primaryKeys {
		keyIndexes[index] = columnIndex(columns, key)
	}
	total := 0
	overlapping := make(map[string]businessImportTargetRow)
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(values))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return 0, nil, err
		}
		total++
		keyValues := make([]any, len(keyIndexes))
		for index, position := range keyIndexes {
			keyValues[index] = values[position]
		}
		key, err := canonicalBusinessImportKey(keyValues)
		if err != nil {
			return 0, nil, err
		}
		if _, incoming := incomingKeys[key]; !incoming {
			continue
		}
		canonical, err := canonicalBusinessImportKey(values)
		if err != nil {
			return 0, nil, err
		}
		overlapping[key] = businessImportTargetRow{values: values, canonical: canonical}
	}
	return total, overlapping, rows.Err()
}

// businessImportSemanticConflicts counts, for every UNIQUE index of the table
// other than its primary key, the incoming rows that an insert would reject.
// The index's own key expressions and predicate are evaluated by SQLite on
// both sides, so partial and expression indexes need no rule of their own.
func businessImportSemanticConflicts(tx *gorm.DB, incoming businessExportTable) ([]businessImportSemanticConflict, error) {
	if len(incoming.Rows) == 0 {
		return nil, nil
	}
	var targetRows int64
	if err := tx.Raw("SELECT COUNT(*) FROM " + quoteIdentifier(incoming.Name)).Row().Scan(&targetRows); err != nil {
		return nil, err
	}
	if targetRows == 0 {
		return nil, nil
	}
	indexes, err := uniqueIndexDefinitions(tx, incoming.Name)
	if err != nil || len(indexes) == 0 {
		return nil, err
	}
	primaryKeys, err := tablePrimaryKeyColumns(tx, incoming.Name)
	if err != nil {
		return nil, err
	}

	probe := quoteIdentifier("opc_import_probe_" + incoming.Name)
	if err := tx.Exec("DROP TABLE IF EXISTS temp." + probe).Error; err != nil {
		return nil, err
	}
	if err := tx.Exec("CREATE TEMP TABLE " + probe + " AS SELECT * FROM main." + quoteIdentifier(incoming.Name) + " WHERE 0").Error; err != nil {
		return nil, err
	}
	defer tx.Exec("DROP TABLE IF EXISTS temp." + probe)
	columns := make([]string, len(incoming.Columns))
	placeholders := make([]string, len(incoming.Columns))
	for index, column := range incoming.Columns {
		columns[index] = quoteIdentifier(column)
		placeholders[index] = "?"
	}
	insert := "INSERT INTO temp." + probe + " (" + strings.Join(columns, ",") + ") VALUES (" + strings.Join(placeholders, ",") + ")"
	for _, row := range incoming.Rows {
		if err := tx.Exec(insert, normalizeImportValues(row)...).Error; err != nil {
			return nil, err
		}
	}

	quotedKeys := make([]string, len(primaryKeys))
	for index, key := range primaryKeys {
		quotedKeys[index] = quoteIdentifier(key)
	}
	result := make([]businessImportSemanticConflict, 0)
	for _, index := range indexes {
		selection := strings.Join(quotedKeys, ",") + "," + index.keys
		where := ""
		if index.predicate != "" {
			where = " WHERE " + index.predicate
		}
		target, err := uniqueIndexKeys(tx, "SELECT "+selection+" FROM main."+quoteIdentifier(incoming.Name)+where, len(primaryKeys))
		if err != nil {
			return nil, fmt.Errorf("read unique index %s of the target: %w", index.name, err)
		}
		candidates, err := uniqueIndexKeys(tx, "SELECT "+selection+" FROM temp."+probe+where, len(primaryKeys))
		if err != nil {
			return nil, fmt.Errorf("read unique index %s of the import: %w", index.name, err)
		}
		conflicts := 0
		for key, primaries := range candidates {
			owners, taken := target[key]
			if !taken {
				continue
			}
			for _, primary := range primaries {
				// The same primary key on both sides is a key conflict, which
				// is counted and resolved elsewhere.
				if len(owners) != 1 || owners[0] != primary {
					conflicts++
				}
			}
		}
		if conflicts != 0 {
			result = append(result, businessImportSemanticConflict{Table: incoming.Name, Index: index.name, Conflicts: conflicts})
		}
	}
	return result, nil
}

type uniqueIndexDefinition struct {
	name, keys, predicate string
}

func uniqueIndexDefinitions(tx *gorm.DB, table string) ([]uniqueIndexDefinition, error) {
	rows, err := tx.Raw(`SELECT name FROM pragma_index_list(` + quoteIdentifier(table) + `) WHERE "unique" = 1 AND origin <> 'pk' ORDER BY name`).Rows()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return nil, err
		}
		names = append(names, name)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	result := make([]uniqueIndexDefinition, 0, len(names))
	for _, name := range names {
		// An index that backs a UNIQUE constraint has no SQL of its own.
		var definition string
		if err := tx.Raw("SELECT COALESCE(sql, '') FROM sqlite_master WHERE type = 'index' AND name = ?", name).Row().Scan(&definition); err != nil {
			return nil, err
		}
		if definition == "" {
			columnRows, err := tx.Raw("SELECT name FROM pragma_index_info(" + quoteIdentifier(name) + ") ORDER BY seqno").Rows()
			if err != nil {
				return nil, err
			}
			columns := make([]string, 0)
			for columnRows.Next() {
				var column string
				if err := columnRows.Scan(&column); err != nil {
					_ = columnRows.Close()
					return nil, err
				}
				columns = append(columns, quoteIdentifier(column))
			}
			if err := columnRows.Close(); err != nil {
				return nil, err
			}
			result = append(result, uniqueIndexDefinition{name: name, keys: strings.Join(columns, ",")})
			continue
		}
		keys, predicate, ok := parseUniqueIndexDefinition(definition)
		if !ok {
			return nil, fmt.Errorf("unique index %s has a definition the import preflight cannot read", name)
		}
		result = append(result, uniqueIndexDefinition{name: name, keys: keys, predicate: predicate})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].name < result[right].name })
	return result, nil
}

// parseUniqueIndexDefinition splits "CREATE UNIQUE INDEX n ON t(keys) WHERE p"
// into its key list and predicate.
func parseUniqueIndexDefinition(definition string) (string, string, bool) {
	// Migrations write these statements over several lines; one space between
	// tokens makes the keywords findable.
	definition = strings.Join(strings.Fields(definition), " ")
	upper := strings.ToUpper(definition)
	on := strings.Index(upper, " ON ")
	if on < 0 {
		return "", "", false
	}
	open := strings.Index(definition[on:], "(")
	if open < 0 {
		return "", "", false
	}
	open += on
	depth := 0
	closing := -1
	inString := false
	for position := open; position < len(definition); position++ {
		switch character := definition[position]; {
		case character == '\'':
			inString = !inString
		case inString:
		case character == '(':
			depth++
		case character == ')':
			depth--
			if depth == 0 {
				closing = position
			}
		}
		if closing >= 0 {
			break
		}
	}
	if closing < 0 {
		return "", "", false
	}
	keys := strings.TrimSpace(definition[open+1 : closing])
	rest := strings.TrimSpace(definition[closing+1:])
	if rest == "" {
		return keys, "", keys != ""
	}
	if !strings.HasPrefix(strings.ToUpper(rest), "WHERE ") {
		return "", "", false
	}
	return keys, strings.TrimSpace(rest[len("WHERE "):]), keys != ""
}

// uniqueIndexKeys maps each index key to the primary keys of the rows that
// hold it. A key with a NULL part never collides.
func uniqueIndexKeys(tx *gorm.DB, query string, primaryColumns int) (map[string][]string, error) {
	rows, err := tx.Raw(query).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := make(map[string][]string)
	for rows.Next() {
		values := make([]any, len(names))
		destinations := make([]any, len(values))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}
		hasNull := false
		for _, value := range values[primaryColumns:] {
			if value == nil {
				hasNull = true
			}
		}
		if hasNull {
			continue
		}
		primary, err := canonicalBusinessImportKey(values[:primaryColumns])
		if err != nil {
			return nil, err
		}
		key, err := canonicalBusinessImportKey(values[primaryColumns:])
		if err != nil {
			return nil, err
		}
		result[key] = append(result[key], primary)
	}
	return result, rows.Err()
}
