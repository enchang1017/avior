# The PostgreSQL image is the base because it carries psql and is pulled for the
# postgres service anyway. goose comes in as a static binary so it does not care
# about the base's libc.
FROM golang:1.26-alpine AS goose
RUN CGO_ENABLED=0 go install github.com/pressly/goose/v3/cmd/goose@v3.22.1

FROM postgres:18-alpine

COPY --from=goose /go/bin/goose /usr/local/bin/goose

WORKDIR /seed
COPY db/migrations ./migrations
COPY deploy/governed ./governed
COPY deploy/seed/ ./
RUN chmod +x seed.sh

# The base image starts postgres. This container seeds and exits.
ENTRYPOINT []
CMD ["./seed.sh"]
