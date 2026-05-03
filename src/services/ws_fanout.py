"""WebSocket fan-out manager for efficient Redis Stream consumption.

Allows multiple WebSocket connections for the same tenant to share a single
XREAD operation, dramatically reducing Redis connection usage under load.
"""
import asyncio
import logging
from collections import defaultdict
from contextlib import asynccontextmanager
from typing import AsyncIterator
from uuid import UUID

from redis.asyncio import Redis

from src.services.event_streams import stream_tenant_events

logger = logging.getLogger(__name__)


class TenantFanout:
    """Manages fan-out of Redis Stream events to multiple WebSocket subscribers.
    
    A single XREAD loop feeds multiple queues, one per WebSocket connection.
    When the last subscriber disconnects, the XREAD loop is automatically stopped.
    """
    
    def __init__(self, redis: Redis, tenant_id: UUID):
        self.redis = redis
        self.tenant_id = tenant_id
        self.subscribers: dict[int, asyncio.Queue] = {}
        self.next_sub_id = 0
        self._reader_task: asyncio.Task | None = None
        self._lock = asyncio.Lock()
    
    async def _reader_loop(self) -> None:
        """Background task that reads from Redis Stream and fans out to all subscribers."""
        try:
            async with stream_tenant_events(
                self.redis, self.tenant_id, start_id="$"
            ) as messages:
                async for event in messages:
                    # Fan out to all active subscribers
                    dead_subs = []
                    for sub_id, queue in self.subscribers.items():
                        try:
                            queue.put_nowait(event)
                        except asyncio.QueueFull:
                            # Subscriber is too slow, disconnect it
                            logger.warning(
                                "Subscriber queue full, dropping",
                                extra={"tenant_id": str(self.tenant_id), "sub_id": sub_id}
                            )
                            dead_subs.append(sub_id)
                    
                    # Clean up dead subscribers
                    for sub_id in dead_subs:
                        self.subscribers.pop(sub_id, None)
        except asyncio.CancelledError:
            logger.debug(
                "Reader loop cancelled for tenant",
                extra={"tenant_id": str(self.tenant_id)}
            )
            raise
        except Exception:
            logger.exception(
                "Reader loop failed for tenant",
                extra={"tenant_id": str(self.tenant_id)}
            )
    
    @asynccontextmanager
    async def subscribe(self) -> AsyncIterator[asyncio.Queue]:
        """Subscribe to events for this tenant.
        
        Yields an asyncio.Queue that receives events from the shared XREAD loop.
        """
        async with self._lock:
            # Assign subscriber ID and create queue
            sub_id = self.next_sub_id
            self.next_sub_id += 1
            queue: asyncio.Queue = asyncio.Queue(maxsize=100)
            self.subscribers[sub_id] = queue
            
            # Start reader task if this is the first subscriber
            if self._reader_task is None or self._reader_task.done():
                self._reader_task = asyncio.create_task(self._reader_loop())
                logger.info(
                    "Started shared XREAD for tenant",
                    extra={"tenant_id": str(self.tenant_id)}
                )
        
        try:
            yield queue
        finally:
            # Cleanup on disconnect
            async with self._lock:
                self.subscribers.pop(sub_id, None)
                
                # Stop reader task if this was the last subscriber
                if not self.subscribers and self._reader_task and not self._reader_task.done():
                    self._reader_task.cancel()
                    try:
                        await self._reader_task
                    except asyncio.CancelledError:
                        pass
                    logger.info(
                        "Stopped shared XREAD for tenant (no subscribers)",
                        extra={"tenant_id": str(self.tenant_id)}
                    )


class FanoutManager:
    """Global manager for tenant fan-out instances.
    
    Maintains one TenantFanout per active tenant with WebSocket connections.
    """
    
    def __init__(self):
        self._fanouts: dict[UUID, TenantFanout] = {}
        self._lock = asyncio.Lock()
    
    async def get_fanout(self, redis: Redis, tenant_id: UUID) -> TenantFanout:
        """Get or create a fan-out manager for the given tenant."""
        async with self._lock:
            if tenant_id not in self._fanouts:
                self._fanouts[tenant_id] = TenantFanout(redis, tenant_id)
            return self._fanouts[tenant_id]
    
    async def cleanup_idle(self) -> None:
        """Remove fan-out managers with no active subscribers (optional cleanup)."""
        async with self._lock:
            idle = [
                tenant_id
                for tenant_id, fanout in self._fanouts.items()
                if not fanout.subscribers
            ]
            for tenant_id in idle:
                del self._fanouts[tenant_id]


# Global singleton
fanout_manager = FanoutManager()
