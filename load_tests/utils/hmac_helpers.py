"""HMAC signature helpers for webhook authentication."""
import hmac
import hashlib


def generate_hmac_signature(secret: str, body: str) -> str:
    """
    Generate HMAC SHA256 signature for webhook payload.
    
    Args:
        secret: The tenant secret key (base64url encoded)
        body: The request body as string
        
    Returns:
        Hex-encoded HMAC signature
    """
    signature = hmac.new(
        secret.encode('utf-8'),
        body.encode('utf-8'),
        hashlib.sha256
    ).hexdigest()
    return signature


def format_signature_header(signature: str) -> str:
    """
    Format signature for X-Webhook-Signature header.
    
    Args:
        signature: Hex-encoded signature
        
    Returns:
        Formatted header value (sha256=<signature>)
    """
    return f"sha256={signature}"


def sign_webhook(secret: str, body: str) -> str:
    """
    Sign webhook body and return formatted header value.
    
    Args:
        secret: The tenant secret key
        body: The request body as string
        
    Returns:
        Formatted signature header value
    """
    signature = generate_hmac_signature(secret, body)
    return format_signature_header(signature)
