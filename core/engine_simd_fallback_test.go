package core

import (
	"context"
	"slices"
	"testing"
)

func TestSIMDFallbackEngineDelegatesSearch(t *testing.T) {
	t.Parallel()

	delegate := &stubEngine{
		id:      KMPEngineID,
		matches: []int64{2, 5, 9},
	}

	engine := NewSIMDFallbackEngine(delegate)

	if engine.GetID() != SIMDEngineID {
		t.Fatalf("GetID() = %q, want %q", engine.GetID(), SIMDEngineID)
	}

	got, err := engine.Search(context.Background(), []byte("abcabcabc"), []byte("abc"))
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if !delegate.called {
		t.Fatal("delegate Search was not called")
	}

	if !slices.Equal(got, delegate.matches) {
		t.Fatalf("Search() = %v, want %v", got, delegate.matches)
	}
}

type stubEngine struct {
	id      string
	called  bool
	matches []int64
}

func (e *stubEngine) GetID() string {
	return e.id
}

func (e *stubEngine) Search(context.Context, []byte, []byte) ([]int64, error) {
	e.called = true
	return e.matches, nil
}
