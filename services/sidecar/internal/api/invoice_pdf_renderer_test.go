package api

import (
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"github.com/signintech/gopdf"
)

func newInvoicePDFDocumentForTest(t *testing.T) *gopdf.GoPdf {
	t.Helper()
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{Unit: gopdf.UnitPT, PageSize: *gopdf.PageSizeA4})
	if err := pdf.AddTTFFontData(invoicePDFFontFamily, invoicePDFNotoSansSC); err != nil {
		t.Fatalf("load invoice PDF test font: %v", err)
	}
	return pdf
}

func TestInvoicePDFSingleLineFitUsesMeasuredWidthAndSanitizesControls(t *testing.T) {
	pdf := newInvoicePDFDocumentForTest(t)
	pdf.AddPage()
	if err := pdf.SetFont(invoicePDFFontFamily, "", 11); err != nil {
		t.Fatalf("set invoice PDF test font: %v", err)
	}
	raw := strings.Repeat("超长客户名称 VeryLongClientName ", 20) + "\t\r\x00尾部"
	sanitized := sanitizeInvoicePDFSingleLine(raw)
	if strings.ContainsAny(sanitized, "\t\r\n\x00") || !strings.Contains(sanitized, "尾部") {
		t.Fatalf("sanitized invoice PDF text = %q", sanitized)
	}
	fitted, err := fitInvoicePDFSingleLine(pdf, sanitized, 180)
	if err != nil {
		t.Fatalf("fit invoice PDF text: %v", err)
	}
	width, err := pdf.MeasureTextWidth(fitted)
	if err != nil || width > 180 || !strings.HasSuffix(fitted, "…") {
		t.Fatalf("fitted invoice PDF text width=%f value=%q err=%v", width, fitted, err)
	}
	if got := sanitizeInvoicePDFNotes("第一行\r第二行\t字段\x00尾部"); got != "第一行\n第二行 字段尾部" {
		t.Fatalf("sanitized invoice PDF notes = %q", got)
	}
}

func TestInvoicePDFLongNotesAddContinuationPagesWhileShortNotesDoNot(t *testing.T) {
	longNotes := strings.Repeat("这是需要完整保留的长中文备注 mixed English words and numbers 2026. ", 80)
	invoice := invoiceRow{Invoice: models.Invoice{
		InvoiceNumber: strings.Repeat("INV-LONG-", 50), ClientID: "client", AmountMinor: 128045,
		Currency: "CNY", Status: "draft", IssueDate: "2026-08-29", DueDate: "2026-09-29", Notes: longNotes,
	}, ClientName: strings.Repeat("极长客户名称 Long Client Name ", 30)}
	project := strings.Repeat("极长项目名称 Long Project Name ", 30)
	invoice.ProjectName = &project

	pdf := newInvoicePDFDocumentForTest(t)
	fullyShown, err := addInvoicePDFSummaryPage(pdf, invoice, invoicePDFTestNow)
	if err != nil {
		t.Fatalf("render long invoice PDF summary: %v", err)
	}
	if fullyShown {
		t.Fatal("long invoice PDF notes were incorrectly marked fully shown")
	}
	if err := addInvoicePDFNotes(pdf, longNotes, invoicePDFTestNow, !fullyShown); err != nil {
		t.Fatalf("render long invoice PDF continuation: %v", err)
	}
	if pdf.GetNumberOfPages() < 2 {
		t.Fatalf("long invoice PDF page count = %d, want at least 2", pdf.GetNumberOfPages())
	}

	shortPDF := newInvoicePDFDocumentForTest(t)
	invoice.Notes = "两行以内的简短备注"
	fullyShown, err = addInvoicePDFSummaryPage(shortPDF, invoice, invoicePDFTestNow)
	if err != nil {
		t.Fatalf("render short invoice PDF summary: %v", err)
	}
	if !fullyShown {
		t.Fatal("short invoice PDF notes were not marked fully shown")
	}
	if err := addInvoicePDFNotes(shortPDF, invoice.Notes, invoicePDFTestNow, !fullyShown); err != nil {
		t.Fatalf("render short invoice PDF continuation: %v", err)
	}
	if shortPDF.GetNumberOfPages() != 1 {
		t.Fatalf("short invoice PDF page count = %d, want 1", shortPDF.GetNumberOfPages())
	}
}
