"""
Main Locust load testing file for EasyHooks.

This file contains the primary user classes for load testing:
- WebhookUser: Simulates clients sending webhooks via HTTP
- WebSocketUser: Simulates clients listening for events via WebSocket
"""
import json
import uuid
import time
from locust import HttpUser, task, between, events
from websocket import create_connection, WebSocketTimeoutException, WebSocketException

from utils.tenant_factory import get_tenant_from_pool
from utils.hmac_helpers import sign_webhook
from utils.metrics_collector import collector
from config import settings


class WebhookUser(HttpUser):
    """
    User that sends webhook events via HTTP POST.
    
    Simulates external systems posting webhook events to the platform.
    Each user is assigned a tenant from the pool.
    """
    
    wait_time = between(0.1, 0.5)
    
    def on_start(self):
        """Initialize user with tenant credentials."""
        try:
            self.tenant_id, self.secret = get_tenant_from_pool()
            self.event_counter = 0
            print(f"WebhookUser started with tenant: {self.tenant_id}")
        except Exception as e:
            print(f"Failed to get tenant from pool: {e}")
            print("Make sure to run: python load_tests/utils/tenant_factory.py --create --count 50")
            raise
    
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
        """Initialize user with tenant credentials and connect WebSocket."""
        try:
            self.tenant_id, self.secret = get_tenant_from_pool()
            self.ws = None
            self.connected = False
            self.messages_received = 0
            self.connect_websocket()
            print(f"WebSocketUser connected for tenant: {self.tenant_id}")
        except Exception as e:
            print(f"Failed to start WebSocketUser: {e}")
            raise
    
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


@events.init.add_listener
def on_locust_init(environment, **kwargs):
    """
    Event handler called when Locust initializes.
    
    Validate tenant pool exists.
    """
    try:
        from utils.tenant_factory import load_tenant_pool
        tenants = load_tenant_pool()
    except UnicodeEncodeError:
        # Windows console encoding issue
        from utils.tenant_factory import load_tenant_pool
        tenants = load_tenant_pool()
    except Exception:
        tenants = []
    
    if not tenants:
        print("\n" + "!" * 80)
        print("WARNING: Tenant pool is empty!")
        print("Run: python load_tests/utils/tenant_factory.py --create --count 50")
        print("!" * 80 + "\n")
    else:
        print(f"\n[OK] Loaded {len(tenants)} tenants from pool\n")
