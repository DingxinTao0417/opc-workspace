package api

import (
	_ "embed"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/signintech/gopdf"
)

// Noto Sans SC is distributed under the SIL Open Font License 1.1. The
// corresponding license and upstream checksum live beside the embedded font.
//
//go:embed fonts/NotoSansSC-Variable.ttf
var invoicePDFNotoSansSC []byte

const invoicePDFFontFamily = "NotoSansSC"

func renderInvoicePDF(destination io.Writer, invoice invoiceRow, generatedAt time.Time) error {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{Unit: gopdf.UnitPT, PageSize: *gopdf.PageSizeA4})
	if err := pdf.AddTTFFontData(invoicePDFFontFamily, invoicePDFNotoSansSC); err != nil {
		return fmt.Errorf("load embedded CJK font: %w", err)
	}
	notesFullyShown, err := addInvoicePDFSummaryPage(pdf, invoice, generatedAt)
	if err != nil {
		return err
	}
	if err := addInvoicePDFNotes(pdf, invoice.Notes, generatedAt, !notesFullyShown); err != nil {
		return err
	}
	if err := pdf.Write(destination); err != nil {
		return fmt.Errorf("encode invoice PDF: %w", err)
	}
	return nil
}

func addInvoicePDFSummaryPage(pdf *gopdf.GoPdf, invoice invoiceRow, generatedAt time.Time) (bool, error) {
	pdf.AddPage()
	pageWidth := gopdf.PageSizeA4.W
	const (
		left  = 52.0
		right = 52.0
	)
	contentWidth := pageWidth - left - right

	pdf.SetFillColor(17, 24, 39)
	pdf.RectFromUpperLeftWithStyle(0, 0, pageWidth, 158, "F")
	if err := invoicePDFText(pdf, left, 34, contentWidth, 20, 10, 148, 163, 184, "OPC WORKSPACE"); err != nil {
		return false, err
	}
	if err := invoicePDFText(pdf, left, 62, contentWidth, 42, 27, 248, 250, 252, "发票  INVOICE"); err != nil {
		return false, err
	}
	if err := invoicePDFText(pdf, left, 112, contentWidth*0.72, 24, 14, 203, 213, 225, invoice.InvoiceNumber); err != nil {
		return false, err
	}
	status := invoicePDFStatusLabel(invoice.Status)
	pdf.SetFillColor(30, 41, 59)
	pdf.RectFromUpperLeftWithStyle(pageWidth-right-94, 108, 94, 28, "F")
	if err := invoicePDFCell(pdf, pageWidth-right-94, 108, 94, 28, 11, 226, 232, 240, status, gopdf.Center|gopdf.Middle); err != nil {
		return false, err
	}

	if err := invoicePDFText(pdf, left, 191, contentWidth, 18, 10, 100, 116, 139, "应付金额 / AMOUNT DUE"); err != nil {
		return false, err
	}
	amount := fmt.Sprintf("%s %s", invoice.Currency, formatInvoiceMinorUnits(invoice.AmountMinor))
	if err := invoicePDFText(pdf, left, 215, contentWidth, 42, 28, 15, 23, 42, amount); err != nil {
		return false, err
	}
	pdf.SetStrokeColor(226, 232, 240)
	pdf.SetLineWidth(1)
	pdf.Line(left, 270, pageWidth-right, 270)

	projectName := "未关联项目"
	if invoice.ProjectName != nil && strings.TrimSpace(*invoice.ProjectName) != "" {
		projectName = strings.TrimSpace(*invoice.ProjectName)
	}
	rows := []struct {
		label string
		value string
	}{
		{"客户", invoice.ClientName},
		{"项目", projectName},
		{"开具日期", invoice.IssueDate},
		{"到期日期", invoice.DueDate},
		{"当前状态", status},
	}
	rowY := 298.0
	for index, row := range rows {
		if index%2 == 0 {
			pdf.SetFillColor(248, 250, 252)
			pdf.RectFromUpperLeftWithStyle(left, rowY-8, contentWidth, 39, "F")
		}
		if err := invoicePDFText(pdf, left+12, rowY, 108, 18, 10, 100, 116, 139, row.label); err != nil {
			return false, err
		}
		if err := invoicePDFText(pdf, left+132, rowY, contentWidth-144, 18, 11, 30, 41, 59, row.value); err != nil {
			return false, err
		}
		rowY += 45
	}

	if err := invoicePDFText(pdf, left, 554, contentWidth, 18, 10, 100, 116, 139, "备注摘要"); err != nil {
		return false, err
	}
	notes := sanitizeInvoicePDFNotes(invoice.Notes)
	if notes == "" {
		notes = "无"
	}
	summary := strings.Join(strings.Fields(notes), " ")
	if err := pdf.SetFont(invoicePDFFontFamily, "", 11); err != nil {
		return false, err
	}
	summaryLines, err := pdf.SplitText(summary, contentWidth)
	if err != nil {
		return false, fmt.Errorf("wrap invoice PDF notes summary: %w", err)
	}
	notesFullyShown := len(summaryLines) <= 2
	if len(summaryLines) > 2 {
		summaryLines = summaryLines[:2]
		summaryLines[1] += "…"
	}
	for index, line := range summaryLines {
		if err := invoicePDFText(pdf, left, 580+float64(index)*18, contentWidth, 18, 11, 30, 41, 59, line); err != nil {
			return false, err
		}
	}
	if err := addInvoicePDFFooter(pdf, generatedAt); err != nil {
		return false, err
	}
	return notesFullyShown, nil
}

