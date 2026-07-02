package assist

import "testing"

func TestParseLLMOutputAcceptsMarkdownFencedJSON(t *testing.T) {
	out, ok := parseLLMOutput("```json\n{\"summary\":\"ok\",\"next_steps\":[\"review\"]}\n```")
	if !ok {
		t.Fatal("expected fenced json to parse")
	}
	if out["summary"] != "ok" {
		t.Fatalf("unexpected summary: %+v", out)
	}
}

func TestParseLLMOutputExtractsJSONFromSurroundingText(t *testing.T) {
	out, ok := parseLLMOutput("Here is the result:\n{\"summary\":\"ok\"}\nThanks")
	if !ok {
		t.Fatal("expected embedded json to parse")
	}
	if out["summary"] != "ok" {
		t.Fatalf("unexpected summary: %+v", out)
	}
}
