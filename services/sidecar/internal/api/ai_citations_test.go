package api

import (
	"strings"
	"testing"
)

func aiCitationKnowledgeFixture() []aiKnowledgeContextSource {
	return []aiKnowledgeContextSource{
		{
			SourceID: "018f0000-0000-7000-8000-000000006101", SourceName: "guide.md", SourceVersion: 2,
			SourceType: "markdown", DocumentID: "018f0000-0000-7000-8000-000000006102",
			DocumentTitle: "Guide", DocumentVersion: 3, ChunkID: "018f0000-0000-7000-8000-000000006103",
			ChunkIndex: 0, StartChar: 0, EndChar: 20, StartLine: 2, EndLine: 4,
			Content: "private evidence content",
		},
		{
			SourceID: "018f0000-0000-7000-8000-000000006104", SourceName: "policy.txt", SourceVersion: 1,
			SourceType: "text", DocumentID: "018f0000-0000-7000-8000-000000006105",
			DocumentTitle: "Policy", DocumentVersion: 1, ChunkID: "018f0000-0000-7000-8000-000000006106",
			ChunkIndex: 2, StartChar: 80, EndChar: 120, StartLine: 8, EndLine: 9,
			Content: "second private evidence",
		},
	}
}

func TestValidateAIResponseCitationsAcceptsOnlyAllowedChunkIdentities(t *testing.T) {
	allowed := aiCitationKnowledgeFixture()
	content := "根据资料，交付需要付款凭证。\n\n[opc:citations]{\"chunk_ids\":[\"" + allowed[1].ChunkID + "\",\"" + allowed[0].ChunkID + "\"]}[/opc:citations]"
	cleaned, encoded, snapshot, err := validateAIResponseCitations(content, allowed)
	if err != nil {
		t.Fatalf("validate citations: %v", err)
	}
	if cleaned != "根据资料，交付需要付款凭证。" || encoded == nil || snapshot.Status != "validated" || len(snapshot.Items) != 2 {
		t.Fatalf("validated citations cleaned=%q snapshot=%#v encoded=%v", cleaned, snapshot, encoded)
	}
	if snapshot.Items[0].ChunkID != allowed[1].ChunkID || snapshot.Items[1].ChunkID != allowed[0].ChunkID {
		t.Fatalf("citation order=%#v", snapshot.Items)
	}
	if strings.Contains(*encoded, "private evidence") || strings.Contains(cleaned, "opc:citations") {
		t.Fatalf("citation snapshot leaked content or control block: %s / %s", *encoded, cleaned)
	}
	status, decoded, err := decodeAICitationSnapshot(encoded)
	if err != nil || status != "validated" || len(decoded) != 2 {
		t.Fatalf("decode citations status=%q items=%#v err=%v", status, decoded, err)
	}
}

func TestValidateAIResponseCitationsClassifiesNoEvidenceMissingAndInvalid(t *testing.T) {
	allowed := aiCitationKnowledgeFixture()
	cases := []struct {
		name    string
		content string
		status  string
	}{
		{name: "no evidence", content: `资料不足，无法可靠回答。[opc:citations]{"chunk_ids":[]}[/opc:citations]`, status: "no_evidence"},
		{name: "missing", content: "模型没有提供引用块", status: "missing"},
		{name: "malformed", content: `回答[opc:citations]{bad}[/opc:citations]`, status: "invalid"},
		{name: "unknown field", content: `回答[opc:citations]{"chunk_ids":[],"trusted":true}[/opc:citations]`, status: "invalid"},
		{name: "out of scope", content: `回答[opc:citations]{"chunk_ids":["018f0000-0000-7000-8000-000000006199"]}[/opc:citations]`, status: "invalid"},
		{name: "duplicate", content: `回答[opc:citations]{"chunk_ids":["` + allowed[0].ChunkID + `","` + allowed[0].ChunkID + `"]}[/opc:citations]`, status: "invalid"},
		{name: "open tail", content: `回答[opc:citations]{"chunk_ids":[`, status: "invalid"},
		{name: "multiple", content: `回答[opc:citations]{"chunk_ids":[]}[/opc:citations][opc:citations]{"chunk_ids":[]}[/opc:citations]`, status: "invalid"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cleaned, encoded, snapshot, err := validateAIResponseCitations(test.content, allowed)
			if err != nil || encoded == nil || snapshot.Status != test.status || len(snapshot.Items) != 0 {
				t.Fatalf("classification cleaned=%q snapshot=%#v encoded=%v err=%v", cleaned, snapshot, encoded, err)
			}
			if strings.Contains(cleaned, "opc:citations") {
				t.Fatalf("control block leaked: %q", cleaned)
			}
		})
	}
}

func TestValidateAIResponseCitationsStripsFabricatedBlockWithoutKnowledge(t *testing.T) {
	content := `普通回答[opc:citations]{"chunk_ids":["018f0000-0000-7000-8000-000000006199"]}[/opc:citations]`
	cleaned, encoded, snapshot, err := validateAIResponseCitations(content, nil)
	if err != nil || cleaned != "普通回答" || encoded != nil || snapshot.Status != "not_requested" {
		t.Fatalf("no-knowledge citation result cleaned=%q encoded=%v snapshot=%#v err=%v", cleaned, encoded, snapshot, err)
	}
	if got := stripAIControlBlocks("before\n" + content); got != "before\n普通回答" {
		t.Fatalf("history control stripping=%q", got)
	}
}

func TestDecodeAICitationSnapshotRejectsUnknownOrInconsistentStoredData(t *testing.T) {
	for _, value := range []string{
		`{"version":1,"status":"missing","items":[],"trusted":true}`,
		`{"version":1,"status":"validated","items":[]}`,
		`{"version":1,"status":"missing","items":[{"chunk_id":"018f0000-0000-7000-8000-000000006103"}]}`,
	} {
		if _, _, err := decodeAICitationSnapshot(&value); err == nil {
			t.Fatalf("invalid stored snapshot was accepted: %s", value)
		}
	}
}
