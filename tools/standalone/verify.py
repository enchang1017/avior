"""Exercise the released standalone image against a disposable real PostgreSQL.

Build deploy/standalone/Dockerfile first; set AVIOR_TEST_IMAGE to its tag.
TMPDIR must be the task's approved scratch workspace.
"""
import http.cookiejar
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import uuid


def docker(*args, check=True):
    return subprocess.run(
        ["docker", *args], check=check, capture_output=True, text=True
    )


def main():
    image = os.environ.get("AVIOR_TEST_IMAGE", "avior:local")
    scratch = Path(os.environ["TMPDIR"])
    name = "avior-check-" + uuid.uuid4().hex[:10]
    network, postgres, app = name, name + "-pg", name + "-app"
    env = {
        "AVIOR_ENV": "development",
        "AVIOR_AUTH_MODE": "trusted-local",
        "AVIOR_SECURE_COOKIES": "false",
        "AVIOR_TARGET_LABEL": "CDP local",
        "AVIOR_STORE_DSN": "postgres://avior_local_store@postgres/avior",
        "AVIOR_TARGET_DSN": "postgres://avior_local_reader@postgres/cdp",
        "AVIOR_STORE_PASSWORD": "test-store-only",
        "AVIOR_TARGET_PASSWORD": "test-reader-only",
        "AVIOR_SESSION_SECRET": "0123456789abcdef0123456789abcdef",
    }

    def run_environment(values):
        return [arg for key, value in values.items() for arg in ("-e", key + "=" + value)]

    def psql(sql, database="postgres"):
        return docker("exec", postgres, "psql", "-U", "postgres", "-d", database,
                      "-XAt", "-v", "ON_ERROR_STOP=1", "-c", sql).stdout.strip()

    try:
        docker("network", "create", network)
        docker("run", "-d", "--name", postgres, "--network", network,
               "--network-alias", "postgres", "-e", "POSTGRES_PASSWORD=test-admin-only",
               "-e", "POSTGRES_DB=cdp", "postgres:18-alpine")
        for _ in range(100):
            if docker("exec", postgres, "pg_isready", "-U", "postgres", check=False).returncode == 0:
                break
            time.sleep(0.3)
        else:
            raise AssertionError("PostgreSQL did not become ready")
        initialize = {**env, "AVIOR_BOOTSTRAP_DSN": "postgres://postgres@postgres/postgres",
                      "PGPASSWORD": "test-admin-only"}
        for _ in range(2):
            docker("run", "--rm", "--network", network,
                   *run_environment(initialize), image, "init")
        psql("CREATE SCHEMA identity; CREATE TABLE identity.users (id int PRIMARY KEY, email text); "
             "INSERT INTO identity.users SELECT n, 'user' || n || '@example.test' "
             "FROM generate_series(1,25) n; ANALYZE identity.users;", "cdp")
        # Password files exercise the entrypoint and the Go configuration together.
        with tempfile.TemporaryDirectory(dir=scratch) as directory:
            password = Path(directory) / "store-password"
            password.write_text(env["AVIOR_STORE_PASSWORD"] + "\n")
            password.chmod(0o644)
            serving = {**env, "AVIOR_STORE_PASSWORD_FILE": "/check/store-password"}
            del serving["AVIOR_STORE_PASSWORD"]
            docker("run", "-d", "--name", app, "--network", network,
                   "-p", "127.0.0.1::8080", "-v", str(password) + ":/check/store-password:ro",
                   *run_environment(serving), image)
            ports = json.loads(docker("inspect", app).stdout)[0]["NetworkSettings"]["Ports"]
            assert ports["8080/tcp"][0]["HostIp"] == "127.0.0.1"
            base = "http://127.0.0.1:" + ports["8080/tcp"][0]["HostPort"]
            opener = urllib.request.build_opener(
                urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
            )

            def call(path, body=None):
                request = urllib.request.Request(
                    base + "/api/v1" + path,
                    data=None if body is None else json.dumps(body).encode(),
                    headers={"Content-Type": "application/json"},
                )
                try:
                    with opener.open(request, timeout=5) as response:
                        return response.status, json.load(response)
                except urllib.error.HTTPError as error:
                    return error.code, json.load(error)

            for _ in range(100):
                try:
                    if call("/health")[0] == 200:
                        break
                except (OSError, urllib.error.URLError):
                    pass
                time.sleep(0.3)
            else:
                raise AssertionError("Avior did not become ready: " + docker("logs", app).stderr)
            with opener.open(base + "/query") as response:
                assert response.status == 200 and b"<html" in response.read()
            assert call("/auth/login", {"username": "developer"})[0] == 200
            status, caller = call("/auth/me")
            assert status == 200 and caller["instance"]["name"] == "CDP local"
            instance = caller["instance"]["id"]
            assert call("/schema/databases?instanceId=" + instance) == (200, ["cdp"])
            target = {"instanceId": instance, "database": "cdp"}
            status, template = call("/query/template", {
                **target, "schema": "identity", "table": "users", "columns": ["id", "email"],
                "offset": 10, "top": 10,
            })
            assert status == 200, template
            status, rows = call("/query/execute", {**target, "statement": template["statement"]})
            assert status == 200 and rows["rowCount"] == 10 and rows["rows"][0][0] == 11, rows
            status, _ = call("/query/execute", {
                **target, "database": "avior", "statement": "SELECT username FROM public.app_user",
            })
            assert status != 200
            denied = docker("run", "--rm", "--network", network,
                            "-e", "PGPASSWORD=test-reader-only", image, "init", check=False)
            assert denied.returncode != 0
            # Check engine permissions too, independently of the console's guard.
            denied = docker("exec", "-e", "PGPASSWORD=test-reader-only", postgres,
                            "psql", "-h", "127.0.0.1", "-U", "avior_local_reader",
                            "-d", "avior", "-c", "SELECT 1", check=False)
            assert denied.returncode != 0
            # Restart preserves the account, schema and data and reapplies no old migration.
            docker("restart", app)
            ports = json.loads(docker("inspect", app).stdout)[0]["NetworkSettings"]["Ports"]
            base = "http://127.0.0.1:" + ports["8080/tcp"][0]["HostPort"]
            for _ in range(100):
                try:
                    if call("/health")[0] == 200:
                        break
                except (OSError, urllib.error.URLError):
                    pass
                time.sleep(0.3)
            else:
                raise AssertionError("Restart failed: " + str(docker("logs", app)))
            assert psql("SELECT count(*) FROM identity.users", "cdp") == "25"
            bad = Path(directory) / "00025_bad.sql"
            bad.write_text("-- +goose Up\nSELECT 1/0;\n")
            failed = docker("run", "--rm", "--network", network,
                            "-v", str(bad) + ":/migrations/00025_bad.sql:ro",
                            *run_environment(env), image, check=False)
            assert failed.returncode != 0 and "division by zero" in failed.stderr
            psql("CREATE DATABASE aviordb OWNER avior_local_store")
            outside_dev = {**env, "AVIOR_ENV": "production",
                           "AVIOR_STORE_DSN": "postgres://avior_local_store@postgres/aviordb"}
            failed = docker("run", "--rm", "--network", network,
                            *run_environment(outside_dev), image, check=False)
            assert failed.returncode != 0
            assert psql("SELECT to_regclass('public.goose_db_version') IS NULL", "aviordb") == "t"
        print("PASS standalone: init, files, migrations, SPA, login, label, paging, isolation, restart, fail-closed")
    finally:
        docker("rm", "-f", app, postgres, check=False)
        docker("network", "rm", network, check=False)


if __name__ == "__main__":
    main()
