FROM golang:1.26-alpine AS build

WORKDIR /app
COPY go.mod go.sum ./
ENV GOMAXPROCS=1 GOFLAGS=-p=1
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/api ./cmd/api

FROM python:3.13-slim-trixie AS runtime
COPY --from=denoland/deno:bin-2.9.6 /deno /usr/local/bin/deno
RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg espeak-ng ca-certificates curl fonts-dejavu-core fonts-noto-core intel-media-va-driver libgomp1 \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /srv
COPY requirements-shorts.txt ./
COPY requirements-studio.lock ./
RUN pip install --no-cache-dir -r requirements-shorts.txt -r requirements-studio.lock
COPY scripts/shorts.py scripts/cloud_transcribe.py scripts/studio_plan.py scripts/studio_render.py scripts/studio_cast.py /srv/scripts/
COPY assets/presenters /srv/assets/presenters
COPY --from=build /bin/api /usr/local/bin/api
RUN mkdir -p /srv/data
ENV TTS_DOCKER_AUTO_MANAGE=false HF_HOME=/srv/data/models \
    AICF_CONTAINER=true \
    FFMPEG_BIN=/usr/bin/ffmpeg FFPROBE_BIN=/usr/bin/ffprobe \
    OMP_NUM_THREADS=1 OPENBLAS_NUM_THREADS=1 MKL_NUM_THREADS=1 \
    SHORTS_THREADS=1 GOMAXPROCS=1 GOMEMLIMIT=256MiB \
    ALLOW_LOCAL_MODELS=false SHORTS_TRANSCRIBER=gemini STUDIO_RESOURCE_MODE=gentle \
    PYTHONDONTWRITEBYTECODE=1 TOKENIZERS_PARALLELISM=false
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]

FROM runtime AS npu
COPY requirements-local-models.txt ./
RUN pip install --no-cache-dir -r requirements-local-models.txt
RUN apt-get update && apt-get install -y --no-install-recommends libtbb12 libze1 \
    && rm -rf /var/lib/apt/lists/*
COPY scripts/install-npu.py /tmp/install-npu.py
RUN python /tmp/install-npu.py && rm /tmp/install-npu.py
RUN pip install --no-cache-dir openvino-genai==2026.3.1.0
COPY scripts/npu_transcribe.py /srv/scripts/npu_transcribe.py
