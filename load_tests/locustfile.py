"""
Main Locust load testing file for EasyHooks.

This file contains the primary user classes for load testing:
- WebhookUser: Simulates clients sending webhooks via HTTP
- WebSocketUser: Simulates clients listening for events via WebSocket

Tenant pool lifecycle
---------------------
Tenants are created via the application's own API before any webhook is
dispatched.  The module-level `_pool_ready` Event coordinates this:

- Master / standalone: creates tenants in `on_locust_init` then sets the event.
- Workers (distributed mode): poll the shared pool file in `on_locust_init`
  until the master writes it, then set the event locally.
- Both `WebhookUser.on_start` and `WebSocketUser.on_start` block on
  `_pool_ready.wait()` before acquiring a tenant, guaranteeing that no
  webhook request is sent until the pool is fully populated.
"""
import json
import sys
import threading
import time
import uuid
from locust import HttpUser, task, between, events
from websocket import create_connection, WebSocketTimeoutException, WebSocketException

from utils.tenant_factory import get_tenant_from_pool
from utils.hmac_helpers import sign_webhook
from utils.metrics_collector import collector
from config import settings

# Signals that the tenant pool is populated and users may start sending requests.
# Set by on_locust_init on master/standalone; workers block until the pool file
# appears on the shared volume and then set it locally.
_pool_ready = threading.Event()

# How long (seconds) on_start waits for the pool before raising.
_POOL_WAIT_TIMEOUT = 120


def _abort_locust(environment, reason: str) -> None:
    """
    Stop Locust immediately with a fatal error message.

    Called when a pre-condition for the test cannot be met (e.g. tenant
    creation failed).  Uses the runner's quit() when available, otherwise
    falls back to sys.exit so the process always terminates with a non-zero
    exit code, preventing docker-compose from marking the init step as
    successful.
    """
    print("\n" + "!" * 72)
    print("[FATAL] Locust abortado — pré-condição não atendida:")
    print(f"  {reason}")
    print("!" * 72 + "\n")

    runner = getattr(environment, "runner", None)
    if runner is not None:
        try:
            runner.quit()
            return
        except Exception:
            pass

    sys.exit(1)


class WebhookUser(HttpUser):
    """
    User that sends webhook events via HTTP POST.

    Simulates external systems posting webhook events to the platform.
    Each user is assigned a tenant from the pool.
    """

    wait_time = between(0.1, 0.5)

    def on_start(self):
        """
        Initialize user with tenant credentials.

        Blocks until the tenant pool is ready (created via the app API)
        before acquiring a tenant, ensuring no webhook is sent prematurely.
        """
        if not _pool_ready.wait(timeout=_POOL_WAIT_TIMEOUT):
            raise RuntimeError(
                f"Tenant pool not ready after {_POOL_WAIT_TIMEOUT}s. "
                "Check LOADTEST_ADMIN_TOKEN and LOADTEST_API_BASE_URL."
            )
        self.tenant_id, self.secret = get_tenant_from_pool()
        self.event_counter = 0
    
    @task(10)
    def send_webhook(self):
        """
        Send a webhook event (most common task).
        
        Weight: 10 (main operation)
        """
        event_id = f"evt-{uuid.uuid4()}"
        payload = {
            "event": "order.created",
            "data": {
                "order_id": self.event_counter,
                "timestamp": time.time()
            }
        }
        body = json.dumps(payload)
        signature = sign_webhook(self.secret, body)
        
        start_time = time.time()
        
        with self.client.post(
            f"/v1/webhooks/{self.tenant_id}",
            headers={
                "X-Webhook-Signature": signature,
                "X-Event-Id": event_id,
                "Content-Type": "application/json"
            },
            data=body,
            catch_response=True,
            name="/v1/webhooks/[tenant_id]"
        ) as response:
            if response.status_code == 202:
                response.success()
                
                # Record custom metric
                duration = time.time() - start_time
                collector.record_histogram(
                    "webhook_ingestion_latency",
                    duration,
                    {"tenant_id": self.tenant_id[:8]}
                )
                collector.increment_counter(
                    "webhooks_sent",
                    labels={"tenant_id": self.tenant_id[:8]}
                )
            else:
                response.failure(f"Got status {response.status_code}: {response.text}")
        
        self.event_counter += 1
    
    @task(1)
    def send_webhook_batch(self):
        """
        Send multiple webhooks in quick succession.
        
        Weight: 1 (occasional burst)
        """
        for _ in range(5):
            self.send_webhook()


