import base64
import hashlib
import hmac
import json
import time
from uuid import UUID

from src.config import settings


class InvalidToken(Exception):
    pass


def _b64url_encode(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode("ascii")


def _b64url_decode(value: str) -> bytes:
    padding = 4 - (len(value) % 4)
    if padding != 4:
        value = value + ("=" * padding)
    return base64.urlsafe_b64decode(value.encode("ascii"))


def _sign(body: str) -> str:
    digest = hmac.new(
        settings.APP_SECRET_KEY.encode("utf-8"),
        body.encode("ascii"),
        hashlib.sha256,
    ).digest()
    return _b64url_encode(digest)


def create_signed_token(tenant_id: UUID, ttl_seconds: int) -> str:
    payload = {
        "sub": str(tenant_id),
        "exp": int(time.time()) + ttl_seconds,
    }
    body = _b64url_encode(json.dumps(payload, separators=(",", ":")).encode("utf-8"))
    return f"{body}.{_sign(body)}"


def verify_signed_token(token: str) -> UUID:
    try:
        body, sig = token.split(".", 1)
    except ValueError as exc:
        raise InvalidToken("malformed token") from exc

    expected = _sign(body)
    if not hmac.compare_digest(sig, expected):
        raise InvalidToken("bad signature")

    try:
        payload = json.loads(_b64url_decode(body))
        exp = int(payload["exp"])
        sub = str(payload["sub"])
    except (ValueError, KeyError, TypeError) as exc:
        raise InvalidToken("malformed payload") from exc

    if exp < int(time.time()):
        raise InvalidToken("expired")

    try:
        return UUID(sub)
    except ValueError as exc:
        raise InvalidToken("invalid sub") from exc
