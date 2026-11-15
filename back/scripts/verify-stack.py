#!/usr/bin/env python3
import argparse
import json
import os
import re
import subprocess
import tarfile
import tempfile
import time
from pathlib import Path


def redact(value, directory):
    env_file = Path(directory) / ".env.prod"
    if env_file.is_file():
        for line in env_file.read_text().splitlines():
            if "=" in line and not line.startswith("#"):
                secret = line.split("=", 1)[1].strip().strip("'\"")
                if len(secret) >= 6:
                    value = value.replace(secret, "[redacted]")
    return re.sub(r"[A-Za-z0-9+/=_-]{24,}", "[redacted]", value)


def run(*args, **kwargs):
    result = subprocess.run(
        args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, **kwargs
    )
    if result.returncode:
        operation = " ".join(str(arg) for arg in args[:2])
        if len(args) > 2 and args[1] == "exec":
            operation += " " + next(
                (arg for arg in args[3:] if arg in ("pg_restore", "wget", "python")),
                "container",
            )
        if args[1] == "compose":
            operation += " " + next(
                (arg for arg in args if arg in ("create", "start", "down")),
                "configuration",
            )
        detail = result.stderr.decode(errors="replace")[-1500:]
        detail = redact(detail, kwargs.get("cwd", "/nonexistent"))
        raise RuntimeError(
            f"{operation} failed (exit {result.returncode}): {detail.strip()}"
        )
    return result.stdout.decode()


def extract(archive, destination):
    with tarfile.open(archive) as source:
        for member in source.getmembers():
            path = Path(member.name)
            if (
                path.is_absolute()
                or ".." in path.parts
                or member.issym()
                or member.islnk()
                or not (member.isfile() or member.isdir())
            ):
                raise ValueError("Unsafe archive member")
        source.extractall(destination, filter="data")


