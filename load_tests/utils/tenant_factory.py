"""Tenant factory for creating test tenants."""
import json
import sys
import argparse
import threading
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
import httpx

# Ensure the load_tests directory is on sys.path so `config` can be imported
# regardless of the working directory (local dev, Docker container, CI/CD).
_load_tests_dir = Path(__file__).parent.parent
if str(_load_tests_dir) not in sys.path:
    sys.path.insert(0, str(_load_tests_dir))

from config import settings  # noqa: E402 — path manipulation required above


class TenantFactory:
    """Factory for creating and managing test tenants."""

    def __init__(self, api_base_url: str, admin_token: str, pool_file: str):
        self.api_base_url = api_base_url
        self.admin_token = admin_token
        self.pool_file = Path(pool_file)
        self.tenants: list[dict] = []
        self._lock = threading.Lock()

    def create_tenant(self, name: str) -> dict:
        """
        Create a single tenant via the application API.

        Args:
            name: Tenant name

        Returns:
            Dict with tenant_id and secret_key
        """
        response = httpx.post(
            f"{self.api_base_url}/admin/tenants",
            headers={
                "Authorization": f"Bearer {self.admin_token}",
                "Content-Type": "application/json",
            },
            json={"name": name},
            timeout=10.0,
        )
        response.raise_for_status()
        return response.json()

    def create_tenants(self, count: int, prefix: str = "loadtest", max_workers: int = 10) -> list[dict]:
        """
        Create multiple tenants in parallel via the application API.

        Uses a thread pool to run concurrent POST /admin/tenants requests,
        reducing setup time from O(count * latency) to O(count / workers * latency).

        Args:
            count:       Number of tenants to create.
            prefix:      Name prefix for tenants (e.g. "loadtest-tenant-0001").
            max_workers: Maximum concurrent API requests (default: 10).

        Returns:
            List of tenant dicts with tenant_id and secret_key.
        """
        print(f"[TenantFactory] Creating {count} test tenants via API (concurrency={max_workers})...")

        names = [f"{prefix}-tenant-{i:04d}" for i in range(count)]
        results: list[dict] = []
        failures = 0

        with ThreadPoolExecutor(max_workers=max_workers) as pool:
            future_to_name = {pool.submit(self.create_tenant, name): name for name in names}

            for future in as_completed(future_to_name):
                name = future_to_name[future]
                try:
                    tenant = future.result()
                    with self._lock:
                        results.append(tenant)
                    print(f"  [OK] {name}: {tenant['tenant_id']}")
                except Exception as exc:
                    failures += 1
                    print(f"  [FAIL] {name}: {exc}")

        if failures:
            print(f"\n[TenantFactory] Warning: {failures}/{count} tenants failed to create.")

        # Sort by name so the pool order is deterministic
        results.sort(key=lambda t: t.get("name", ""))
        self.tenants = results
        return results
    
    def save_pool(self):
        """Save tenant pool to JSON file."""
        self.pool_file.parent.mkdir(parents=True, exist_ok=True)
        with open(self.pool_file, 'w') as f:
            json.dump(self.tenants, f, indent=2)
        print(f"\n[OK] Saved {len(self.tenants)} tenants to {self.pool_file}")
    
    def load_pool(self) -> list[dict]:
        """Load tenant pool from JSON file."""
        if not self.pool_file.exists():
            return []
        
        with open(self.pool_file, 'r') as f:
            self.tenants = json.load(f)
        return self.tenants
    
    def get_tenant(self, index: int = 0) -> dict:
        """
        Get tenant from pool by index.
        
        Args:
            index: Index in tenant pool
            
        Returns:
            Tenant dict with tenant_id and secret_key
        """
        if not self.tenants:
            self.load_pool()
        
        if not self.tenants:
            raise RuntimeError("Tenant pool is empty. Run with --create first.")
        
        return self.tenants[index % len(self.tenants)]


def load_tenant_pool():
    """
    Load tenant pool from file (convenience function for locustfile).
    
    Returns:
        List of tenant dicts
    """
    factory = TenantFactory(
        settings.API_BASE_URL,
        settings.ADMIN_TOKEN,
        settings.TENANT_POOL_FILE
    )
    return factory.load_pool()


def get_tenant_from_pool(index: int = None):
    """
    Get a tenant from the pool (convenience function for locustfile).
    
    Args:
        index: Optional specific index, otherwise use random
        
    Returns:
        Tuple of (tenant_id, secret_key)
    """
    import random
    
    tenants = load_tenant_pool()
    if not tenants:
        raise RuntimeError("Tenant pool is empty. Run tenant_factory.py --create first.")
    
    if index is None:
        tenant = random.choice(tenants)
    else:
        tenant = tenants[index % len(tenants)]
    
    return tenant['tenant_id'], tenant['secret_key']


def main():
    parser = argparse.ArgumentParser(description="Tenant factory for load testing")
    parser.add_argument(
        "--create",
        action="store_true",
        help="Create new tenants"
    )
    parser.add_argument(
        "--count",
        type=int,
        default=50,
        help="Number of tenants to create (default: 50)"
    )
    parser.add_argument(
        "--prefix",
        type=str,
        default="loadtest",
        help="Tenant name prefix (default: loadtest)"
    )
    parser.add_argument(
        "--list",
        action="store_true",
        help="List existing tenants from pool"
    )
    
    args = parser.parse_args()
    
    factory = TenantFactory(
        settings.API_BASE_URL,
        settings.ADMIN_TOKEN,
        settings.TENANT_POOL_FILE
    )
    
    if args.create:
        factory.create_tenants(args.count, args.prefix)
        factory.save_pool()

        if not factory.tenants:
            print("\n[FATAL] Nenhum tenant foi criado — abortando.")
            print("Verifique LOADTEST_ADMIN_TOKEN e LOADTEST_API_BASE_URL.")
            sys.exit(1)

        print(f"\n[OK] Pool pronta com {len(factory.tenants)} tenants.")
    elif args.list:
        tenants = factory.load_pool()
        print(f"\nTenant pool ({len(tenants)} tenants):")
        for i, tenant in enumerate(tenants):
            print(f"  [{i:3d}] {tenant['tenant_id']}")
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
