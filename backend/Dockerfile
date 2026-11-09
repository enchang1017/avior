# The gateway, built for a cluster rather than for a developer's machine.
#
# Dockerfile.dev beside this one runs air and mounts the source; this one builds
# once and ships a binary. Two files rather than a stage in one, because the
# development image's job is to rebuild on every keystroke and this one's is to
# contain as little as possible.
# --platform=$BUILDPLATFORM pins the build stage to the machine doing the
# building. The runners are amd64 and the nodes are arm64, and without this
# buildx emulates the whole compile under QEMU — for a language that
# cross-compiles by setting a variable.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .

# Static, because the runtime stage has no libc to link against. The version is
# stamped rather than derived: the binary reports it on every start, and a build
# that cannot say which commit it came from is one nobody can match to a
# pipeline afterwards.
ARG VERSION=unknown
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
	-trimpath \
	-ldflags="-s -w -X main.version=${VERSION}" \
	-o /out/avior ./cmd/avior

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/avior /avior

# Nonroot, and no shell: the gateway holds credentials for every governed
# database on the server, and a container somebody can exec into is the shortest
# path to reading them out of the process environment.
USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/avior"]
