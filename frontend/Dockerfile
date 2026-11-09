# The single-page app, built once and served as files.
#
# The app addresses the gateway at /api/v1 relative to its own origin, so this
# image carries no API address and needs no build-time environment: the ingress
# decides which service answers /api, and the same image is correct in every
# environment.
# --platform=$BUILDPLATFORM: what this stage produces is static files, which
# have no architecture. Emulating a Vite build to make bytes that would have
# been identical either way is the most expensive way to change nothing.
FROM --platform=$BUILDPLATFORM node:23-alpine AS build

ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable

WORKDIR /app

COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY frontend/ .
RUN pnpm run build

FROM nginx:1.27-alpine

COPY --from=build /app/dist /usr/share/nginx/html
COPY frontend/deploy/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 8080
