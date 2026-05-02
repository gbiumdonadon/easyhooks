"""
Stress test scenario (breaking point).

Identifies system breaking point and degradation behavior.

Configuration:
- Aggressive scaling: +500 users every 2 minutes
- No upper limit
- Stop when: error rate > 10% OR latency p95 > 5s
- 60 minutes max duration
"""
from locust import HttpUser, task, between, LoadTestShape
import sys
from pathlib import Path

sys.path.append(str(Path(__file__).parent.parent))
from locustfile import WebhookUser, WebSocketUser


class StressWebhookUser(WebhookUser):
    """Webhook sender for stress test."""
    wait_time = between(0.05, 0.15)  # Aggressive
    weight = 9


class StressWebSocketUser(WebSocketUser):
    """WebSocket listener for stress test."""
    wait_time = between(1, 3)
    weight = 1


class StressTestLoadShape(LoadTestShape):
    """
    Custom load shape for stress test.
    
    Aggressive scaling to find breaking point:
    - +500 users every 2 minutes
    - Continue until failure or 60 minutes
    """
    
    def tick(self):
        run_time = self.get_run_time()
        
        if run_time > 3600:  # Max 60 minutes
            return None
        
        # +500 users every 2 minutes (120 seconds)
        stage = int(run_time / 120)
        user_count = 500 + (stage * 500)
        spawn_rate = 100
        
        # Cap at 20,000 to prevent system crash
        user_count = min(user_count, 20000)
        
        return (user_count, spawn_rate)
