"""Redis Streams service for tenant event distribution with history support.

Replaces Pub/Sub with persistent streams that allow retroactive event delivery.
"""
import contextlib
import logging
from collections.abc import AsyncIterator
from uuid import UUID

from redis.asyncio import Redis

from src.config import settings

logger = logging.getLogger(__name__)


def tenant_stream_key(tenant_id: UUID | str) -> str:
    """Returns the Redis Stream key for a tenant."""
    return f"{settings.TENANT_EVENTS_STREAM_PREFIX}{tenant_id}"


async def publish_tenant_event(
    redis: Redis,
    tenant_id: UUID | str,
    payload: bytes,
) -> str:
    """Publish an event to a tenant's Redis Stream.
    
    Uses XADD with MAXLEN ~ to cap stream size (approximate trimming).
    
    Args:
        redis: Redis client
        tenant_id: Tenant UUID or string
        payload: Event payload (raw bytes)
    
    Returns:
        Message ID (e.g., '1234567890123-0')
    """
    stream_key = tenant_stream_key(tenant_id)
    
    # XADD stream:tenant:{id} MAXLEN ~ 1000 * payload {payload}
    # The "~" makes trimming approximate for better performance
    msg_id = await redis.xadd(
        stream_key,
        {"payload": payload},
        maxlen=settings.STREAM_MAX_LEN,
        approximate=True,
    )
    
    # msg_id is bytes, decode to string
    if isinstance(msg_id, bytes):
        msg_id = msg_id.decode()
    
    return msg_id


async def read_tenant_history(
    redis: Redis,
    tenant_id: UUID | str,
    count: int | None = None,
) -> list[dict]:
    """Read the most recent events from a tenant's stream.
    
    Uses XREVRANGE to read in reverse chronological order (newest first).
    
    Args:
        redis: Redis client
        tenant_id: Tenant UUID or string
        count: Maximum number of events to retrieve (defaults to STREAM_HISTORY_COUNT)
    
    Returns:
        List of dicts with keys:
        - id: Message ID (str)
        - payload: Event payload (bytes)
        
        Ordered from oldest to newest (reversed from XREVRANGE result).
    """
    stream_key = tenant_stream_key(tenant_id)
    if count is None:
        count = settings.STREAM_HISTORY_COUNT
    
    # XREVRANGE stream:tenant:{id} + - COUNT {count}
    # Returns: [(id, {field: value}), ...]
    # + = latest, - = oldest
    results = await redis.xrevrange(stream_key, "+", "-", count=count)
    
    if not results:
        return []
    
    # Parse results and reverse to get chronological order (oldest first)
    events = []
    for msg_id, fields in reversed(results):
        # msg_id is bytes, fields is dict of bytes
        if isinstance(msg_id, bytes):
            msg_id = msg_id.decode()
        
        payload = fields.get(b"payload") or fields.get("payload")
        if payload is None:
            logger.warning(
                "Event in stream missing payload field",
                extra={"tenant_id": str(tenant_id), "msg_id": msg_id}
            )
            continue
        
        events.append({
            "id": msg_id,
            "payload": payload if isinstance(payload, bytes) else payload.encode(),
        })
    
    return events


@contextlib.asynccontextmanager
async def stream_tenant_events(
    redis: Redis,
    tenant_id: UUID | str,
    start_id: str = "$",
    block_ms: int = 5000,
) -> AsyncIterator[AsyncIterator[dict]]:
    """Stream new events from a tenant's Redis Stream.
    
    Context manager that yields an async iterator of events.
    Uses XREAD with BLOCK to wait for new messages.
    
    Args:
        redis: Redis client
        tenant_id: Tenant UUID or string
        start_id: Starting message ID:
            - "$" = only new messages
            - "0" = all messages from beginning
            - specific ID = messages after that ID
        block_ms: Block timeout in milliseconds
    
    Yields:
        AsyncIterator that yields dicts with:
        - id: Message ID (str)
        - payload: Event payload (bytes)
    """
    stream_key = tenant_stream_key(tenant_id)
    current_id = start_id
    
    async def _messages() -> AsyncIterator[dict]:
        nonlocal current_id
        
        while True:
            # XREAD BLOCK {ms} STREAMS {key} {id}
            # Returns: [[stream_key, [(id, fields), ...]], ...]
            try:
                results = await redis.xread(
                    {stream_key: current_id},
                    block=block_ms,
                )
            except Exception as exc:
                logger.warning(
                    "XREAD failed for tenant stream",
                    extra={"tenant_id": str(tenant_id), "error": str(exc)}
                )
                break
            
            if not results:
                # Timeout, no new messages
                continue
            
            # Parse results: results[0] = (stream_key, [(msg_id, fields), ...])
            for _stream, messages in results:
                for msg_id, fields in messages:
                    if isinstance(msg_id, bytes):
                        msg_id = msg_id.decode()
                    
                    payload = fields.get(b"payload") or fields.get("payload")
                    if payload is None:
                        logger.warning(
                            "Event in stream missing payload field",
                            extra={"tenant_id": str(tenant_id), "msg_id": msg_id}
                        )
                        continue
                    
                    # Update current_id to resume from this point if reconnecting
                    current_id = msg_id
                    
                    yield {
                        "id": msg_id,
                        "payload": payload if isinstance(payload, bytes) else payload.encode(),
                    }
    
    try:
        yield _messages()
    finally:
        # No cleanup needed for XREAD (no subscription state like Pub/Sub)
        pass
