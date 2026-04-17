package core

import "context"

type Engine interface {
	Search(ctx context.Context, data []byte, pattern []byte) ([]int64, error)
	GetID() string
}
