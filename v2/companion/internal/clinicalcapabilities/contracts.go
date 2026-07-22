package clinicalcapabilities

import "encoding/json"

const (
	RecordsSearchKey = "clinical.records.search"
	TimelineBuildKey = "clinical.timeline.build"
)

func SearchInputSchema() map[string]any   { return mustSchema(searchInputSchemaJSON) }
func SearchOutputSchema() map[string]any  { return mustSchema(searchOutputSchemaJSON) }
func TimelineInputSchema() map[string]any { return mustSchema(timelineInputSchemaJSON) }
func TimelineOutputSchema() map[string]any {
	return mustSchema(timelineOutputSchemaJSON)
}

func mustSchema(raw string) map[string]any {
	var schema map[string]any
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		panic(err)
	}
	return schema
}

const canonicalReferenceSchemaJSON = `{
  "type":"object","additionalProperties":false,
  "required":["document_id","source_version","sha256","locator"],
  "properties":{
    "document_id":{"type":"string","minLength":1,"maxLength":200},
    "source_version":{"type":"string","minLength":1,"maxLength":200},
    "sha256":{"type":"string","minLength":64,"maxLength":64},
    "locator":{"type":"object"}
  }
}`

const searchInputSchemaJSON = `{
  "type":"object","additionalProperties":false,"required":["query"],
  "properties":{
    "query":{"type":"string","minLength":1,"maxLength":4000},
    "limit":{"type":"integer","minimum":1,"maximum":50},
    "cursor":{"type":"string","maxLength":4096}
  }
}`

const searchOutputSchemaJSON = `{
  "type":"object","additionalProperties":false,
  "required":["schema_version","status","query","matches","next_cursor","truncated","warnings"],
  "properties":{
    "schema_version":{"type":"string","enum":["clinical.records.search.v1"]},
    "status":{"type":"string","enum":["completed","partial"]},
    "query":{"type":"string"},
    "matches":{"type":"array","maxItems":50,"items":{
      "type":"object","additionalProperties":false,"required":["excerpt","score","reference"],
      "properties":{
        "excerpt":{"type":"string","maxLength":1200},
        "score":{"type":"number"},
        "reference":` + canonicalReferenceSchemaJSON + `
      }
    }},
    "next_cursor":{"type":"string"},
    "truncated":{"type":"boolean"},
    "warnings":{"type":"array","items":{"type":"string"}}
  }
}`

const timelineInputSchemaJSON = `{
  "type":"object","additionalProperties":false,
  "properties":{
    "date_from":{"type":"string","maxLength":64},
    "date_to":{"type":"string","maxLength":64},
    "order":{"type":"string","enum":["asc","desc"]},
    "max_events":{"type":"integer","minimum":1,"maximum":200},
    "focus":{"type":"string","maxLength":2000}
  }
}`

const timelineOutputSchemaJSON = `{
  "type":"object","additionalProperties":false,
  "required":["schema_version","status","scope","events","coverage","warnings"],
  "properties":{
    "schema_version":{"type":"string","enum":["clinical.timeline.build.v1"]},
    "status":{"type":"string","enum":["completed","partial","abstained"]},
    "scope":{"type":"object","additionalProperties":false,
      "required":["date_from","date_to","order","focus"],
      "properties":{"date_from":{"type":"string"},"date_to":{"type":"string"},"order":{"type":"string","enum":["asc","desc"]},"focus":{"type":"string"}}},
    "events":{"type":"array","maxItems":200,"items":{
      "type":"object","additionalProperties":false,
      "required":["date","date_precision","type","title","summary","references"],
      "properties":{
        "date":{"type":"string"},
        "date_precision":{"type":"string","enum":["instant","day","month","year","unknown"]},
        "type":{"type":"string","minLength":1,"maxLength":100},
        "title":{"type":"string","minLength":1,"maxLength":300},
        "summary":{"type":"string","minLength":1,"maxLength":2000},
        "references":{"type":"array","minItems":1,"items":` + canonicalReferenceSchemaJSON + `}
      }
    }},
    "coverage":{"type":"object","additionalProperties":false,
      "required":["sources_considered","events_without_date","corpus_truncated","event_limit_truncated"],
      "properties":{"sources_considered":{"type":"integer"},"events_without_date":{"type":"integer"},"corpus_truncated":{"type":"boolean"},"event_limit_truncated":{"type":"boolean"}}},
    "warnings":{"type":"array","items":{"type":"string"}}
  }
}`
