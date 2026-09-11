package aieval

import "testing"

func TestInvoiceFactPolarityAndParaphrase(t *testing.T) {
	cases, err := LoadEmbeddedKnowledgeCases()
	if err != nil {
		t.Fatal(err)
	}
	var item Case
	for _, candidate := range cases {
		if candidate.ID == "zh_invoice_grounded" {
			item = candidate
		}
	}
	for _, test := range []struct {
		answer string
		want   bool
	}{
		{"付款完成后，不需要归档，直接丢弃发票和付款凭证。", false},
		{"收款后应保存账单以及支付证明，便于日后查阅。", true},
		{"发票和付款凭证需要归档，但是也可以丢掉这些资料。", false},
		{"不要归档发票及付款凭证。", false},
		{"付款之后，将发票及付款凭证存档备查。", true},
	} {
		result := Evaluate(item, Observation{CaseID: item.ID, Answer: test.answer, CitationStatus: item.Expected.CitationStatus, CitationChunkIDs: item.Expected.AllowedChunkIDs})
		if result.Passed != test.want {
			t.Errorf("answer=%q passed=%v want=%v failures=%v", test.answer, result.Passed, test.want, result.Failures)
		}
	}
}

func TestBoundedFactRulesAreDomainIndependent(t *testing.T) {
	rules := []FactCheck{{ID: "keep", Aliases: []string{"archive", "retain", "store"}, Opposed: []string{"discard", "destroy"}, Affirmative: true}}
	for _, test := range []struct {
		text string
		want bool
	}{
		{"Retain the signed document.", true},
		{"Archive the signed document; never discard it.", true},
		{"Do not discard it. Archive the signed document.", true},
		{"Do not archive the document.", false},
		{"Archive the document, but you may destroy it.", false},
		{"The retailer has a storefront.", false},
		{"Preserve a copy.", false}, // Unknown paraphrases require human judgment.
	} {
		if got := len(evaluateFactChecks(rules, test.text)) == 0; got != test.want {
			t.Errorf("%q = %v, want %v", test.text, got, test.want)
		}
	}
	for _, text := range []string{"There is no conflict, human confirmation is required.", "These sources do not conflict, human confirmation is required.", "These sources conflict; these sources do not conflict."} {
		if failures := evaluateFactChecks([]FactCheck{{ID: "conflict", Aliases: []string{"conflict"}, Affirmative: true}}, text); len(failures) != 1 || failures[0].Code != "FACT_CONTRADICTED" {
			t.Errorf("%q: %#v", text, failures)
		}
	}
}

func TestV4AssessmentSeparatesEvidenceLayers(t *testing.T) {
	dataset, err := LoadEmbeddedKnowledgeDataset()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range dataset.Cases {
		if len(item.Expected.FactChecks) == 0 {
			t.Fatalf("v4 case has no fact rules: %s", item.ID)
		}
		result := Evaluate(item, Observation{CaseID: item.ID, Answer: "unknown", CitationStatus: "no_evidence"})
		if !result.Assessment.HumanReviewRequired || result.Assessment.Method != "deterministic_rules" || result.Assessment.FactChecks == 0 || result.Assessment.KeywordChecks != len(item.Expected.ForbiddenPhrases) {
			t.Fatalf("assessment=%#v", result.Assessment)
		}
	}
}
