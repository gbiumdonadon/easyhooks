"""
WebSocket scale test scenario.

Tests massive WebSocket connection scaling.

Configuration:
- Target: 10,000 concurrent WebSocket connections
- Moderate webhook traffic (100 req/s)
- 60 minutes duration
- Focus on connection stability and message delivery
"""
from locust import HttpUser, task, between, LoadTestShape
import sys
from pathlib import Path

sys.path.append(str(Path(__file__).parent.parent))
from locustfile import WebhookUser, WebSocketUser


class WebSocketScaleWebhookUser(WebhookUser):
    """Webhook sender for WebSocket scale test (light load)."""
    wait_time = between(5, 10)  # Low frequency
    weight = 1  # 1% webhook senders (just enough to generate events)


class WebSocketScaleWSUser(WebSocketUser):
    """WebSocket listener for scale test."""
    wait_time = between(1, 3)
    weight = 99  # 99% WebSocket connections


class WebSocketScaleLoadShape(LoadTestShape):
    """
    Custom load shape for WebSocket scale test.
    
    Timeline:
    - 0-1min: Ramp to 1,000 connections (warmup)
    - 1-6min: Ramp to 5,000 connections
    - 6-16min: Ramp to 10,000 connections
    - 16-46min: Maintain 10,000 connections (steady state)
    - 46-60min: Gradual ramp down
    """
    
    def tick(self):
        run_time = self.get_run_time()
        
        if run_time < 60:  # 0-1min: warmup
            user_count = int(run_time / 60 * 1000)
            spawn_rate = 50
            return (user_count, spawn_rate)
        
        elif run_time < 360:  # 1-6min: ramp to 5k
            elapsed = run_time - 60
            user_count = 1000 + int(elapsed / 300 * 4000)
            spawn_rate = 50
            return (user_count, spawn_rate)
        
        elif run_time < 960:  # 6-16min: ramp to 10k
            elapsed = run_time - 360
            user_count = 5000 + int(elapsed / 600 * 5000)
            spawn_rate = 50
            return (user_count, spawn_rate)
        
        elif run_time < 2760:  # 16-46min: steady at 10k
            return (10000, 10)
        
        elif run_time < 3600:  # 46-60min: ramp down
            elapsed = run_time - 2760
            user_count = 10000 - int(elapsed / 840 * 9000)
            spawn_rate = 50
            return (user_count, spawn_rate)
        
        else:
            return None  # Test complete
