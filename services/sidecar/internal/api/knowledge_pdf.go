package api

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	ledpdf "github.com/ledongthuc/pdf"
)

const (
	// knowledgePDFMaxTextRunes matches the knowledge_documents content_text
	// CHECK budget so a document full of PDF text cannot exceed SQLite limits.
	knowledgePDFMaxTextRunes = 16_777_216
	// knowledgePDFMaxOperations bounds CPU when a content stream degenerates
	// into an enormous number of no-op or whitespace operators.
	knowledgePDFMaxOperations = 5_000_000
	// knowledgePDFMaxPages bounds the page walk; a 16 MiB source cannot carry
	// more real text pages than this.
	knowledgePDFMaxPages = 20_000
)

var (
	errKnowledgePDFTextBudget      = errors.New("knowledge PDF text budget exhausted")
	errKnowledgePDFOperationBudget = errors.New("knowledge PDF operation budget exhausted")
)

type knowledgePDFPageLocation struct {
	Number    int
	StartLine int
	EndLine   int
}

type knowledgePDFExtraction struct {
	Text  string
	Pages []knowledgePDFPageLocation
}

// extractKnowledgePDF turns the managed PDF copy into normalized text plus a
// page map. Only user-selected files reach this path; extraction never opens
// network connections and never executes embedded scripts.
func extractKnowledgePDF(content []byte) (knowledgePDFExtraction, error) {
	document, err := ledpdf.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		if errors.Is(err, ledpdf.ErrInvalidPassword) {
			return knowledgePDFExtraction{}, newProjectRequestError(
				http.StatusUnprocessableEntity, "KNOWLEDGE_PDF_ENCRYPTED",
				"The PDF document is password protected and cannot be indexed",
			)
		}
		return knowledgePDFExtraction{}, newProjectRequestError(
			http.StatusUnprocessableEntity, "KNOWLEDGE_PDF_INVALID",
			"The PDF document could not be read as a valid PDF",
		)
	}
	totalPages := document.NumPage()
	if totalPages < 0 || totalPages > knowledgePDFMaxPages {
		return knowledgePDFExtraction{}, newProjectRequestError(
			http.StatusUnprocessableEntity, "KNOWLEDGE_PDF_TOO_MANY_PAGES",
			"The PDF document has too many pages to index",
		)
	}
	textBudget := 0
	var result knowledgePDFExtraction
	var combined strings.Builder
	lineCursor := 1
	for number := 1; number <= totalPages; number++ {
		pageText, err := knowledgePDFPageText(document, number, &textBudget)
		if err != nil {
			return knowledgePDFExtraction{}, knowledgePDFExtractionError(err)
		}
		pageText = strings.ReplaceAll(pageText, "\r\n", "\n")
		pageText = strings.ReplaceAll(pageText, "\r", "\n")
		if strings.TrimSpace(pageText) == "" {
			continue
		}
		lines := strings.Count(pageText, "\n") + 1
		result.Pages = append(result.Pages, knowledgePDFPageLocation{
			Number: number, StartLine: lineCursor, EndLine: lineCursor + lines - 1,
		})
		combined.WriteString(pageText)
		combined.WriteString("\n")
		lineCursor += lines
	}
	if len(result.Pages) == 0 {
		return knowledgePDFExtraction{}, newProjectRequestError(
			http.StatusUnprocessableEntity, "KNOWLEDGE_PDF_NO_TEXT",
			"The PDF document does not contain extractable text on any page",
		)
	}
	result.Text = strings.TrimSuffix(combined.String(), "\n")
	return result, nil
}