def verify(archive, production_project):
    if not re.fullmatch(r"[a-zA-Z0-9_-]+", production_project):
        raise ValueError("Unsafe project name")
    project = f"confhub_restore_{int(time.time())}_{os.getpid()}"
    with tempfile.TemporaryDirectory(prefix="confhub-restore-stack-") as temporary:
        root = Path(temporary)
        os.chmod(root, 0o700)
        extract(archive, root / "archive")
        backup = root / "archive"
        extract(backup / "deployment-config.tar.gz", root / "deployment")
        back = root / "deployment" / "back"
        for name in (
            "identity-svc",
            "conference-svc",
            "application-svc",
            "engagement-svc",
            "operations",
        ):
            directory = back / "secrets" / "mtls" / name
            directory.chmod(0o750)
            (directory / "key.pem").chmod(0o640)
            (directory / "cert.pem").chmod(0o644)
        for name in ("key", "credentials.enc"):
            (back / "secrets" / "admin-auth" / name).chmod(0o640)
        (back / "secrets" / "jwt_ed25519.key").chmod(0o640)
        images = {}
        for line in (backup / "images.txt").read_text().splitlines():
            service, image = line.split()
            if not re.fullmatch(r"[a-z][a-z0-9-]*", service) or not re.fullmatch(
                r"sha256:[a-f0-9]{64}", image
            ):
                raise ValueError("Unsafe image manifest")
            images[service] = image
        print("Loading archived container images", flush=True)
        with (backup / "images.tar.gz").open("rb") as stream:
            run("docker", "image", "load", stdin=stream)
        services = {service: {"image": image} for service, image in images.items()}
        for service in (
            "identity-svc",
            "conference-svc",
            "application-svc",
            "engagement-svc",
        ):
            services[service]["volumes"] = [
                f"{back}/secrets/jwt_ed25519.pub:/run/secrets/jwt_public:ro"
            ]
        services["identity-svc"]["volumes"].append(
            f"{back}/secrets/jwt_ed25519.key:/run/secrets/jwt_private:ro"
        )
        services["nginx"].update(
            ports=[],
            volumes=[f"{back}/nginx/nginx.local.conf:/etc/nginx/nginx.conf:ro"],
        )
        services["engagement-svc"]["environment"] = {
            "SMTP_HOST": "mailpit",
            "SMTP_PORT": "1025",
            "SMTP_TLS_MODE": "",
            "SMTP_USER": "",
            "SMTP_PASSWORD": "",
        }
        services["identity-svc"]["environment"] = {
            "ORCID_CLIENT_ID": "",
            "ORCID_CLIENT_SECRET": "",
        }
        if "jaeger" in services:
            services["jaeger"]["ports"] = []
        override = {"services": services, "networks": {"default": {"internal": True}}}
        overlay = root / "restore.json"
        overlay.write_text(json.dumps(override))
        # Reset public port mappings independently of Compose list merging.
        port_reset = root / "ports.yml"
        reset_lines = "services:\n  nginx:\n    ports: !reset []\n"
        reset_lines += "".join(
            f"  {service}:\n    build: !reset null\n"
            for service in (
                "identity-svc",
                "conference-svc",
                "application-svc",
                "engagement-svc",
                "django-admin",
                "frontend",
            )
        )
        if "jaeger" in services:
            reset_lines += "  jaeger:\n    ports: !reset []\n"
        port_reset.write_text(reset_lines)
        (back / "secrets" / "admin-auth").chmod(0o750)
        compose = [
            "docker",
            "compose",
            "-p",
            project,
            "--env-file",
            str(back / ".env.prod"),
            "-f",
            str(back / "docker-compose.prod.yml"),
            "-f",
            str(back / "docker-compose.build.yml"),
            "-f",
            str(overlay),
            "-f",
            str(port_reset),
        ]
        try:
            print("Creating isolated restore stack", flush=True)
            run(*compose, "create", "--no-build", cwd=back)
            for volume in ("miniodata", "natsdata", "redisstatedata") + (
                ("jaegerdata",) if "jaeger" in services else ()
            ):
                snapshot = backup / f"{production_project}_{volume}.tar.gz"
                if not snapshot.exists():
                    raise ValueError(f"Missing {volume} snapshot")
                run(
                    "docker",
                    "run",
                    "--rm",
                    "--network",
                    "none",
                    "-v",
                    f"{project}_{volume}:/restore",
                    "-v",
                    f"{backup}:/backup:ro",
                    "alpine:3.21",
                    "tar",
                    "-xzf",
                    f"/backup/{snapshot.name}",
                    "-C",
                    "/restore",
                )
            print("Restoring persistent volumes and PostgreSQL", flush=True)
            run(*compose, "start", "postgres", cwd=back)
            for _ in range(60):
                state = run(
                    "docker",
                    "inspect",
                    "-f",
                    "{{.State.Health.Status}}",
                    f"{project}-postgres-1",
                ).strip()
                if state == "healthy":
                    break
                time.sleep(1)
            else:
                raise RuntimeError("Restore PostgreSQL readiness timeout")
            with (backup / "postgres.dump").open("rb") as stream:
                run(
                    "docker",
                    "exec",
                    "-i",
                    f"{project}-postgres-1",
                    "pg_restore",
                    "-U",
                    "postgres",
                    "-d",
                    "confhub",
                    "--exit-on-error",
                    "--clean",
                    "--if-exists",
                    stdin=stream,
                )
            print("Starting restored services and checking business APIs", flush=True)
            try:
                run(*compose, "start", cwd=back)
            except RuntimeError:
                for service in images:
                    name = f"{project}-{service}-1"
                    state = json.loads(run("docker", "inspect", name))[0]["State"]
                    if state.get("Health", {}).get("Status") == "unhealthy" or (
                        state["Status"] != "created" and not state["Running"]
                    ):
                        result = subprocess.run(
                            ["docker", "logs", "--tail", "15", name],
                            capture_output=True,
                        )
                        logs = redact(
                            (result.stdout + result.stderr).decode(errors="replace"),
                            back,
                        )
                        print(
                            f"Restore diagnostics ({service}): {logs[-2500:]}",
                            flush=True,
                        )
                raise
            deadline = time.monotonic() + 180
            while time.monotonic() < deadline:
                states = json.loads(
                    run(
                        "docker",
                        "inspect",
                        *[f"{project}-{service}-1" for service in images],
                    )
                )
                if all(
                    item["State"]["Running"]
                    and item["State"].get("Health", {}).get("Status", "healthy")
                    == "healthy"
                    for item in states
                ):
                    break
                time.sleep(2)
            else:
                failed = [
                    item["Name"]
                    for item in states
                    if not item["State"]["Running"]
                    or item["State"].get("Health", {}).get("Status", "healthy")
                    != "healthy"
                ]
                raise RuntimeError("Restore health checks failed: " + ", ".join(failed))
            run(
                "docker",
                "exec",
                f"{project}-nginx-1",
                "wget",
                "-qO-",
                "http://127.0.0.1/api/v1/conferences?per_page=1",
            )
            run(
                "docker",
                "exec",
                f"{project}-django-admin-1",
                "python",
                "-c",
                "from console.client import call; call('APPLICATION_SVC_URL','GET','/internal/operations/queues'); call('ENGAGEMENT_SVC_URL','GET','/internal/operations/queues')",
            )
            print(
                f"Full isolated restore passed: {len(images)} containers, database, object storage, event stream, Redis and mTLS APIs"
            )
        finally:
            if not re.fullmatch(r"confhub_restore_[0-9]+_[0-9]+", project):
                raise RuntimeError("Unsafe cleanup project")
            try:
                run(*compose, "down", "--volumes", "--remove-orphans", cwd=back)
            except RuntimeError:
                print(
                    f"Cleanup needs attention for isolated project {project}",
                    flush=True,
                )


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Verify a backup in an isolated Docker stack"
    )
    parser.add_argument("archive", type=Path)
    parser.add_argument("production_project")
    args = parser.parse_args()
    try:
        verify(args.archive, args.production_project)
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as exc:
        # Compose stderr can include environment values; never emit it.
        parser.exit(
            1,
            f"Restore verification failed: {type(exc).__name__}: {str(exc) if not isinstance(exc, subprocess.CalledProcessError) else 'container operation failed'}\n",
        )
