# The migration runner: goose and the migration directory, and nothing else.
#
# Separate from the backend image on purpose. Applying migrations is a different
# act by a different principal — it connects as the owner of aviordb, where the
# gateway connects as roles that cannot issue DDL — and an image that carries
# both would let either one run the other's credential.
#
# goose comes in as a static binary so the runtime base does not have to be Go's
# and does not have to share its libc. The same two-stage shape as
# deploy/seed/Dockerfile, which carries goose for the same reason.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS goose
ARG TARGETARCH
# Copied out by find rather than from a fixed path. `go install` writes to
# /go/bin/goose when it is building for the machine it is on and to
# /go/bin/<goos>_<goarch>/goose when it is cross-compiling, and GOBIN cannot be
# used to flatten that — Go refuses it outright for a cross-compiled binary.
RUN CGO_ENABLED=0 GOARCH=${TARGETARCH} go install github.com/pressly/goose/v3/cmd/goose@v3.22.1 && \
	mkdir -p /out && \
	find /go/bin -type f -name goose -exec cp {} /out/goose \;

FROM alpine:3.21

COPY --from=goose /out/goose /usr/local/bin/goose
COPY db/migrations /migrations
COPY deploy/migrate/run.sh /usr/local/bin/run.sh
RUN chmod +x /usr/local/bin/run.sh

ENTRYPOINT ["/usr/local/bin/run.sh"]
CMD ["up"]
