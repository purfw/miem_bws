import os
from dataclasses import replace

import pytest
import pyotp
from cryptography.fernet import Fernet, InvalidToken

from console.credentials import load_credentials, save_credentials
from console.credentials_tool import migrate
from console import credentials_tool


def test_store_does_not_contain_plaintext_or_seed(credentials, tmp_path):
    directory = tmp_path / "vault"
    save_credentials(directory, credentials)
    cipher = (directory / "credentials.enc").read_bytes()
    assert b"Test-password-2026" not in cipher
    assert credentials.password_hash.encode() not in cipher
    assert credentials.totp_secret.encode() not in cipher
    assert (
        load_credentials(directory / "credentials.enc", directory / "key")
        == credentials
    )
    assert os.stat(directory).st_mode & 0o7777 == 0o2770
    assert os.stat(directory / "key").st_mode & 0o777 == 0o640


def test_store_detects_ciphertext_tampering(credentials, tmp_path):
    directory = tmp_path / "vault"
    save_credentials(directory, credentials)
    cipher = bytearray((directory / "credentials.enc").read_bytes())
    cipher[-12] ^= 1
    (directory / "credentials.enc").write_bytes(cipher)
    with pytest.raises(InvalidToken):
        load_credentials(directory / "credentials.enc", directory / "key")


def test_wrong_key_cannot_decrypt_store(credentials, tmp_path):
    directory = tmp_path / "vault"
    save_credentials(directory, credentials)
    other_key = tmp_path / "other-key"
    other_key.write_bytes(Fernet.generate_key())
    with pytest.raises(InvalidToken):
        load_credentials(directory / "credentials.enc", other_key)


def test_migration_removes_legacy_secret_file_and_environment(
    credentials, tmp_path
):
    secrets_dir = tmp_path / "secrets"
    secrets_dir.mkdir()
    legacy = secrets_dir / "admin-credentials.txt"
    legacy.write_text(
        "password=old-password\ntotp_secret=" + credentials.totp_secret
    )
    environment = tmp_path / ".env.prod"
    environment.write_text(
        "POSTGRES_PASSWORD=preserve-service-secret\n"
        f"ADMIN_USERNAME={credentials.username}\n"
        f"ADMIN_PASSWORD_HASH='{credentials.password_hash}'\n"
        f"ADMIN_TOTP_SECRET={credentials.totp_secret}\n"
        f"ADMIN_ACTOR_ID={credentials.actor_id}\n"
    )
    migrate(environment)
    assert not legacy.exists()
    assert (
        environment.read_text()
        == "POSTGRES_PASSWORD=preserve-service-secret\n"
    )
    assert os.stat(environment).st_mode & 0o777 == 0o600
    directory = secrets_dir / "admin-auth"
    migrated = load_credentials(
        directory / "credentials.enc", directory / "key"
    )
    assert replace(migrated, version=credentials.version) == credentials
    original_cipher = (directory / "credentials.enc").read_bytes()
    migrate(environment)
    assert (directory / "credentials.enc").read_bytes() == original_cipher


def test_failed_migration_keeps_existing_credentials(credentials, tmp_path):
    directory = tmp_path / "secrets"
    directory.mkdir()
    legacy = directory / "admin-credentials.txt"
    legacy.write_text("password=keep-until-migration-succeeds")
    environment = tmp_path / ".env.prod"
    content = "ADMIN_USERNAME=admin\nADMIN_TOTP_SECRET=INVALID\n"
    environment.write_text(content)
    with pytest.raises(KeyError):
        migrate(environment)
    assert legacy.exists()
    assert environment.read_text() == content


def test_setup_requires_an_interactive_terminal(tmp_path, monkeypatch):
    monkeypatch.setattr(credentials_tool.sys.stdin, "isatty", lambda: False)
    with pytest.raises(ValueError, match="interactive"):
        credentials_tool.setup(tmp_path / ".env.prod")
    assert not (tmp_path / "secrets").exists()


def test_setup_rotates_seed_and_version_without_saving_uri(
    credentials, tmp_path, monkeypatch, capsys
):
    directory = tmp_path / "secrets" / "admin-auth"
    save_credentials(directory, credentials)
    environment = tmp_path / ".env.prod"
    environment.write_text("SERVICE_TOKEN_SECRET=preserve-this-value\n")
    new_seed = pyotp.random_base32()
    monkeypatch.setattr(credentials_tool.sys.stdin, "isatty", lambda: True)
    monkeypatch.setattr(credentials_tool.sys.stdout, "isatty", lambda: True)
    monkeypatch.setattr("builtins.input", lambda prompt: "")
    monkeypatch.setattr(
        credentials_tool.pyotp, "random_base32", lambda: new_seed
    )
    monkeypatch.setattr(
        credentials_tool.getpass,
        "getpass",
        lambda prompt: pyotp.TOTP(new_seed).now()
        if prompt.startswith("Current")
        else "New-password-2026",
    )
    credentials_tool.setup(environment)
    updated = load_credentials(
        directory / "credentials.enc", directory / "key"
    )
    assert updated.totp_secret == new_seed != credentials.totp_secret
    assert updated.version != credentials.version
    assert updated.actor_id == credentials.actor_id
    assert updated.password_hash.startswith("argon2$")
    output = capsys.readouterr().out
    assert output.count("otpauth://") == 1
    assert "New-password-2026" not in output
    assert b"otpauth://" not in (directory / "credentials.enc").read_bytes()
    assert (
        environment.read_text() == "SERVICE_TOKEN_SECRET=preserve-this-value\n"
    )


def test_failed_setup_confirmation_preserves_previous_store(
    credentials, tmp_path, monkeypatch
):
    directory = tmp_path / "secrets" / "admin-auth"
    save_credentials(directory, credentials)
    original = (directory / "credentials.enc").read_bytes()
    environment = tmp_path / ".env.prod"
    content = "SERVICE_TOKEN_SECRET=preserve-this-value\n"
    environment.write_text(content)
    monkeypatch.setattr(credentials_tool.sys.stdin, "isatty", lambda: True)
    monkeypatch.setattr(credentials_tool.sys.stdout, "isatty", lambda: True)
    monkeypatch.setattr("builtins.input", lambda prompt: "")
    monkeypatch.setattr(
        credentials_tool.getpass,
        "getpass",
        lambda prompt: "not-a-code"
        if prompt.startswith("Current")
        else "New-password-2026",
    )
    with pytest.raises(ValueError, match="confirmation failed"):
        credentials_tool.setup(environment)
    assert (directory / "credentials.enc").read_bytes() == original
    assert environment.read_text() == content
