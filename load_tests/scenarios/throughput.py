"""
Throughput load test scenario.

Measures maximum sustainable throughput.

Configuration:
- Gradual scaling: 100 → 500 → 1000 → 2000 → 5000 users
- 30 minutes duration
- Primarily webhook senders with some WebSocket listeners
"""
from locust import HttpUser, task, between, LoadTestShape
import sys
from pathlib import Path

sys.path.append(str(Path(__file__).parent.parent))
from locustfile import WebhookUser, WebSocketUser


class ThroughputWebhookUser(WebhookUser):
    """Webhook sender optimized for throughput."""
    wait_time = between(0.1, 0.3)  # More aggressive
    weight = 9  # 90% webhook senders


class ThroughputWebSocketUser(WebSocketUser):
    """WebSocket listener for throughput test."""
    wait_time = between(3, 7)
    weight = 1  # 10% WebSocket listeners


class ThroughputLoadShape(LoadTestShape):
    """
    Custom load shape for throughput test.
    
    Timeline:
    - 0-3min: Ramp to 100 users
    - 3-6min: Ramp to 500 users
    - 6-9min: Ramp to 1000 users
    - 9-15min: Ramp to 2000 users
    - 15-21min: Ramp to 5000 users
    - 21-30min: Maintain 5000 users
    """
    
    stages = [
        {"duration": 180, "users": 100},    # 0-3min
        {"duration": 180, "users": 500},    # 3-6min
        {"duration": 180, "users": 1000},   # 6-9min
        {"duration": 360, "users": 2000},   # 9-15min
        {"duration": 360, "users": 5000},   # 15-21min
        {"duration": 540, "users": 5000},   # 21-30min (steady)
    ]
    
    def tick(self):
        run_time = self.get_run_time()
        
        cumulative_time = 0
        for stage in self.stages:
            cumulative_time += stage["duration"]
            
            if run_time < cumulative_time:
                # Calculate position within current stage
                stage_start = cumulative_time - stage["duration"]
                stage_progress = run_time - stage_start
                
                # Linear interpolation to target users
                prev_users = self.stages[self.stages.index(stage) - 1]["users"] if stage != self.stages[0] else 0
                target_users = stage["users"]
                
                if stage_progress < stage["duration"]:
                    user_count = int(prev_users + (target_users - prev_users) * stage_progress / stage["duration"])
                else:
                    user_count = target_users
                
                spawn_rate = max(10, user_count // 10)
                return (user_count, spawn_rate)
        
        return None  # Test complete