func knowledgePDFExtractionError(err error) error {
	var requestErr *projectRequestError
	if errors.As(err, &requestErr) {
		return err
	}
	switch {
	case errors.Is(err, errKnowledgePDFTextBudget):
		return newProjectRequestError(
			http.StatusUnprocessableEntity, "KNOWLEDGE_PDF_TEXT_TOO_LARGE",
			"The PDF document contains more text than the knowledge base can index",
		)
	case errors.Is(err, errKnowledgePDFOperationBudget):
		return newProjectRequestError(
			http.StatusUnprocessableEntity, "KNOWLEDGE_PDF_TOO_COMPLEX",
			"The PDF document is too complex to extract text from",
		)
	default:
		return newProjectRequestError(
			http.StatusUnprocessableEntity, "KNOWLEDGE_PDF_INVALID",
			"The PDF document could not be read as a valid PDF",
		)
	}
}

type knowledgePDFNopEncoding struct{}

func (knowledgePDFNopEncoding) Decode(raw string) string { return raw }

// knowledgePDFPageText extracts one page with the same text-operator walk as
// the upstream plain-text reader, wrapped with text and operation budgets.
// The upstream library reports malformed structures by panicking, so every
// panic is recovered into a stable error at this boundary.
func knowledgePDFPageText(document *ledpdf.Reader, number int, textBudget *int) (text string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			text = ""
			if budgetErr, ok := recovered.(error); ok &&
				(errors.Is(budgetErr, errKnowledgePDFTextBudget) || errors.Is(budgetErr, errKnowledgePDFOperationBudget)) {
				err = budgetErr
				return
			}
			err = fmt.Errorf("pdf page %d extraction failed: %v", number, recovered)
		}
	}()
	page := document.Page(number)
	if page.V.IsNull() {
		return "", nil
	}
	contents := page.V.Key("Contents")
	if contents.Kind() == ledpdf.Null {
		return "", nil
	}
	fonts := make(map[string]*ledpdf.Font)
	for _, name := range page.Fonts() {
		if _, ok := fonts[name]; !ok {
			font := page.Font(name)
			fonts[name] = &font
		}
	}
	var builder strings.Builder
	write := func(value string) {
		*textBudget += utf8.RuneCountInString(value)
		if *textBudget > knowledgePDFMaxTextRunes {
			panic(errKnowledgePDFTextBudget)
		}
		builder.WriteString(value)
	}
	encoding := ledpdf.TextEncoding(knowledgePDFNopEncoding{})
	encoded := func(raw string) {
		write(knowledgePDFDecodeText(encoding, raw))
	}
	operations := 0
	ledpdf.Interpret(contents, func(stack *ledpdf.Stack, op string) {
		operations++
		if operations > knowledgePDFMaxOperations {
			panic(errKnowledgePDFOperationBudget)
		}
		args := knowledgePDFOperatorArguments(stack)
		switch op {
		default:
			return
		case "BT":
			write("\n")
		case "T*":
			encoded("\n")
		case "Tf":
			if len(args) != 2 {
				panic("bad Tf operator")
			}
			if font, ok := fonts[args[0].Name()]; ok {
				encoding = font.Encoder()
			} else {
				encoding = knowledgePDFNopEncoding{}
			}
		case "\"", "'", "Tj":
			if len(args) != 1 {
				panic("bad text showing operator")
			}
			encoded(args[0].RawString())
		case "TJ":
			if len(args) != 1 {
				panic("bad TJ operator")
			}
			value := args[0]
			for i := 0; i < value.Len(); i++ {
				item := value.Index(i)
				if item.Kind() == ledpdf.String {
					encoded(item.RawString())
				}
			}
		}
	})
	return builder.String(), nil
}

func knowledgePDFDecodeText(encoding ledpdf.TextEncoding, raw string) string {
	var builder strings.Builder
	for _, ch := range encoding.Decode(raw) {
		builder.WriteRune(ch)
	}
	return builder.String()
}

func knowledgePDFOperatorArguments(stack *ledpdf.Stack) []ledpdf.Value {
	count := stack.Len()
	args := make([]ledpdf.Value, count)
	for i := count - 1; i >= 0; i-- {
		args[i] = stack.Pop()
	}
	return args
}
