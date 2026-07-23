package embeddingdeterministic

import (
	"context"
	"reflect"
	"testing"

	"github.com/devpablocristo/runtime-v2/internal/embeddings"
)

func TestDeterministicEmbeddingIsStableAcrossRetrievalTasks(t *testing.T) {
	const dimensions = 768
	provider, err := New(dimensions)
	if err != nil {
		t.Fatal(err)
	}
	document, err := provider.Embed(context.Background(), embeddings.EmbeddingRequest{
		Text: "Blood pressure protocol", TaskType: embeddings.TaskDocument,
	})
	if err != nil {
		t.Fatal(err)
	}
	query, err := provider.Embed(context.Background(), embeddings.EmbeddingRequest{
		Text: "blood pressure protocol", TaskType: embeddings.TaskQuery,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(document) != dimensions || !reflect.DeepEqual(document, query) {
		t.Fatal("same normalized text must yield the same bounded vector for local retrieval")
	}
	other, err := provider.Embed(context.Background(), embeddings.EmbeddingRequest{
		Text: "calendar scheduling policy", TaskType: embeddings.TaskQuery,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(document, other) {
		t.Fatal("different token sets must not produce the same vector")
	}
}

func TestDeterministicEmbeddingRejectsInvalidInput(t *testing.T) {
	if _, err := New(0); err == nil {
		t.Fatal("expected invalid dimensions to fail")
	}
	provider, _ := New(768)
	if _, err := provider.Embed(context.Background(), embeddings.EmbeddingRequest{
		Text: " ", TaskType: embeddings.TaskDocument,
	}); err == nil {
		t.Fatal("expected blank text to fail")
	}
	if _, err := provider.Embed(context.Background(), embeddings.EmbeddingRequest{
		Text: "text", TaskType: "classification",
	}); err == nil {
		t.Fatal("expected unsupported task type to fail")
	}
}
