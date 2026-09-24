package api

import (
	"bytes"
	"encoding/csv"
	"errors"
	"strconv"
	"strings"
	"unicode"

	"gorm.io/gorm"
)

const maxFinancialCSVBytes = 16 << 20
const financialCSVColumns = "id,type,status,amount_minor,currency,occurred_on,category,client,project,invoice,notes,created_at,updated_at"

type financialCSV struct {
	Data   []byte
	Rows   int
	SHA256 string
}
type financialCSVBuffer struct{ bytes.Buffer }

func (b *financialCSVBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxFinancialCSVBytes {
		return 0, newFinancialEntryRequestError(413, "EXPORT_TOO_LARGE", "CSV export is limited to 16 MiB; narrow the filters")
	}
	return b.Buffer.Write(p)
}

// CSV quoting alone does not prevent spreadsheet formula interpretation. Keep
// user data intact in SQLite, but export risky text as an explicit text cell.
func financialCSVText(value string) string {
	trimmed := strings.TrimLeftFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
	if strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") || strings.HasPrefix(value, "\n") || strings.ContainsAny(firstCSVCharacter(trimmed), "=+-@＝＋－＠") {
		return "'" + value
	}
	return value
}
func firstCSVCharacter(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
}

func readFinancialCSV(tx *gorm.DB, filters financialEntryFilters, sort string) (financialCSV, error) {
	query, valid := applyFinancialEntrySort(financialEntryRowsQuery(applyFinancialEntryFilters(tx.Table("financial_entries AS entry"), filters)), sort)
	if !valid {
		return financialCSV{}, newFinancialEntryRequestError(400, "INVALID_SORT", "sort contains an unsupported field")
	}
	rows, err := query.Limit(10001).Rows()
	if err != nil {
		return financialCSV{}, err
	}
	defer rows.Close()
	var buffer financialCSVBuffer
	_, _ = buffer.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(&buffer)
	if err := writer.Write(strings.Split(financialCSVColumns, ",")); err != nil {
		return financialCSV{}, err
	}
	count := 0
	for rows.Next() {
		if count == 10000 {
			return financialCSV{}, newFinancialEntryRequestError(413, "EXPORT_TOO_LARGE", "CSV export is limited to 10000 matching entries; narrow the filters")
		}
		var row financialEntryRow
		if err := tx.ScanRows(rows, &row); err != nil {
			return financialCSV{}, err
		}
		count++
		values := []string{row.ID, row.Type, row.Status, strconv.FormatInt(row.AmountMinor, 10), row.Currency, row.OccurredOn, row.Category, stringValue(row.ClientName), stringValue(row.ProjectName), stringValue(row.InvoiceNumber), row.Notes, normalizeTimestamp(row.CreatedAt), normalizeTimestamp(row.UpdatedAt)}
		for i, v := range values {
			values[i] = financialCSVText(v)
		}
		if err := writer.Write(values); err != nil {
			return financialCSV{}, err
		}
	}
	if err := rows.Err(); err != nil {
		return financialCSV{}, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		var domain *financialEntryRequestError
		if errors.As(err, &domain) {
			return financialCSV{}, err
		}
		return financialCSV{}, newFinancialEntryRequestError(500, "EXPORT_FAILED", "Financial entries could not be encoded")
	}
	data := buffer.Bytes()
	return financialCSV{Data: data, Rows: count, SHA256: sha256Hex(data)}, nil
}
