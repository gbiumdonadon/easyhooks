import hashlib
import hmac
import secrets

from passlib.context import CryptContext

from src.config import settings

pwd_context = CryptContext(schemes=["bcrypt"], deprecated="auto")


def generate_secret_key(nbytes: int | None = None) -> str:
    return secrets.token_urlsafe(nbytes or settings.SECRET_KEY_BYTES)


def hash_secret(raw_secret: str) -> str:
    return pwd_context.hash(raw_secret)


def verify_secret(raw_secret: str, hashed: str) -> bool:
    return pwd_context.verify(raw_secret, hashed)


def verify_hmac_signature(secret_raw: str, body: bytes, signature: str) -> bool:
    """Verifica assinatura no formato 'sha256=<hex>' (compatível GitHub).

    Aceita também o hex puro como fallback. Comparação em tempo constante
    via hmac.compare_digest para mitigar timing attacks.
    """
    if not signature:
        return False

    provided = signature.strip()
    if provided.startswith("sha256="):
        provided = provided[len("sha256="):]

    expected = hmac.new(
        secret_raw.encode("utf-8"),
        body,
        hashlib.sha256,
    ).hexdigest()

    return hmac.compare_digest(provided.lower(), expected)
