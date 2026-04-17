package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
)

const DefaultParallelChunkSize = 8 << 20

type parallelChunk struct {
	id      int
	start   int
	end     int
	scanEnd int
}

type parallelChunkResult struct {
	chunkID int
	matches []int64
	err     error
}

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

func (s *SearchService) selectEngine(engineID string) (Engine, error) {
	engine, ok := s.engines[engineID]

	if !ok {
		engine, ok = s.engines[s.defaultEngine]

		if !ok {
			return nil, &ErrNoEngine{}
		}
	}

	return engine, nil
}

func (s *SearchService) Lookup(ctx context.Context, data []byte, pattern []byte, engineID string) ([]int64, error) {
	engine, err := s.selectEngine(engineID)
	if err != nil {
		return nil, err
	}

	return engine.Search(ctx, data, pattern)
}

func (s *SearchService) ParallelSearch(ctx context.Context, filePath string, pattern []byte) ([]int64, error) {
	mapped, unmap, err := mmapReadOnlyFile(filePath)
	if err != nil {
		return nil, err
	}
	if unmap != nil {
		defer func() {
			_ = unmap()
		}()
	}

	return s.parallelSearchMapped(ctx, mapped, pattern, DefaultParallelChunkSize, runtime.NumCPU())
}

func (s *SearchService) parallelSearchMapped(ctx context.Context, data []byte, pattern []byte, chunkSize int, workers int) ([]int64, error) {
	pLen := len(pattern)
	dLen := len(data)
	if pLen == 0 || dLen == 0 || pLen > dLen {
		return nil, nil
	}

	if chunkSize <= 0 {
		chunkSize = DefaultParallelChunkSize
	}
	if chunkSize < pLen {
		chunkSize = pLen
	}

	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers < 1 {
		workers = 1
	}

	chunks := buildParallelChunks(dLen, pLen, chunkSize)
	if len(chunks) == 0 {
		return nil, nil
	}

	jobs := make(chan parallelChunk, workers*2)
	results := make(chan parallelChunkResult, workers*2)
	dispatchErrCh := make(chan error, 1)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			engine := NewSIMDEngine(0)
			for chunk := range jobs {
				if err := ctx.Err(); err != nil {
					results <- parallelChunkResult{err: err}
					return
				}

				local, err := engine.Search(ctx, data[chunk.start:chunk.scanEnd], pattern)
				if err != nil {
					results <- parallelChunkResult{err: err}
					return
				}

				if len(local) == 0 {
					continue
				}

				base := int64(chunk.start)
				chunkEnd := int64(chunk.end)
				converted := make([]int64, 0, len(local))
				for _, index := range local {
					absolute := base + index
					if absolute < chunkEnd {
						converted = append(converted, absolute)
					}
				}

				if len(converted) > 0 {
					results <- parallelChunkResult{chunkID: chunk.id, matches: converted}
				}
			}
		}()
	}

	go func() {
		defer close(jobs)

		for _, chunk := range chunks {
			select {
			case <-ctx.Done():
				dispatchErrCh <- ctx.Err()
				return
			case jobs <- chunk:
			}
		}

		dispatchErrCh <- nil
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	ordered := make([][]int64, len(chunks))
	var workerErr error

	for result := range results {
		if result.err != nil {
			if workerErr == nil {
				workerErr = result.err
			}
			continue
		}

		ordered[result.chunkID] = append(ordered[result.chunkID], result.matches...)
	}

	dispatchErr := <-dispatchErrCh
	if workerErr != nil {
		return nil, workerErr
	}
	if dispatchErr != nil {
		return nil, dispatchErr
	}

	total := 0
	for _, matches := range ordered {
		total += len(matches)
	}
	if total == 0 {
		return nil, nil
	}

	merged := make([]int64, 0, total)
	for _, matches := range ordered {
		merged = append(merged, matches...)
	}

	return merged, nil
}

func buildParallelChunks(dataLen int, patternLen int, chunkSize int) []parallelChunk {
	if patternLen == 0 || dataLen == 0 || patternLen > dataLen {
		return nil
	}

	searchLimit := dataLen - patternLen + 1
	chunkCount := (searchLimit + chunkSize - 1) / chunkSize
	chunks := make([]parallelChunk, 0, chunkCount)

	for start, id := 0, 0; start < searchLimit; start, id = start+chunkSize, id+1 {
		end := start + chunkSize
		if end > searchLimit {
			end = searchLimit
		}

		scanEnd := end + patternLen - 1
		if scanEnd > dataLen {
			scanEnd = dataLen
		}

		chunks = append(chunks, parallelChunk{
			id:      id,
			start:   start,
			end:     end,
			scanEnd: scanEnd,
		})
	}

	return chunks
}

func mmapReadOnlyFile(filePath string) ([]byte, func() error, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, err
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}

	size := info.Size()
	if size == 0 {
		if err := file.Close(); err != nil {
			return nil, nil, err
		}
		return nil, nil, nil
	}

	maxInt := int64(^uint(0) >> 1)
	if size > maxInt {
		_ = file.Close()
		return nil, nil, fmt.Errorf("file is too large to mmap on this architecture: %s", filePath)
	}

	mapped, mmapErr := syscall.Mmap(int(file.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	closeErr := file.Close()

	if mmapErr != nil {
		if closeErr != nil {
			return nil, nil, errors.Join(mmapErr, closeErr)
		}
		return nil, nil, mmapErr
	}

	if closeErr != nil {
		_ = syscall.Munmap(mapped)
		return nil, nil, closeErr
	}

	return mapped, func() error {
		return syscall.Munmap(mapped)
	}, nil
}