class WebSocketUser(HttpUser):
    """
    User that maintains a WebSocket connection to receive events.

    Simulates clients listening for real-time webhook delivery.
    Each user maintains one persistent WebSocket connection.
    """

    wait_time = between(1, 5)

    def on_start(self):
        """
        Initialize user with tenant credentials and connect WebSocket.

        Blocks until the tenant pool is ready before acquiring a tenant,
        ensuring no connection is attempted before tenants exist in the API.
        """
        if not _pool_ready.wait(timeout=_POOL_WAIT_TIMEOUT):
            raise RuntimeError(
                f"Tenant pool not ready after {_POOL_WAIT_TIMEOUT}s. "
                "Check LOADTEST_ADMIN_TOKEN and LOADTEST_API_BASE_URL."
            )
        self.tenant_id, self.secret = get_tenant_from_pool()
        self.ws = None
        self.connected = False
        self.messages_received = 0
        self.connect_websocket()
    
    def connect_websocket(self):
        """Connect to WebSocket endpoint."""
        try:
            # First, get WS token
            start_time = time.time()
            response = self.client.post(
                f"/v1/tokens/{self.tenant_id}",
                headers={"Authorization": f"Bearer {self.secret}"},
                name="/v1/tokens/[tenant_id]"
            )
            
            if response.status_code != 200:
                print(f"Failed to get WS token: {response.status_code} {response.text}")
                return
            
            token = response.json()["token"]
            
            # Connect WebSocket using the configured host, not hardcoded localhost
            ws_base = settings.API_BASE_URL.replace("http://", "ws://").replace("https://", "wss://")
            ws_url = f"{ws_base}/ws/events/{self.tenant_id}?token={token}"
            self.ws = create_connection(ws_url, timeout=10)
            self.connected = True
            
            # Record connection time
            duration = time.time() - start_time
            collector.record_histogram("websocket_connect_time", duration)
            collector.increment_counter("websocket_connections")
            
        except Exception as e:
            print(f"Failed to connect WebSocket: {e}")
            self.connected = False
            collector.increment_counter("websocket_connection_errors")
    
    @task
    def listen_events(self):
        """
        Listen for incoming events on WebSocket.
        
        This is the main task - keeping connection alive and receiving messages.
        """
        if not self.connected or not self.ws:
            # Try to reconnect
            self.connect_websocket()
            return
        
        try:
            # Try to receive message (non-blocking with timeout)
            start_time = time.time()
            message = self.ws.recv()
            duration = time.time() - start_time
            
            if message:
                self.messages_received += 1
                
                # Parse and extract timestamp if available
                try:
                    data = json.loads(message)
                    sent_timestamp = data.get("data", {}).get("timestamp")
                    
                    if sent_timestamp:
                        # Calculate end-to-end latency (webhook POST → WS receive)
                        e2e_latency = time.time() - sent_timestamp
                        collector.record_histogram(
                            "websocket_e2e_latency",
                            e2e_latency,
                            {"tenant_id": self.tenant_id[:8]}
                        )
                except (json.JSONDecodeError, KeyError):
                    pass
                
                # Record message receive time
                collector.record_histogram("websocket_receive_latency", duration)
                collector.increment_counter(
                    "websocket_messages_received",
                    labels={"tenant_id": self.tenant_id[:8]}
                )
                
                # Fire Locust event for tracking
                events.request.fire(
                    request_type="WSS",
                    name="ws_receive_message",
                    response_time=duration * 1000,  # Convert to ms
                    response_length=len(message),
                    exception=None,
                    context={}
                )
        
        except WebSocketTimeoutException:
            # Normal - no messages available
            pass
        
        except WebSocketException as e:
            # Connection lost
            print(f"WebSocket error: {e}")
            self.connected = False
            collector.increment_counter("websocket_disconnections")
            
            # Fire failure event
            events.request.fire(
                request_type="WSS",
                name="ws_receive_message",
                response_time=0,
                response_length=0,
                exception=e,
                context={}
            )
        
        except Exception as e:
            print(f"Unexpected error in listen_events: {e}")
            collector.increment_counter("websocket_errors")
    
    def on_stop(self):
        """Cleanup when user stops."""
        if self.ws and self.connected:
            try:
                self.ws.close()
                print(f"WebSocketUser disconnected. Received {self.messages_received} messages.")
            except Exception as e:
                print(f"Error closing WebSocket: {e}")


