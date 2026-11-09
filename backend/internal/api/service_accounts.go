package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/oapi-codegen/nullable"

	"github.com/enchang1017/avior/backend/internal/feature/accounts"
	"github.com/enchang1017/avior/backend/internal/feature/token"
	"github.com/enchang1017/avior/backend/internal/gen"
)

// FEAT-063. Service accounts: made and listed through the account slice,
// given tokens through the token slice. Every operation here needs
// account.manage and a session, which operationAccess states.

// ListServiceAccounts returns every service account with its roles and tokens.
func (s *Server) ListServiceAccounts(
	ctx context.Context, _ gen.ListServiceAccountsRequestObject,
) (gen.ListServiceAccountsResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.ListServiceAccounts401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	listed, err := s.accounts.ListServiceAccounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListServiceAccounts200JSONResponse, 0, len(listed))
	for _, account := range listed {
		out = append(out, toServiceAccount(account))
	}
	return out, nil
}

// CreateServiceAccount makes a service account holding no role. AC-1.
func (s *Server) CreateServiceAccount(
	ctx context.Context, request gen.CreateServiceAccountRequestObject,
) (gen.CreateServiceAccountResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.CreateServiceAccount401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	created, err := s.accounts.CreateServiceAccount(ctx, accounts.Caller{UserID: caller.ID},
		request.Body.Name, request.Body.Description)
	switch {
	case errors.Is(err, accounts.ErrServiceNameInvalid):
		return serviceAccountRefused("service_account.name_invalid",
			"A service account is named svc- and then what it is, with no spaces: svc-reporter."), nil
	case errors.Is(err, accounts.ErrNameTaken):
		return serviceAccountRefused("service_account.name_taken",
			"An account named "+request.Body.Name+" already exists. Names are compared without regard to case."), nil
	case errors.Is(err, accounts.ErrDescriptionBlank):
		return serviceAccountRefused("service_account.description_blank",
			"Say what the agent is for and who runs it. A year from now, that is all anybody will have."), nil
	case err != nil:
		return nil, err
	}
	return gen.CreateServiceAccount201JSONResponse(toServiceAccount(created)), nil
}

func serviceAccountRefused(code, message string) gen.CreateServiceAccount422JSONResponse {
	return gen.CreateServiceAccount422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
		Code: code, Message: message,
	}}
}

// CreateServiceAccountToken mints a token for a service account. AC-4, AC-5.
func (s *Server) CreateServiceAccountToken(
	ctx context.Context, request gen.CreateServiceAccountTokenRequestObject,
) (gen.CreateServiceAccountTokenResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.CreateServiceAccountToken401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	lifetime := 0
	if request.Body.ExpiresInDays != nil {
		lifetime = *request.Body.ExpiresInDays
	}
	minted, err := s.tokens.MintForService(ctx, request.OperatorId, request.Body.Name, lifetime)
	var tooMany *token.TooManyLiveError
	switch {
	case errors.As(err, &tooMany):
		return gen.CreateServiceAccountToken409JSONResponse{
			Code: "token.too_many_live",
			Message: "This service account already holds every live token it may: " + quoted(tooMany.Live) +
				". Revoke one before minting another.",
		}, nil
	case errors.Is(err, token.ErrNotServiceAccount):
		return gen.CreateServiceAccountToken404JSONResponse{NotFoundJSONResponse: serviceAccountUnknown()}, nil
	case errors.Is(err, token.ErrNameBlank):
		return gen.CreateServiceAccountToken422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code: "token.name_blank", Message: "Give the token a name, so you can tell it apart from the other.",
		}}, nil
	case errors.Is(err, token.ErrLifetimeOutOfRange):
		return gen.CreateServiceAccountToken422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code: "token.lifetime_out_of_range", Message: "A token lasts between one and 730 days (two years).",
		}}, nil
	case err != nil:
		return nil, err
	}
	return gen.CreateServiceAccountToken201JSONResponse{
		Id: minted.ID, Name: minted.Name, CreatedAt: minted.CreatedAt, ExpiresAt: minted.ExpiresAt,
		LastUsedAt: nullable.NewNullNullable[time.Time](),
		RevokedAt:  nullable.NewNullNullable[time.Time](),
		Token:      minted.Value,
	}, nil
}

// RevokeServiceAccountToken revokes one of a service account's tokens.
func (s *Server) RevokeServiceAccountToken(
	ctx context.Context, request gen.RevokeServiceAccountTokenRequestObject,
) (gen.RevokeServiceAccountTokenResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.RevokeServiceAccountToken401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	err := s.tokens.RevokeForService(ctx, request.OperatorId, request.TokenId)
	switch {
	case errors.Is(err, token.ErrNotFound):
		return gen.RevokeServiceAccountToken404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{
			Code: "token.not_found", Message: "That service account holds no live token with that id.",
		}}, nil
	case err != nil:
		return nil, err
	}
	return gen.RevokeServiceAccountToken204Response{}, nil
}

func serviceAccountUnknown() gen.NotFoundJSONResponse {
	return gen.NotFoundJSONResponse{Code: "service_account.not_found", Message: "No such service account."}
}

func quoted(names []string) string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, `"`+name+`"`)
	}
	return strings.Join(out, " and ")
}

func toServiceAccount(account accounts.ServiceAccount) gen.ServiceAccount {
	tokens := make([]gen.ApiToken, 0, len(account.Tokens))
	for _, held := range account.Tokens {
		tokens = append(tokens, apiToken(token.Token{
			ID: held.ID, Name: held.Name, CreatedAt: held.CreatedAt, ExpiresAt: held.ExpiresAt,
			LastUsedAt: held.LastUsedAt, RevokedAt: held.RevokedAt,
		}))
	}
	return gen.ServiceAccount{
		Id:          account.ID,
		Username:    account.Username,
		Description: account.Description,
		Disabled:    account.Disabled,
		CreatedBy:   account.CreatedBy,
		CreatedAt:   account.CreatedAt,
		Roles:       account.Roles,
		Tokens:      tokens,
	}
}
