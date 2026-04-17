package transport

import (
	"jexxor/bytestorm/api"
	"jexxor/bytestorm/core"
)

type SearchHandler struct {
	api.UnimplementedSearchServiceServer
	svc *core.SearchService
}

func NewSearchHandler(svc *core.SearchService) *SearchHandler {
	return &SearchHandler{svc: svc}
}
