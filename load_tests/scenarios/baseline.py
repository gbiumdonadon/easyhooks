"""
Baseline load test scenario.

Establishes baseline performance with moderate load.

Configuration:
- 100 virtual users
- ~100 requests/second
- 10 minutes duration
- Mix of webhook senders and WebSocket listeners
"""
from locust import HttpUser, task, between, LoadTestShape
import sys
from pathlib import Path

sys.path.append(str(Path(__file__).parent.parent))
from locustfile import WebhookUser, WebSocketUser


class BaselineWebhookUser(WebhookUser):
    """Webhook sender for baseline test."""
    wait_time = between(0.5, 1.5)
    weight = 8  # 80% webhook senders


class BaselineWebSocketUser(WebSocketUser):
    """WebSocket listener for baseline test."""
    wait_time = between(2, 5)
    weight = 2  # 20% WebSocket listeners


class BaselineLoadShape(LoadTestShape):
    """
    Custom load shape for baseline test.
    
    Timeline:
    - 0-2min: Ramp up to 100 users
    - 2-10min: Maintain 100 users
    """
    
    def tick(self):
        run_time = self.get_run_time()
        
        if run_time < 120:  # 0-2 minutes: ramp up
            user_count = int(run_time / 120 * 100)
            spawn_rate = 1
            return (user_count, spawn_rate)
        
        elif run_time < 600:  # 2-10 minutes: steady state
            return (100, 1)
        
        else:  # After 10 minutes: stop
            return None
