FROM python:3.12-slim

WORKDIR /app

# Install system dependencies for healthchecks
RUN apt-get update && \
    apt-get install -y --no-install-recommends curl && \
    rm -rf /var/lib/apt/lists/*

# Install Python dependencies
COPY load_tests/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy load test files
COPY load_tests/ /load_tests/

# Make scripts executable
RUN chmod +x /load_tests/wait-for-api.sh /load_tests/docker-entrypoint.sh

# Set Python path
ENV PYTHONPATH=/app:/load_tests

# Entrypoint resolves LOADTEST_ADMIN_TOKEN fallback (see docker-entrypoint.sh)
# before handing off to the container's command.
ENTRYPOINT ["/load_tests/docker-entrypoint.sh"]
CMD ["locust", "-f", "/load_tests/locustfile.py"]
