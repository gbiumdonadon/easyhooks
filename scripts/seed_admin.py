"""Idempotent seed script: creates the initial superadmin user."""
import asyncio
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from sqlalchemy import select
from src.database import async_session_factory
from src.models.admin_user import AdminUser
from src.security import hash_secret


async def seed():
    token = os.environ.get("ADMIN_SEED_TOKEN")
    if not token:
        print("ADMIN_SEED_TOKEN not set, skipping seed.")
        return

    async with async_session_factory() as session:
        result = await session.execute(
            select(AdminUser).where(AdminUser.username == "superadmin")
        )
        if result.scalar_one_or_none():
            print("Admin already seeded, skipping.")
            return

        admin = AdminUser(
            username="superadmin",
            token_hash=hash_secret(token),
            role="admin",
        )
        session.add(admin)
        await session.commit()
        print("Admin 'superadmin' seeded successfully.")


if __name__ == "__main__":
    asyncio.run(seed())