func addInvoicePDFNotes(pdf *gopdf.GoPdf, notes string, generatedAt time.Time, required bool) error {
	notes = sanitizeInvoicePDFNotes(notes)
	if notes == "" || !required {
		return nil
	}
	if err := pdf.SetFont(invoicePDFFontFamily, "", 10.5); err != nil {
		return err
	}
	lines := make([]string, 0)
	for _, paragraph := range strings.Split(notes, "\n") {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}
		wrapped, err := pdf.SplitText(paragraph, gopdf.PageSizeA4.W-104)
		if err != nil {
			return fmt.Errorf("wrap invoice PDF notes: %w", err)
		}
		lines = append(lines, wrapped...)
	}
	const (
		left       = 52.0
		firstY     = 110.0
		lineHeight = 18.0
		lastY      = 758.0
	)
	y := lastY + 1
	page := 0
	for _, line := range lines {
		if y > lastY {
			page++
			pdf.AddPage()
			pdf.SetFillColor(17, 24, 39)
			pdf.RectFromUpperLeftWithStyle(0, 0, gopdf.PageSizeA4.W, 72, "F")
			if err := invoicePDFText(pdf, left, 26, gopdf.PageSizeA4.W-104, 28, 18, 248, 250, 252, fmt.Sprintf("备注 / NOTES · %d", page)); err != nil {
				return err
			}
			y = firstY
		}
		if line != "" {
			if err := invoicePDFText(pdf, left, y, gopdf.PageSizeA4.W-104, lineHeight, 10.5, 30, 41, 59, line); err != nil {
				return err
			}
		}
		y += lineHeight
		if y > lastY {
			if err := addInvoicePDFFooter(pdf, generatedAt); err != nil {
				return err
			}
		}
	}
	if y <= lastY {
		return addInvoicePDFFooter(pdf, generatedAt)
	}
	return nil
}

func addInvoicePDFFooter(pdf *gopdf.GoPdf, generatedAt time.Time) error {
	pdf.SetStrokeColor(226, 232, 240)
	pdf.SetLineWidth(0.7)
	pdf.Line(52, 782, gopdf.PageSizeA4.W-52, 782)
	disclaimer := "由 opc-workspace 本地生成的业务账单/记录，不代表税务电子发票"
	if err := invoicePDFText(pdf, 52, 792, gopdf.PageSizeA4.W-104, 14, 8.5, 100, 116, 139, disclaimer); err != nil {
		return err
	}
	generated := "生成时间 " + generatedAt.UTC().Format("2006-01-02 15:04:05 UTC")
	return invoicePDFCell(pdf, 52, 810, gopdf.PageSizeA4.W-104, 12, 7.5, 148, 163, 184, generated, gopdf.Right|gopdf.Middle)
}

func invoicePDFText(pdf *gopdf.GoPdf, x, y, width, height, size float64, red, green, blue uint8, text string) error {
	return invoicePDFCell(pdf, x, y, width, height, size, red, green, blue, text, gopdf.Left|gopdf.Top)
}

func invoicePDFCell(pdf *gopdf.GoPdf, x, y, width, height, size float64, red, green, blue uint8, text string, align int) error {
	if err := pdf.SetFont(invoicePDFFontFamily, "", size); err != nil {
		return err
	}
	pdf.SetTextColor(red, green, blue)
	pdf.SetXY(x, y)
	fitted, err := fitInvoicePDFSingleLine(pdf, sanitizeInvoicePDFSingleLine(text), width)
	if err != nil {
		return err
	}
	return pdf.CellWithOption(&gopdf.Rect{W: width, H: height}, fitted, gopdf.CellOption{Align: align})
}

func invoicePDFStatusLabel(status string) string {
	labels := map[string]string{
		"draft": "草稿", "sent": "已发送", "viewed": "已查看", "paid": "已付款", "overdue": "已逾期",
	}
	if label, ok := labels[status]; ok {
		return label
	}
	return status
}

func formatInvoiceMinorUnits(value int64) string {
	return fmt.Sprintf("%d.%02d", value/100, value%100)
}

func fitInvoicePDFSingleLine(pdf *gopdf.GoPdf, value string, width float64) (string, error) {
	measured, err := pdf.MeasureTextWidth(value)
	if err != nil {
		return "", err
	}
	if measured <= width {
		return value, nil
	}
	const ellipsis = "…"
	runes := []rune(value)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + ellipsis
		measured, err = pdf.MeasureTextWidth(candidate)
		if err != nil {
			return "", err
		}
		if measured <= width {
			return candidate, nil
		}
	}
	return ellipsis, nil
}

func sanitizeInvoicePDFSingleLine(value string) string {
	value = strings.Map(func(character rune) rune {
		if character == '\n' || character == '\r' || character == '\t' {
			return ' '
		}
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, strings.TrimSpace(value))
	return strings.Join(strings.Fields(value), " ")
}

func sanitizeInvoicePDFNotes(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.Map(func(character rune) rune {
		if character == '\n' {
			return character
		}
		if character == '\t' {
			return ' '
		}
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
	return strings.TrimSpace(value)
}
