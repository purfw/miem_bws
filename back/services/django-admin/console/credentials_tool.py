import argparse
import os
import getpass
import re
import sys
import uuid
from pathlib import Path

import pyotp

from .credentials import (
    AdminPasswordHasher,
    Credentials,
    _atomic_write,
    load_credentials,
    load_accounts,
    save_credentials,
    change_account,
)

LEGACY_KEYS = {
    "ADMIN_USERNAME",
    "ADMIN_PASSWORD_HASH",
    "ADMIN_TOTP_SECRET",
    "ADMIN_ACTOR_ID",
    "ADMIN_PASSWORD",
    "ADMIN_OTPAUTH_URI",
}


def read_environment(path):
    lines = path.read_text().splitlines(keepends=True)
    values = {}
    for line in lines:
        if "=" in line and not line.lstrip().startswith("#"):
            key, value = line.strip().split("=", 1)
            values[key] = value.strip().strip("'\"")
    return lines, values


def scrub_environment(path, lines):
    clean = "".join(
        line
        for line in lines
        if line.strip().split("=", 1)[0] not in LEGACY_KEYS
    )
    _atomic_write(path, clean.encode(), mode=0o600)


def migrate(env_file):
    lines, values = read_environment(env_file)
    directory = env_file.parent / "secrets" / "admin-auth"
    if (directory / "credentials.enc").exists():
        load_credentials(directory / "credentials.enc", directory / "key")
    else:
        credentials = Credentials(
            username=values["ADMIN_USERNAME"],
            password_hash=values["ADMIN_PASSWORD_HASH"],
            totp_secret=values["ADMIN_TOTP_SECRET"],
            actor_id=values["ADMIN_ACTOR_ID"],
            version=str(uuid.uuid4()),
        )
        save_credentials(directory, credentials)
    # Remove legacy plaintext only after the encrypted store was read back.
    os.chmod(directory, 0o2770)
    if (directory / ".lock").exists():
        os.chmod(directory / ".lock", 0o660)
    scrub_environment(env_file, lines)
    (env_file.parent / "secrets" / "admin-credentials.txt").unlink(
        missing_ok=True
    )


def setup(env_file, new_account=False, roles=("moderator",)):
    if not sys.stdin.isatty() or not sys.stdout.isatty():
        raise ValueError(
            "Run setup from an interactive SSH terminal; "
            "do not pipe or log secrets"
        )
    lines, values = read_environment(env_file)
    directory = env_file.parent / "secrets" / "admin-auth"
    old = None
    if (directory / "credentials.enc").exists() and not new_account:
        old = load_credentials(
            directory / "credentials.enc", directory / "key"
        )
    default_username = (
        old.username
        if old
        else ("" if new_account else values.get("ADMIN_USERNAME", "admin"))
    )
    username = (
        input(f"Administrator username [{default_username}]: ").strip()
        or default_username
    )
    if not re.fullmatch(r"[A-Za-z0-9_.@-]{1,80}", username):
        raise ValueError("Invalid username")
    password = getpass.getpass(
        "New administrator password (at least 12 characters): "
    )
    if len(password) < 12:
        raise ValueError("Password must contain at least 12 characters")
    if password != getpass.getpass("Repeat password: "):
        raise ValueError("Passwords do not match")
    hasher = AdminPasswordHasher()
    password_hash = hasher.encode(password, hasher.salt())
    del password
    totp = pyotp.TOTP(pyotp.random_base32())
    print(
        "Add this URI to your authenticator now. "
        "It is shown once and is not saved:"
    )
    print(totp.provisioning_uri(name=username, issuer_name="ConfHub"))
    otp = getpass.getpass("Current six-digit authenticator code: ").strip()
    if not re.fullmatch(r"[0-9]{6}", otp) or not totp.verify(
        otp, valid_window=1
    ):
        raise ValueError(
            "Authenticator confirmation failed; previous credentials were kept"
        )
    credentials = Credentials(
        username=username,
        password_hash=password_hash,
        totp_secret=totp.secret,
        actor_id=old.actor_id
        if old
        else values.get("ADMIN_ACTOR_ID", str(uuid.uuid4())),
        version=str(uuid.uuid4()),
        roles=roles
        if new_account
        else (old.roles if old else ("superadmin",)),
    )
    save_credentials(directory, credentials)
    scrub_environment(env_file, lines)
    (env_file.parent / "secrets" / "admin-credentials.txt").unlink(
        missing_ok=True
    )
    print(
        "Administrator account saved. "
        "Previous sessions for this account are revoked."
    )


def update_account(env_file, username, roles=None, active=None):
    directory = env_file.parent / "secrets" / "admin-auth"
    change_account(directory, username, roles=roles, active=active)
    print("Account updated; its previous sessions are revoked.")


def main():
    parser = argparse.ArgumentParser(
        description="Configure encrypted administrator authentication"
    )
    parser.add_argument(
        "command",
        choices=(
            "migrate",
            "setup",
            "add",
            "list",
            "role",
            "disable",
            "enable",
        ),
    )
    parser.add_argument("--env-file", required=True, type=Path)
    parser.add_argument("--username")
    parser.add_argument("--roles", default="moderator")
    args = parser.parse_args()
    try:
        env_file = args.env_file.resolve()
        roles = tuple(args.roles.split(","))
        if args.command in ("migrate", "setup"):
            {"migrate": migrate, "setup": setup}[args.command](env_file)
        elif args.command == "add":
            setup(env_file, new_account=True, roles=roles)
        elif args.command == "list":
            directory = env_file.parent / "secrets" / "admin-auth"
            for account in load_accounts(
                directory / "credentials.enc", directory / "key"
            ):
                print(
                    f"{account.username}\t{','.join(account.roles)}\t"
                    f"{'active' if account.active else 'disabled'}"
                )
        else:
            if not args.username:
                raise ValueError("--username is required")
            update_account(
                env_file,
                args.username,
                roles=roles if args.command == "role" else None,
                active=args.command == "enable"
                if args.command in ("disable", "enable")
                else None,
            )
    except (ValueError, KeyError, OSError) as exc:
        parser.exit(1, f"Administrator setup failed: {exc}\n")


if __name__ == "__main__":
    main()
