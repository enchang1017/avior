# Runbook — deploying Avior from the published images

For whoever runs Avior on a host with Docker rather than in a cluster. Nothing
is built on that host: every image is pulled from GHCR at a released version.
To change Avior itself, use the development stack (`make dev`), which builds
from the checkout and is not affected by anything here.

## The images

Every published release pushes three images, for `linux/amd64` and
`linux/arm64`:

| Image | Built from | Does |
| --- | --- | --- |
| `ghcr.io/enchang1017/avior-backend` | `backend/Dockerfile` | the gateway: one static binary, distroless, nonroot, no shell, on `:8080` |
| `ghcr.io/enchang1017/avior-frontend` | `frontend/Dockerfile` | the console: the built app behind nginx on `:8080`, no API address baked in |
| `ghcr.io/enchang1017/avior-migrate` | `deploy/migrate/Dockerfile` | goose and `db/migrations`, applied to `aviordb` as its owner, then exits |

Tags:

| Tag | Moves? | Means |
| --- | --- | --- |
| `1.2.0` | never | the release `v1.2.0`. Deploy this. |
| `latest` | on every release that is not a prerelease | the newest stable release. Convenient to try; not something to roll back to. |

A prerelease such as `v1.3.0-rc.1` is pushed as `1.3.0-rc.1` and leaves
`latest` where it was. A release whose tag is not `vMAJOR.MINOR.PATCH` is
refused and pushes nothing. The gateway logs the version it was built from on
every start (`avior listening … version=1.2.0`).

`.github/workflows/images.yml` publishes them when a GitHub release is
published, with the workflow's own token and `packages: write` in that one job.
Pull requests that touch the images build all three for both platforms without
pushing.

## 0. Once per PostgreSQL server

Run `deploy/store/bootstrap.sql` as a superuser, exactly as in
`docs/runbooks/new-governed-database.md` step 0. It creates Avior's roles and
`aviordb`. Load `pg_stat_statements` on the server if you want the observatory.

The compose file below does **not** start a PostgreSQL server. Avior governs the
server you already have, and one bundled beside it would be the wrong one.

## 1. Configure

Everything lives in `deploy/compose/`. Check out the release tag, or copy that
release's `deploy/compose/` to the host keeping its path, and start from the
directory that holds `deploy/`. **Every command from here on runs inside
`deploy/compose/`, with `AVIOR_VERSION` exported**, so the same lines work for
starting, inspecting and upgrading. In a new shell, `cd` and export again.
`docker compose` finds `deploy/compose/docker-compose.yml` by being run there,
so no command names it.

```
cd deploy/compose
export AVIOR_VERSION=1.2.0
cp gateway.env.example gateway.env
cp migrate.env.example migrate.env
mkdir -p secrets/gateway secrets/migrate
```

- `deploy/compose/gateway.env.example` → `gateway.env`: the gateway's
  environment. Set the DSNs to your server, **with no password in them**, and
  the directory settings. `AVIOR_ENV` is not in it: `docker-compose.yml` sets it
  to `production` unless the shell sets `AVIOR_ENV=staging`.
- `deploy/compose/migrate.env.example` → `migrate.env`: the DSN of `aviordb`'s
  owner, again with no password.
- `secrets/gateway/`: one file per secret, named as `gateway.env` names them —
  `session_secret` (at least 32 random bytes, e.g. `openssl rand -base64 48`),
  `admin_password`, `store_password`, `target_password`, and the write, deploy
  and provision passwords for the roles you configured.
- `secrets/migrate/store_owner_password`: the password of `avior`.

The gateway runs as uid 65532, so it has to be able to read its files:

```
chmod 0400 secrets/*/*
sudo chown 65532 secrets/gateway/*
```

`gateway.env`, `migrate.env` and `secrets/` are gitignored. None of them
belongs in a repository or in an image.

## 2. Start

```
docker compose pull
docker compose up -d
```

`AVIOR_VERSION` is required by every `docker compose` command against this file,
`ps` and `logs` included; compose refuses to run without it rather than
guessing. In order:

1. `migrate` applies any new migration and exits. If it fails, nothing else
   starts, and this says why:

   ```
   docker compose logs migrate
   ```

2. `backend` starts read-only, with every capability dropped. It refuses to
   start on a missing value, the development session secret, `trusted-local`
   sign-in, or self-approval of writes in production, and says which.
3. `frontend` and `edge` start. `edge` is nginx sending `/api/` to the gateway
   and everything else to the console, so the browser sees one origin.

Check it:

```
docker compose ps     # edge is (healthy)
docker compose logs backend
curl -fsS http://127.0.0.1:8080/api/v1/health
```

## 3. Put TLS in front

`edge` serves plain HTTP on `127.0.0.1:8080` and nowhere else. The session
cookie is `Secure`, so a browser reaching Avior over plain HTTP from another
machine signs in and is then refused on every request. Terminate TLS in a proxy
you already run on the host and forward to `127.0.0.1:8080`.

To publish on another address, set `AVIOR_PUBLISH`, for example
`AVIOR_PUBLISH=10.0.0.5:8080`, and only to a network your TLS proxy is on.

## Upgrade and roll back

From `deploy/compose/` of the new release, keeping `gateway.env`, `migrate.env`
and `secrets/` from the old one:

```
export AVIOR_VERSION=1.3.0
docker compose pull
docker compose up -d
docker compose ps
```

The migration runs first, then the gateway is replaced. Migrations only move
forward. To roll back, start the previous version: it runs against the newer
schema, which is safe only when the newer release's notes say its migrations
were additive.

## Pull refused, or images from a fork

`AVIOR_IMAGE_OWNER=<owner>` pulls `ghcr.io/<owner>/avior-*` instead.

A pull answered `denied` or `unauthorized` means the package is private. Its
owner makes it public under the package's settings on GitHub; until then,
`docker login ghcr.io` with a token holding `read:packages` pulls it.
