package api

import (
	"context"
	"fmt"

	"github.com/getkin/kin-openapi/openapi3filter"
	oapimiddleware "github.com/oapi-codegen/nethttp-middleware"

	"github.com/enchang1017/avior/backend/internal/gen"
)

// RequestValidator enforces api/openapi.yaml against every request.
//
// Without it the schema is decorative: a pattern, a minLength, or a maximum in
// the contract would be documented and unenforced, and each handler would have
// to re-implement it. With it, a constraint starts being enforced the moment it
// is written into the spec.
//
// Two things it deliberately does not do:
//
// Authentication. The spec's security block documents which endpoints need a
// session, but [Authenticate] resolves it and the handlers decide, so that a
// refusal carries the Error body the contract defines rather than the
// validator's own text.
//
// Response validation. Responses are shaped by generated types, so a mismatch
// is a compile error rather than something to catch at runtime.
func RequestValidator() (gen.MiddlewareFunc, error) {
	spec, err := gen.GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("api: load embedded spec: %w", err)
	}
	// Servers is left as the spec declares it. The router mounts the API under
	// /api/v1, and the validator needs that prefix to match an incoming path
	// against an operation; clearing it turns every request into a 404.
	return oapimiddleware.OapiRequestValidatorWithOptions(spec, &oapimiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: func(context.Context, *openapi3filter.AuthenticationInput) error {
				return nil
			},
		},
	}), nil
}
