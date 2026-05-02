"""HTTP metrics middleware for Prometheus."""
import time
import re
from starlette.middleware.base import BaseHTTPMiddleware
from starlette.requests import Request
from starlette.responses import Response

from src.observability import metrics


class HTTPMetricsMiddleware(BaseHTTPMiddleware):
    """
    Middleware que registra métricas HTTP para todas as requisições.
    
    Métricas registradas:
    - http_requests_total: contador de requisições por método, endpoint e status
    - http_request_duration_seconds: histograma de duração das requisições
    """
    
    # Patterns para normalizar endpoints (remover IDs dinâmicos)
    NORMALIZATION_PATTERNS = [
        (re.compile(r'/admin/tenants/[0-9a-f-]{36}'), '/admin/tenants/{tenant_id}'),
        (re.compile(r'/v1/webhooks/[0-9a-f-]{36}'), '/v1/webhooks/{tenant_id}'),
        (re.compile(r'/v1/tokens/[0-9a-f-]{36}'), '/v1/tokens/{tenant_id}'),
        (re.compile(r'/ws/events/[0-9a-f-]{36}'), '/ws/events/{tenant_id}'),
    ]
    
    def _normalize_path(self, path: str) -> str:
        """Normaliza o path removendo IDs dinâmicos."""
        for pattern, replacement in self.NORMALIZATION_PATTERNS:
            path = pattern.sub(replacement, path)
        return path
    
    async def dispatch(self, request: Request, call_next) -> Response:
        """Intercepta requisição, mede duração e registra métricas."""
        # Ignora o próprio endpoint de métricas para evitar recursão
        if request.url.path == "/metrics":
            return await call_next(request)
        
        # Normaliza o endpoint
        normalized_path = self._normalize_path(request.url.path)
        
        # Mede tempo de execução
        start_time = time.time()
        
        # Processa a requisição
        response = await call_next(request)
        
        # Calcula duração
        duration = time.time() - start_time
        
        # Registra métricas
        metrics.http_requests_total.labels(
            method=request.method,
            endpoint=normalized_path,
            status_code=response.status_code
        ).inc()
        
        metrics.http_request_duration_seconds.labels(
            method=request.method,
            endpoint=normalized_path,
            status_code=response.status_code
        ).observe(duration)
        
        return response
