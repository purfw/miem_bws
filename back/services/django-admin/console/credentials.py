import json
import fcntl
import re
import os
import tempfile
import uuid
from dataclasses import asdict, dataclass
from functools import lru_cache
from contextlib import contextmanager
from dataclasses import replace
from pathlib import Path

import pyotp
from cryptography.fernet import Fernet
from django.contrib.auth.hashers import Argon2PasswordHasher


class AdminPasswordHasher(Argon2PasswordHasher):
    time_cost = 3
    memory_cost = 64 * 1024
    parallelism = 4


@dataclass(frozen=True)
class Credentials:
    username: str
    password_hash: str
    totp_secret: str
    actor_id: str
    version: str
    roles: tuple[str, ...] = ("superadmin",)
    active: bool = True

    def validate(self):
        if not re.fullmatch(
            r"[A-Za-z0-9_.@-]{1,80}", self.username
        ) or not self.password_hash.startswith(("argon2$", "pbkdf2_sha256$")):
            raise ValueError("Invalid administrator identity or password hash")
        uuid.UUID(self.actor_id)
        uuid.UUID(self.version)
        if len(self.totp_secret) < 32:
            raise ValueError(
                "Administrator TOTP secret must contain at least 160 bits"
            )
        pyotp.TOTP(self.totp_secret).at(0)
        if not self.roles or any(
            role not in ROLE_PERMISSIONS for role in self.roles
        ):
            raise ValueError("Invalid administrator roles")
        if not isinstance(self.active, bool):
            raise ValueError("Invalid administrator status")

    def permits(self, permission):
        return self.active and any(
            permission in ROLE_PERMISSIONS[role] for role in self.roles
        )


ROLE_PERMISSIONS = {
    "superadmin": {
        "read",
        "moderate",
        "accounts",
        "operations",
        "audit",
        "manage_admins",
    },
    "moderator": {"read", "moderate", "audit"},
    "operator": {"read", "operations"},
    "auditor": {"read", "audit"},
}


def load_credentials(auth_file, key_file):
    return next(
        account
        for account in load_accounts(auth_file, key_file)
        if account.active and "superadmin" in account.roles
    )


def load_accounts(auth_file, key_file):
    plaintext = Fernet(Path(key_file).read_bytes().strip()).decrypt(
        Path(auth_file).read_bytes()
    )
    payload = json.loads(plaintext)
    records = payload.get("accounts", [payload])
    accounts = []
    for record in records:
        record = dict(record)
        record["roles"] = tuple(record.get("roles", ("superadmin",)))
        account = Credentials(**record)
        account.validate()
        accounts.append(account)
    if (
        not 1 <= len(accounts) <= 50
        or len({a.username for a in accounts}) != len(accounts)
        or len({a.actor_id for a in accounts}) != len(accounts)
    ):
        raise ValueError("Invalid administrator account list")
    if not any(a.active and "superadmin" in a.roles for a in accounts):
        raise ValueError("At least one active superadmin is required")
    return accounts


def _atomic_write(path, data, mode=0o640):
    path = Path(path)
    fd, temporary = tempfile.mkstemp(
        prefix="." + path.name + ".", dir=path.parent
    )
    try:
        with os.fdopen(fd, "wb") as stream:
            os.fchmod(stream.fileno(), mode)
            if os.getuid() == 0:
                os.fchown(stream.fileno(), 0, 0)
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


@contextmanager
def _store_lock(directory):
    directory = Path(directory)
    directory.mkdir(parents=True, exist_ok=True, mode=0o2770)
    if directory.stat().st_uid == os.getuid():
        os.chmod(directory, 0o2770)
    with (directory / ".lock").open("a+b") as lock:
        if os.fstat(lock.fileno()).st_uid == os.getuid():
            os.fchmod(lock.fileno(), 0o660)
        fcntl.flock(lock, fcntl.LOCK_EX)
        yield directory


def save_credentials(directory, credentials, *, create_only=False):
    credentials.validate()
    with _store_lock(directory) as directory:
        accounts = (
            load_accounts(directory / "credentials.enc", directory / "key")
            if (directory / "credentials.enc").exists()
            else []
        )
        if create_only and any(
            a.username == credentials.username for a in accounts
        ):
            raise ValueError("Этот логин уже занят")
        accounts = [
            account
            for account in accounts
            if account.actor_id != credentials.actor_id
        ]
        accounts.append(credentials)
        _save_accounts(directory, accounts)


def change_account(
    directory, username, *, roles=None, active=None, version=None
):
    with _store_lock(directory) as directory:
        accounts = load_accounts(
            directory / "credentials.enc", directory / "key"
        )
        account = next((a for a in accounts if a.username == username), None)
        if account is None:
            raise ValueError("Учётная запись не найдена")
        if version is not None and account.version != version:
            raise ValueError("Права уже изменились. Обновите страницу")
        updated = replace(
            account,
            roles=tuple(roles) if roles is not None else account.roles,
            active=active if active is not None else account.active,
            version=str(uuid.uuid4()),
        )
        updated.validate()
        _save_accounts(
            directory,
            [updated if a.username == username else a for a in accounts],
        )
        return updated


def _save_accounts(directory, accounts):
    if not any(a.active and "superadmin" in a.roles for a in accounts):
        raise ValueError(
            "The last active superadmin cannot be disabled or demoted"
        )
    if len(accounts) > 50 or len({a.username for a in accounts}) != len(
        accounts
    ):
        raise ValueError("Duplicate username or account limit reached")
    key_file = directory / "key"
    auth_file = directory / "credentials.enc"
    if key_file.exists():
        key = key_file.read_bytes().strip()
        if key_file.stat().st_uid == os.getuid():
            os.chmod(key_file, 0o640)
        if os.getuid() == 0:
            os.chown(key_file, 0, 0)
    else:
        key = Fernet.generate_key()
        _atomic_write(key_file, key)
    cipher = Fernet(key).encrypt(
        json.dumps({"accounts": [asdict(a) for a in accounts]}).encode()
    )
    _atomic_write(auth_file, cipher)
    if load_accounts(auth_file, key_file) != accounts:
        raise ValueError("Administrator credentials verification failed")


@lru_cache(maxsize=8)
def _cached_accounts(auth_file, key_file, revision):
    return load_accounts(auth_file, key_file)


def get_credentials(username=None):
    auth_file, key_file = (
        os.environ["ADMIN_AUTH_FILE"],
        os.environ["ADMIN_AUTH_KEY_FILE"],
    )
    revision = tuple(
        (
            os.stat(path).st_ino,
            os.stat(path).st_mtime_ns,
            os.stat(path).st_size,
        )
        for path in (auth_file, key_file)
    )
    accounts = _cached_accounts(auth_file, key_file, revision)
    if username is None:
        return next(
            account
            for account in accounts
            if account.active and "superadmin" in account.roles
        )
    return next(
        (
            account
            for account in accounts
            if account.username == username and account.active
        ),
        None,
    )


get_credentials.cache_clear = _cached_accounts.cache_clear
