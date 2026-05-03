"""Configuration for load tests."""
from pathlib import Path
from pydantic_settings import BaseSettings

# Resolve the load_tests directory from this file's location.
# This ensures paths are correct regardless of the working directory
# (local dev, Docker container, CI/CD, etc).
LOAD_TESTS_DIR = Path(__file__).parent


class LoadTestSettings(BaseSettings):
    """Load test configuration."""

    API_BASE_URL: str = "http://localhost:8000"
    # Set via LOADTEST_ADMIN_TOKEN env var or in .env — never hardcode a real token here
    ADMIN_TOKEN: str = "change-this-to-your-admin-seed-token"

    # Test configuration
    DEFAULT_SPAWN_RATE: int = 10
    DEFAULT_RUN_TIME: str = "10m"

    # Tenant pool configuration
    TENANT_POOL_SIZE: int = 50
    # Path anchored to the load_tests/ directory; overridable via env var
    TENANT_POOL_FILE: str = str(LOAD_TESTS_DIR / ".tenant_pool.json")

    # Number of tenants to auto-create when pool is empty
    TENANT_COUNT: int = 50
    TENANT_PREFIX: str = "loadtest"

    # WebSocket configuration
    WS_TIMEOUT: int = 10
    WS_RECEIVE_TIMEOUT: int = 5

    # Metrics
    ENABLE_CUSTOM_METRICS: bool = True
    METRICS_EXPORT_INTERVAL: int = 10

    model_config = {"env_file": ".env", "env_prefix": "LOADTEST_", "extra": "ignore"}


settings = LoadTestSettings()
