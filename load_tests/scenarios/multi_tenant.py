"""
Multi-tenant isolation test scenario.

Validates that tenants are isolated under load.

Configuration:
- 3 tenant groups with different load profiles:
  * VIP tenant: 10 req/s (normal)
  * Normal tenants (10): 50 req/s each (normal)
  * Abusive tenant: 5000 req/s (flood)
- 30 minutes duration
- Measure latency degradation per tenant group
"""
from locust import HttpUser, task, between, LoadTestShape
import sys
from pathlib import Path

sys.path.append(str(Path(__file__).parent.parent))
from locustfile import WebhookUser


class VIPTenantUser(WebhookUser):
    """VIP tenant with normal load."""
    wait_time = between(0.08, 0.12)  # ~10 req/s
    weight = 1
    
    def on_start(self):
        super().on_start()
        # Use first tenant as VIP
        from utils.tenant_factory import get_tenant_from_pool
        self.tenant_id, self.secret = get_tenant_from_pool(0)
        print(f"VIP Tenant: {self.tenant_id}")


class NormalTenantUser(WebhookUser):
    """Normal tenant with moderate load."""
    wait_time = between(0.015, 0.025)  # ~50 req/s per user
    weight = 10
    
    def on_start(self):
        super().on_start()
        # Use tenants 1-10 as normal
        from utils.tenant_factory import get_tenant_from_pool
        import random
        tenant_index = random.randint(1, 10)
        self.tenant_id, self.secret = get_tenant_from_pool(tenant_index)


class AbusiveTenantUser(WebhookUser):
    """Abusive tenant with flood load."""
    wait_time = between(0.0001, 0.0002)  # ~5000 req/s
    weight = 100  # Many users to generate high load
    
    def on_start(self):
        super().on_start()
        # Use tenant 11 as abusive
        from utils.tenant_factory import get_tenant_from_pool
        self.tenant_id, self.secret = get_tenant_from_pool(11)
        print(f"Abusive Tenant: {self.tenant_id}")


class MultiTenantLoadShape(LoadTestShape):
    """
    Custom load shape for multi-tenant test.
    
    Timeline:
    - 0-5min: Ramp up normal tenants
    - 5-10min: Add VIP tenant
    - 10-25min: Add abusive tenant (stress test)
    - 25-30min: Remove abusive, observe recovery
    """
    
    def tick(self):
        run_time = self.get_run_time()
        
        if run_time < 300:  # 0-5min: normal tenants only
            user_count = int(run_time / 300 * 50)  # Ramp to 50 normal users
            return (user_count, 2)
        
        elif run_time < 600:  # 5-10min: add VIP
            return (55, 1)  # 50 normal + 5 VIP users
        
        elif run_time < 1500:  # 10-25min: add abusive
            return (255, 5)  # 50 normal + 5 VIP + 200 abusive
        
        elif run_time < 1800:  # 25-30min: remove abusive, recovery
            return (55, 1)  # Back to normal + VIP
        
        else:
            return None  # Test complete
