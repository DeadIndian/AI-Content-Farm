FROM python:3.12-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir flask piper-tts pathvalidate gunicorn

WORKDIR /app
COPY app.py /app/app.py

EXPOSE 5002

CMD ["gunicorn", "--bind", "0.0.0.0:5002", "--workers", "1", "--threads", "1", "--timeout", "180", "app:app"]
