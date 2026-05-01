from contextlib import asynccontextmanager
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from src.config import settings
from src.redis_client import redis_pool
from src.routers.admin import router as admin_router
from src.routers.tokens import router as tokens_router
from src.routers.webhooks import router as webhooks_router
from src.routers.ws import router as ws_router
from src.services.kafka_producer import kafka_manager


@asynccontextmanager
async def lifespan(app: FastAPI):
    await kafka_manager.start()
    try:
        yield
    finally:
        await kafka_manager.stop()
        await redis_pool.aclose()


app = FastAPI(title="Webhooks Platform", version="0.1.0", lifespan=lifespan)

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

app.include_router(admin_router)
app.include_router(webhooks_router)
app.include_router(tokens_router)
app.include_router(ws_router)
