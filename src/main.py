from contextlib import asynccontextmanager
from fastapi import FastAPI, Response
from fastapi.middleware.cors import CORSMiddleware
from src.config import settings
from src.middleware import HTTPMetricsMiddleware
from src.observability import metrics, tracing
from src.redis_client import redis_pool
from src.routers.admin import router as admin_router
from src.routers.tokens import router as tokens_router
from src.routers.webhooks import router as webhooks_router
from src.routers.ws import router as ws_router
from src.services.kafka_producer import kafka_manager


@asynccontextmanager
async def lifespan(app: FastAPI):
    tracing.setup_tracing(
        service_name=settings.OTEL_SERVICE_NAME,
        otlp_endpoint=settings.OTEL_EXPORTER_OTLP_ENDPOINT,
        enabled=settings.TRACING_ENABLED,
        sample_rate=settings.TRACING_SAMPLE_RATE,
    )
    
    if settings.TRACING_ENABLED:
        tracing.instrument_fastapi(app)
        tracing.instrument_redis()
    
    await kafka_manager.start()
    try:
        yield
    finally:
        await kafka_manager.stop()
        await redis_pool.aclose()


app = FastAPI(title="Easyhooks", version="0.1.0", lifespan=lifespan)

# CORS para permitir o Playground (Docusaurus) em localhost:3001
origins = [origin.strip() for origin in settings.CORS_ORIGINS.split(",") if origin.strip()]
app.add_middleware(
    CORSMiddleware,
    allow_origins=origins,
    allow_credentials=True,
    allow_methods=["GET", "POST", "OPTIONS"],
    allow_headers=[
        "Authorization",
        "Content-Type",
        "X-Webhook-Signature",
        "X-Event-Id",
    ],
)

# HTTP Metrics Middleware para registrar métricas no Prometheus
app.add_middleware(HTTPMetricsMiddleware)

app.include_router(admin_router)
app.include_router(webhooks_router)
app.include_router(tokens_router)
app.include_router(ws_router)


@app.get("/health", include_in_schema=False)
async def health_check():
    """Health check endpoint for load balancers and monitoring."""
    return {"status": "healthy", "service": "easyhooks"}


@app.get("/metrics", include_in_schema=False)
async def prometheus_metrics():
    """Prometheus metrics endpoint."""
    metrics_data, content_type = metrics.get_metrics()
    return Response(content=metrics_data, media_type=content_type)
