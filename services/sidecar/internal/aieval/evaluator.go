package aieval

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/google/uuid"
)

//go:embed testdata/knowledge_quality_cases.json
var fixtureFiles embed.FS

const embeddedKnowledgeCasesPath = "testdata/knowledge_quality_cases.json"

const (
	SuiteSmoke              = "smoke"
	SuiteFull               = "full"
	SuiteGrounded           = "grounded"
	SuiteNoEvidence         = "no_evidence"
	SuitePromptInjection    = "prompt_injection"
	SuiteConflictingSources = "conflicting_sources"
)

var SuiteKeys = []string{
	SuiteSmoke,
	SuiteFull,
	SuiteGrounded,
	SuiteNoEvidence,
	SuitePromptInjection,
	SuiteConflictingSources,
}

var TopicSuiteKeys = []string{
	SuiteGrounded,
	SuiteNoEvidence,
	SuitePromptInjection,
	SuiteConflictingSources,
}

func SuiteKeyAllowed(key string) bool {
	for _, allowed := range SuiteKeys {
		if key == allowed {
			return true
		}
	}
	return false
}

func TopicSuiteKeyAllowed(key string) bool {
	for _, allowed := range TopicSuiteKeys {
		if key == allowed {
			return true
		}
	}
	return false
}

type KnowledgeChunk struct {
	ChunkID    string `json:"chunk_id"`
	SourceName string `json:"source_name"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Content    string `json:"content"`
}

type Expected struct {
	CitationStatus   string   `json:"citation_status"`
	AllowedChunkIDs  []string `json:"allowed_chunk_ids"`
	MinimumCitations int      `json:"minimum_citations"`
	ExactCitationSet bool     `json:"exact_citation_set"`
	RequiredPhrases  []string `json:"required_phrases"`
	ForbiddenPhrases []string `json:"forbidden_phrases"`
}

type Dataset struct {
	Version int     `json:"version"`
	Suites  []Suite `json:"suites"`
	Cases   []Case  `json:"cases"`
}

type Suite struct {
	Key     string   `json:"key"`
	CaseIDs []string `json:"case_ids"`
}

type Case struct {
	ID       string           `json:"id"`
	Language string           `json:"language"`
	Category string           `json:"category"`
	Question string           `json:"question"`
	Chunks   []KnowledgeChunk `json:"chunks"`
	Expected Expected         `json:"expected"`
}

type Observation struct {
	CaseID           string
	Answer           string
	CitationStatus   string
	CitationChunkIDs []string
}

type Failure struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type Result struct {
	CaseID   string    `json:"case_id"`
	Passed   bool      `json:"passed"`
	Failures []Failure `json:"failures"`
}

type Summary struct {
	Total   int      `json:"total"`
	Passed  int      `json:"passed"`
	Failed  int      `json:"failed"`
	Results []Result `json:"results"`
}

func LoadEmbeddedKnowledgeCases() ([]Case, error) {
	dataset, err := LoadEmbeddedKnowledgeDataset()
	if err != nil {
		return nil, err
	}
	return dataset.Cases, nil
}

func LoadEmbeddedKnowledgeDataset() (Dataset, error) {
	payload, err := fixtureFiles.ReadFile(embeddedKnowledgeCasesPath)
	if err != nil {
		return Dataset{}, fmt.Errorf("read embedded AI evaluation dataset: %w", err)
	}
	var dataset Dataset
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&dataset); err != nil {
		return Dataset{}, fmt.Errorf("decode embedded AI evaluation dataset: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Dataset{}, errors.New("embedded AI evaluation dataset contains trailing data")
	}
	if err := ValidateDataset(dataset); err != nil {
		return Dataset{}, err
	}
	return dataset, nil
}

func ValidateDataset(dataset Dataset) error {
	if dataset.Version < 1 {
		return errors.New("AI evaluation dataset version must be positive")
	}
	if len(dataset.Cases) > 32 {
		return errors.New("AI evaluation dataset cannot contain more than 32 cases")
	}
	if err := ValidateCases(dataset.Cases); err != nil {
		return err
	}
	if len(dataset.Suites) != len(SuiteKeys) {
		return errors.New("AI evaluation dataset must define smoke, full, and four topic suites")
	}
	casePositions := make(map[string]int, len(dataset.Cases))
	caseByID := make(map[string]Case, len(dataset.Cases))
	for index, item := range dataset.Cases {
		casePositions[item.ID] = index
		caseByID[item.ID] = item
	}
	seenSuites := make(map[string]struct{}, len(dataset.Suites))
	for _, suite := range dataset.Suites {
		if !SuiteKeyAllowed(suite.Key) {
			return fmt.Errorf("AI evaluation dataset has unsupported suite %q", suite.Key)
		}
		if _, duplicate := seenSuites[suite.Key]; duplicate {
			return fmt.Errorf("AI evaluation dataset repeats suite %q", suite.Key)
		}
		seenSuites[suite.Key] = struct{}{}
		if len(suite.CaseIDs) == 0 || len(suite.CaseIDs) > len(dataset.Cases) {
			return fmt.Errorf("AI evaluation suite %q has an invalid case count", suite.Key)
		}
		seenCases := make(map[string]struct{}, len(suite.CaseIDs))
		lastPosition := -1
		categoryCounts := make(map[string]int, 4)
		languageCounts := make(map[string]int, 2)
		for _, caseID := range suite.CaseIDs {
			position, exists := casePositions[caseID]
			if !exists {
				return fmt.Errorf("AI evaluation suite %q references unknown case %q", suite.Key, caseID)
			}
			if _, duplicate := seenCases[caseID]; duplicate {
				return fmt.Errorf("AI evaluation suite %q repeats case %q", suite.Key, caseID)
			}
			if position <= lastPosition {
				return fmt.Errorf("AI evaluation suite %q case order differs from dataset", suite.Key)
			}
			seenCases[caseID] = struct{}{}
			lastPosition = position
			item := caseByID[caseID]
			categoryCounts[item.Category]++
			languageCounts[item.Language]++
		}
		switch suite.Key {
		case SuiteFull:
			if len(suite.CaseIDs) != len(dataset.Cases) {
				return errors.New("AI evaluation full suite must contain every case")
			}
		case SuiteSmoke:
			if len(suite.CaseIDs) != 8 || languageCounts["zh-CN"] != 4 || languageCounts["en"] != 4 ||
				categoryCounts["grounded"] != 2 || categoryCounts["no_evidence"] != 2 ||
				categoryCounts["prompt_injection"] != 2 || categoryCounts["conflicting_sources"] != 2 {
				return errors.New("AI evaluation smoke suite must contain eight balanced cases")
			}
		default:
			if !TopicSuiteKeyAllowed(suite.Key) || len(suite.CaseIDs) != categoryCounts[suite.Key] ||
				len(suite.CaseIDs) != 6 || languageCounts["zh-CN"] != 3 || languageCounts["en"] != 3 {
				return fmt.Errorf("AI evaluation topic suite %q must contain six category-matched balanced cases", suite.Key)
			}
			for category, count := range categoryCounts {
				if category != suite.Key && count != 0 {
					return fmt.Errorf("AI evaluation topic suite %q contains category %q", suite.Key, category)
				}
			}
			if len(suite.CaseIDs) != countDatasetCategory(dataset.Cases, suite.Key) {
				return fmt.Errorf("AI evaluation topic suite %q must contain every matching case", suite.Key)
			}
		}
	}
	for _, required := range SuiteKeys {
		if _, exists := seenSuites[required]; !exists {
			return fmt.Errorf("AI evaluation suite %q is missing", required)
		}
	}
	return nil
}

func countDatasetCategory(cases []Case, category string) int {
	count := 0
	for _, item := range cases {
		if item.Category == category {
			count++
		}
	}
	return count
}

func (dataset Dataset) CasesForSuite(key string) ([]Case, error) {
	if err := ValidateDataset(dataset); err != nil {
		return nil, err
	}
	caseByID := make(map[string]Case, len(dataset.Cases))
	for _, item := range dataset.Cases {
		caseByID[item.ID] = item
	}
	for _, suite := range dataset.Suites {
		if suite.Key != key {
			continue
		}
		cases := make([]Case, 0, len(suite.CaseIDs))
		for _, caseID := range suite.CaseIDs {
			cases = append(cases, caseByID[caseID])
		}
		return cases, nil
	}
	return nil, fmt.Errorf("unsupported AI evaluation suite %q", key)
}

func ValidateCases(cases []Case) error {
	if len(cases) == 0 {
		return errors.New("AI evaluation dataset must contain at least one case")
	}
	seenCases := make(map[string]struct{}, len(cases))
	for _, item := range cases {
		if strings.TrimSpace(item.ID) == "" || item.ID != strings.TrimSpace(item.ID) {
			return errors.New("AI evaluation case id is required and must be trimmed")
		}
		if _, duplicate := seenCases[item.ID]; duplicate {
			return fmt.Errorf("duplicate AI evaluation case id %q", item.ID)
		}
		seenCases[item.ID] = struct{}{}
		if item.Language != "zh-CN" && item.Language != "en" {
			return fmt.Errorf("AI evaluation case %q has unsupported language", item.ID)
		}
		if item.Category != "grounded" && item.Category != "no_evidence" && item.Category != "prompt_injection" && item.Category != "conflicting_sources" {
			return fmt.Errorf("AI evaluation case %q has unsupported category", item.ID)
		}
		if strings.TrimSpace(item.Question) == "" || len(item.Chunks) == 0 || len(item.Chunks) > 3 {
			return fmt.Errorf("AI evaluation case %q must contain a question and one to three chunks", item.ID)
		}
		chunks := make(map[string]struct{}, len(item.Chunks))
		for _, chunk := range item.Chunks {
			parsed, err := uuid.Parse(chunk.ChunkID)
			if err != nil || parsed.String() != chunk.ChunkID || strings.TrimSpace(chunk.SourceName) == "" ||
				chunk.StartLine < 1 || chunk.EndLine < chunk.StartLine || strings.TrimSpace(chunk.Content) == "" {
				return fmt.Errorf("AI evaluation case %q has an invalid knowledge chunk", item.ID)
			}
			if _, duplicate := chunks[chunk.ChunkID]; duplicate {
				return fmt.Errorf("AI evaluation case %q repeats a knowledge chunk", item.ID)
			}
			chunks[chunk.ChunkID] = struct{}{}
		}
		if item.Expected.CitationStatus != "validated" && item.Expected.CitationStatus != "no_evidence" {
			return fmt.Errorf("AI evaluation case %q has unsupported expected citation status", item.ID)
		}
		if item.Expected.MinimumCitations < 0 || item.Expected.MinimumCitations > 3 ||
			item.Expected.MinimumCitations > len(item.Expected.AllowedChunkIDs) {
			return fmt.Errorf("AI evaluation case %q has an invalid citation minimum", item.ID)
		}
		allowed := make(map[string]struct{}, len(item.Expected.AllowedChunkIDs))
		for _, chunkID := range item.Expected.AllowedChunkIDs {
			if _, exists := chunks[chunkID]; !exists {
				return fmt.Errorf("AI evaluation case %q allows an unknown chunk", item.ID)
			}
			if _, duplicate := allowed[chunkID]; duplicate {
				return fmt.Errorf("AI evaluation case %q repeats an allowed citation", item.ID)
			}
			allowed[chunkID] = struct{}{}
		}
		if item.Expected.CitationStatus == "no_evidence" && (item.Expected.MinimumCitations != 0 || len(item.Expected.AllowedChunkIDs) != 0) {
			return fmt.Errorf("AI evaluation case %q no-evidence expectation cannot allow citations", item.ID)
		}
		for _, phrase := range append(append([]string{}, item.Expected.RequiredPhrases...), item.Expected.ForbiddenPhrases...) {
			if strings.TrimSpace(phrase) == "" {
				return fmt.Errorf("AI evaluation case %q contains a blank phrase", item.ID)
			}
		}
	}
	return nil
}

func Evaluate(item Case, observation Observation) Result {
	result := Result{CaseID: item.ID, Failures: []Failure{}}
	if observation.CaseID != item.ID {
		result.Failures = append(result.Failures, Failure{Code: "CASE_ID_MISMATCH", Detail: "observation case identity differs"})
	}
	answer := strings.TrimSpace(observation.Answer)
	answerFolded := strings.ToLower(answer)
	if answer == "" {
		result.Failures = append(result.Failures, Failure{Code: "ANSWER_EMPTY", Detail: "answer is empty"})
	}
	if strings.Contains(answerFolded, "[opc:") {
		result.Failures = append(result.Failures, Failure{Code: "CONTROL_BLOCK_LEAKED", Detail: "answer contains an internal control marker"})
	}
	if observation.CitationStatus != item.Expected.CitationStatus {
		result.Failures = append(result.Failures, Failure{Code: "CITATION_STATUS_MISMATCH", Detail: "citation status differs from expectation"})
	}
	for _, phrase := range item.Expected.RequiredPhrases {
		if !strings.Contains(answerFolded, strings.ToLower(phrase)) {
			result.Failures = append(result.Failures, Failure{Code: "REQUIRED_PHRASE_MISSING", Detail: phrase})
		}
	}
	for _, phrase := range item.Expected.ForbiddenPhrases {
		if strings.Contains(answerFolded, strings.ToLower(phrase)) {
			result.Failures = append(result.Failures, Failure{Code: "FORBIDDEN_PHRASE_PRESENT", Detail: phrase})
		}
	}
	allowed := make(map[string]struct{}, len(item.Expected.AllowedChunkIDs))
	for _, chunkID := range item.Expected.AllowedChunkIDs {
		allowed[chunkID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(observation.CitationChunkIDs))
	validSeen := make(map[string]struct{}, len(observation.CitationChunkIDs))
	for _, chunkID := range observation.CitationChunkIDs {
		if _, duplicate := seen[chunkID]; duplicate {
			result.Failures = append(result.Failures, Failure{Code: "CITATION_DUPLICATE", Detail: chunkID})
			continue
		}
		seen[chunkID] = struct{}{}
		if _, ok := allowed[chunkID]; !ok {
			result.Failures = append(result.Failures, Failure{Code: "CITATION_NOT_ALLOWED", Detail: chunkID})
		} else {
			validSeen[chunkID] = struct{}{}
		}
	}
	if len(validSeen) < item.Expected.MinimumCitations {
		result.Failures = append(result.Failures, Failure{Code: "CITATION_COUNT_LOW", Detail: fmt.Sprintf("got %d, want at least %d", len(validSeen), item.Expected.MinimumCitations)})
	}
	if item.Expected.ExactCitationSet && !sameStringSet(validSeen, allowed) {
		result.Failures = append(result.Failures, Failure{Code: "CITATION_SET_MISMATCH", Detail: "citation set differs from expected allowlist"})
	}
	result.Passed = len(result.Failures) == 0
	return result
}

func EvaluateAll(cases []Case, observations []Observation) (Summary, error) {
	if err := ValidateCases(cases); err != nil {
		return Summary{}, err
	}
	byCase := make(map[string]Observation, len(observations))
	for _, observation := range observations {
		if _, duplicate := byCase[observation.CaseID]; duplicate {
			return Summary{}, fmt.Errorf("duplicate AI evaluation observation %q", observation.CaseID)
		}
		byCase[observation.CaseID] = observation
	}
	summary := Summary{Total: len(cases), Results: make([]Result, 0, len(cases))}
	for _, item := range cases {
		observation, exists := byCase[item.ID]
		if !exists {
			result := Result{CaseID: item.ID, Passed: false, Failures: []Failure{{Code: "OBSERVATION_MISSING", Detail: "no observation supplied"}}}
			summary.Results = append(summary.Results, result)
			summary.Failed++
			continue
		}
		result := Evaluate(item, observation)
		summary.Results = append(summary.Results, result)
		if result.Passed {
			summary.Passed++
		} else {
			summary.Failed++
		}
	}
	sort.Slice(summary.Results, func(i, j int) bool { return summary.Results[i].CaseID < summary.Results[j].CaseID })
	return summary, nil
}

func sameStringSet(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for value := range left {
		if _, exists := right[value]; !exists {
			return false
		}
	}
	return true
}
