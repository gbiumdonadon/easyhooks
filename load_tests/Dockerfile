FROM python:3.12-slim

WORKDIR /app

# Install dependencies
COPY load_tests/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy load test files
COPY load_tests/ /load_tests/

# Set Python path
ENV PYTHONPATH=/app:/load_tests

CMD ["locust", "-f", "/load_tests/locustfile.py"]