@events.test_stop.add_listener
def on_test_stop(environment, **kwargs):
    """
    Event handler called when test stops.
    
    Print custom metrics summary.
    """
    print("\n" + "=" * 80)
    print("LOAD TEST COMPLETED")
    print("=" * 80)
    
    # Print custom metrics
    collector.print_summary()
    
    # Export to JSON
    try:
        import os
        os.makedirs("reports", exist_ok=True)
        collector.export_json("reports/custom_metrics.json")
        print("[OK] Custom metrics exported to: reports/custom_metrics.json")
    except Exception as e:
        print(f"Failed to export metrics: {e}")


def _wait_for_pool_file(max_wait: int = 120) -> bool:
    """
    Poll the pool file until it contains at least one tenant or timeout.

    Used by worker processes in distributed mode: the master writes the pool
    file after creation, and workers wait for it to appear on the shared volume.

    Returns True when the pool is populated, False on timeout.
    """
    from utils.tenant_factory import load_tenant_pool

    interval = 2
    elapsed = 0
    while elapsed < max_wait:
        try:
            if load_tenant_pool():
                return True
        except Exception:
            pass
        time.sleep(interval)
        elapsed += interval
    return False


@events.init.add_listener
def on_locust_init(environment, **kwargs):
    """
    Ensure the tenant pool is fully populated before users start.

    Execution depends on the runner role:

    Master / standalone
        If pool file already exists → set _pool_ready immediately.
        If pool is empty → create all tenants via the application API,
        save the pool file, then set _pool_ready.

    Worker (distributed mode)
        Pool file is on a shared volume written by the master.
        Poll until the file appears, then set _pool_ready locally.
        Workers never call the API directly to avoid race conditions.
    """
    from locust.runners import WorkerRunner
    from utils.tenant_factory import TenantFactory, load_tenant_pool

    try:
        tenants = load_tenant_pool()
    except Exception:
        tenants = []

    is_worker = isinstance(environment.runner, WorkerRunner)

    # ── Pool already populated ──────────────────────────────────────────────
    if tenants:
        print(f"\n[INIT] {len(tenants)} tenants disponíveis no pool — pronto para iniciar.\n")
        _pool_ready.set()
        return

    # ── Worker: wait for master to write the pool file ──────────────────────
    if is_worker:
        print("\n[WORKER] Pool vazia — aguardando master criar tenants via API...")
        if _wait_for_pool_file(max_wait=_POOL_WAIT_TIMEOUT):
            count = len(load_tenant_pool())
            print(f"[WORKER] Pool recebida com {count} tenants — pronto para iniciar.\n")
            _pool_ready.set()
        else:
            print(
                f"\n[WORKER] TIMEOUT: pool ainda vazia após {_POOL_WAIT_TIMEOUT}s. "
                "Verifique o master e LOADTEST_ADMIN_TOKEN.\n"
            )
        return

    # ── Master / standalone: create tenants via the application API ─────────
    has_token = settings.ADMIN_TOKEN != "change-this-to-your-admin-seed-token"
    if not has_token:
        print("\n" + "!" * 80)
        print("ERRO: Pool vazia e LOADTEST_ADMIN_TOKEN não está configurado.")
        print("Configure a variável e reinicie, ou crie manualmente:")
        print("  python load_tests/utils/tenant_factory.py --create --count 50")
        print("!" * 80 + "\n")
        return

    tenant_count = settings.TENANT_COUNT
    print(f"\n[INIT] Pool vazia — criando {tenant_count} tenants via API da aplicação...")

    factory = TenantFactory(
        api_base_url=settings.API_BASE_URL,
        admin_token=settings.ADMIN_TOKEN,
        pool_file=settings.TENANT_POOL_FILE,
    )
    try:
        factory.create_tenants(count=tenant_count, prefix=settings.TENANT_PREFIX)
    except Exception as exc:
        print(f"\n[INIT] ERRO ao criar tenants: {exc}")
        print("Verifique LOADTEST_API_BASE_URL e LOADTEST_ADMIN_TOKEN.")
        _abort_locust(environment, f"Tenant creation raised an exception: {exc}")
        return

    if not factory.tenants:
        _abort_locust(
            environment,
            "0 tenants foram criados. Verifique LOADTEST_ADMIN_TOKEN e "
            "LOADTEST_API_BASE_URL antes de iniciar o teste.",
        )
        return

    factory.save_pool()
    print(
        f"[INIT] {len(factory.tenants)} tenants criados e salvos em "
        f"{settings.TENANT_POOL_FILE} — pronto para iniciar.\n"
    )
    _pool_ready.set()
