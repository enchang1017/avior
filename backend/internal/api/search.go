package api

import (
	"context"
	"errors"

	"github.com/enchang1017/avior/backend/internal/feature/search"
	"github.com/enchang1017/avior/backend/internal/gen"
	rank "github.com/enchang1017/avior/backend/internal/platform/search"
)

// SearchService is the part of the search slice this layer needs.
type SearchService interface {
	Search(ctx context.Context, caller search.Caller, query string, limit *int) (search.Result, error)
}

// Search is FEAT-066. The query is never logged or kept: whatever an operator
// typed is theirs, and a search box collects things nobody meant to submit.
func (s *Server) Search(ctx context.Context, request gen.SearchRequestObject) (gen.SearchResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.Search401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	result, err := s.search.Search(ctx, search.Caller{
		UserID: caller.ID, Roles: caller.Roles, Permissions: caller.Permissions,
	}, request.Params.Q, request.Params.Limit)
	if errors.Is(err, search.ErrQueryLength) {
		return gen.Search422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code:    "search.query_length",
			Message: "Search for 2 to 64 characters.",
		}}, nil
	}
	if err != nil {
		return nil, err
	}
	out := gen.SearchResult{
		Hits:     make([]gen.SearchHit, 0, len(result.Hits)),
		Limit:    result.Limit,
		Indexing: result.Indexing,
	}
	for _, hit := range result.Hits {
		out.Hits = append(out.Hits, toSearchHit(hit))
	}
	return gen.Search200JSONResponse(out), nil
}

var searchKinds = map[rank.Kind]gen.SearchHitKind{
	rank.KindDatabase:     gen.SearchHitKindDatabase,
	rank.KindTable:        gen.SearchHitKindTable,
	rank.KindWriteRequest: gen.SearchHitKindWriteRequest,
	rank.KindRelease:      gen.SearchHitKindRelease,
	rank.KindOperator:     gen.SearchHitKindOperator,
}

func toSearchHit(hit search.Hit) gen.SearchHit {
	out := gen.SearchHit{Kind: searchKinds[hit.Kind], Label: hit.Label}
	switch hit.Kind {
	case rank.KindPage:
		// Pages are matched in the browser, and the server never ranks one.
	case rank.KindDatabase:
		out.Database = &hit.Database
	case rank.KindTable:
		kind := gen.SearchHitObjectKind(hit.ObjectKind)
		out.Database, out.Schema, out.Name, out.ObjectKind = &hit.Database, &hit.Schema, &hit.Name, &kind
	case rank.KindWriteRequest, rank.KindRelease:
		out.Id, out.Database, out.State = &hit.ID, &hit.Database, &hit.State
	case rank.KindOperator:
		out.Id, out.ServiceAccount = &hit.ID, &hit.ServiceAccount
	}
	return out
}
