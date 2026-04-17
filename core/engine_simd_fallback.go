package core

import "context"

type SIMDFallbackEngine struct {
	delegate Engine
}

func NewSIMDFallbackEngine(delegate Engine) *SIMDFallbackEngine {
	if delegate == nil {
		delegate = NewKMPEngine()
	}

	return &SIMDFallbackEngine{delegate: delegate}
}

func (e *SIMDFallbackEngine) GetID() string {
	return SIMDEngineID
}

func (e *SIMDFallbackEngine) Search(ctx context.Context, data []byte, pattern []byte) ([]int64, error) {
	return e.delegate.Search(ctx, data, pattern)
}
