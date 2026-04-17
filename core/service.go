package core

import "context"

type SearchService struct {
	engines       map[string]Engine
	defaultEngine string
}

func NewSearchService(defaultEngine string) *SearchService {
	return &SearchService{
		engines:       make(map[string]Engine),
		defaultEngine: defaultEngine,
	}
}

func (s *SearchService) RegisterEngine(engine Engine) {
	s.engines[engine.GetID()] = engine
}

type ErrNoEngine struct{}

func (e *ErrNoEngine) Error() string {
	return "no search engine available"
}

func (s *SearchService) Lookup(ctx context.Context, data []byte, pattern []byte, engineID string) ([]int64, error) {
	engine, ok := s.engines[engineID]

	if !ok {
		engine, ok = s.engines[s.defaultEngine]

		if !ok {
			return nil, &ErrNoEngine{}
		}
	}

	return engine.Search(ctx, data, pattern)
}
