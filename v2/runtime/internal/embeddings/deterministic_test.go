package embeddings

import (
	"context"
	"reflect"
	"testing"
)

func TestDeterministicEmbeddingIsStableAcrossRetrievalTasks(t *testing.T) {
	provider, err := NewDeterministic(DefaultDim)
	if err != nil {
		t.Fatal(err)
	}
	document, err := provider.Embed(context.Background(), "Blood pressure protocol", TaskDocument)
	if err != nil {
		t.Fatal(err)
	}
	query, err := provider.Embed(context.Background(), "blood pressure protocol", TaskQuery)
	if err != nil {
		t.Fatal(err)
	}
	if len(document) != DefaultDim || !reflect.DeepEqual(document, query) {
		t.Fatal("same normalized text must yield the same bounded vector for local retrieval")
	}
	other, err := provider.Embed(context.Background(), "calendar scheduling policy", TaskQuery)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(document, other) {
		t.Fatal("different token sets must not produce the same vector")
	}
}

func TestDeterministicEmbeddingRejectsInvalidInput(t *testing.T) {
	if _, err := NewDeterministic(0); err == nil {
		t.Fatal("expected invalid dimensions to fail")
	}
	provider, _ := NewDeterministic(DefaultDim)
	if _, err := provider.Embed(context.Background(), " ", TaskDocument); err == nil {
		t.Fatal("expected blank text to fail")
	}
	if _, err := provider.Embed(context.Background(), "text", "classification"); err == nil {
		t.Fatal("expected unsupported task type to fail")
	}
}
