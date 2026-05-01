from fastapi import APIRouter, Body, Depends, Header, HTTPException

from src.dependencies import AuthenticatedTenant, get_authenticated_tenant
from src.services.kafka_producer import get_kafka_producer
from src.services.webhook_service import produce_webhook_message

router = APIRouter(prefix="/v1/webhooks", tags=["webhooks"])


@router.post("/{tenant_id}", status_code=202)
async def ingest_webhook(
    tenant: AuthenticatedTenant = Depends(get_authenticated_tenant),
    payload: dict = Body(...),
    x_event_id: str | None = Header(None, alias="X-Event-Id"),
    producer=Depends(get_kafka_producer),
):
    if not x_event_id or not x_event_id.strip():
        raise HTTPException(
            status_code=400,
            detail="Missing required header X-Event-Id",
        )

    await produce_webhook_message(
        producer=producer,
        tenant_id=tenant.tenant_id,
        event_id=x_event_id.strip(),
        payload=payload,
    )
    return {"status": "accepted", "tenant_id": str(tenant.tenant_id)}
