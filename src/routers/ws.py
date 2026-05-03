import asyncio
import logging
from uuid import UUID

from fastapi import APIRouter, Depends, Query, WebSocket, WebSocketDisconnect

from src.config import settings
from src.redis_client import get_redis
from src.services.event_streams import read_tenant_history, stream_tenant_events
from src.services.ws_fanout import fanout_manager
from src.services.ws_token import InvalidToken, verify_signed_token

router = APIRouter(tags=["ws"])
logger = logging.getLogger(__name__)

# WebSocket heartbeat interval in seconds
WS_HEARTBEAT_INTERVAL = 30


def authenticate_ws_connection(tenant_id: UUID, token: str | None) -> UUID:
    if not token:
        raise InvalidToken("missing token")
    token_tenant = verify_signed_token(token)
    if token_tenant != tenant_id:
        raise InvalidToken("tenant mismatch")
    return token_tenant


async def _heartbeat(websocket: WebSocket, interval: int = WS_HEARTBEAT_INTERVAL) -> None:
    """Send periodic ping messages to keep WebSocket connection alive.
    
    This helps detect dead connections early and prevents timeouts
    during periods of low activity.
    """
    try:
        while True:
            await asyncio.sleep(interval)
            await websocket.send_json({"type": "ping", "timestamp": asyncio.get_event_loop().time()})
    except Exception as exc:
        logger.debug("Heartbeat task stopped", extra={"error": str(exc)})
        # Task will be cancelled when connection closes, this is expected


@router.websocket("/ws/events/{tenant_id}")
async def ws_events(
    websocket: WebSocket,
    tenant_id: UUID,
    token: str | None = Query(None),
    redis=Depends(get_redis),
):
    try:
        authenticate_ws_connection(tenant_id, token)
    except InvalidToken as exc:
        await websocket.close(code=1008, reason=str(exc))
        return

    await websocket.accept()
    
    # 1. Send historical events (most recent N events from Redis Stream)
    try:
        history = await read_tenant_history(redis, tenant_id)
        for event in history:
            await websocket.send_text(event["payload"].decode() if isinstance(event["payload"], bytes) else event["payload"])
        
        logger.info(
            "Sent historical events to WebSocket client",
            extra={"tenant_id": str(tenant_id), "count": len(history)}
        )
    except Exception as exc:
        logger.warning(
            "Failed to send history to WebSocket client",
            extra={"tenant_id": str(tenant_id), "error": str(exc)}
        )
    
    # 2. Stream new events (only messages published after connection)
    if settings.WS_USE_FANOUT:
        # Use fan-out: multiple connections share a single XREAD
        fanout = await fanout_manager.get_fanout(redis, tenant_id)
        async with fanout.subscribe() as event_queue:
            async def _forward() -> None:
                try:
                    while True:
                        event = await event_queue.get()
                        payload = event["payload"]
                        if isinstance(payload, bytes):
                            payload = payload.decode()
                        await websocket.send_text(payload)
                except asyncio.CancelledError:
                    raise
                except Exception:
                    logger.exception("ws forwarder failed for tenant %s", tenant_id)

            forwarder = asyncio.create_task(_forward())
            heartbeat_task = asyncio.create_task(_heartbeat(websocket))
            
            try:
                while True:
                    await websocket.receive_text()
            except WebSocketDisconnect:
                return
            finally:
                # Cancel both tasks on disconnect
                forwarder.cancel()
                heartbeat_task.cancel()
                try:
                    await forwarder
                except asyncio.CancelledError:
                    pass
                try:
                    await heartbeat_task
                except asyncio.CancelledError:
                    pass
    else:
        # Legacy mode: each connection has its own XREAD
        async with stream_tenant_events(redis, tenant_id, start_id="$") as messages:
            async def _forward() -> None:
                try:
                    async for event in messages:
                        payload = event["payload"]
                        if isinstance(payload, bytes):
                            payload = payload.decode()
                        await websocket.send_text(payload)
                except asyncio.CancelledError:
                    raise
                except Exception:
                    logger.exception("ws forwarder failed for tenant %s", tenant_id)

            forwarder = asyncio.create_task(_forward())
            heartbeat_task = asyncio.create_task(_heartbeat(websocket))
            
            try:
                while True:
                    await websocket.receive_text()
            except WebSocketDisconnect:
                return
            finally:
                # Cancel both tasks on disconnect
                forwarder.cancel()
                heartbeat_task.cancel()
                try:
                    await forwarder
                except asyncio.CancelledError:
                    pass
                try:
                    await heartbeat_task
                except asyncio.CancelledError:
                    pass
