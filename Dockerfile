FROM golang:1.26-alpine AS build

WORKDIR /app
COPY go.mod go.sum ./
ENV GOMAXPROCS=2 GOFLAGS=-p=2
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/api ./cmd/api

FROM python:3.13-slim-trixie AS runtime
COPY --from=denoland/deno:bin-2.9.6 /deno /usr/local/bin/deno
RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg ca-certificates curl fonts-dejavu-core fonts-noto-core intel-media-va-driver libgomp1 \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /srv
COPY requirements-shorts.txt ./
RUN pip install --no-cache-dir -r requirements-shorts.txt
COPY scripts/shorts.py /srv/scripts/shorts.py
COPY --from=build /bin/api /usr/local/bin/api
RUN mkdir -p /srv/data
ENV TTS_DOCKER_AUTO_MANAGE=false HF_HOME=/srv/data/models \
    AICF_CONTAINER=true \
    FFMPEG_BIN=/usr/bin/ffmpeg FFPROBE_BIN=/usr/bin/ffprobe \
    OMP_NUM_THREADS=2 OPENBLAS_NUM_THREADS=1 MKL_NUM_THREADS=1 \
    SHORTS_THREADS=2 GOMAXPROCS=2 GOMEMLIMIT=256MiB \
    PYTHONDONTWRITEBYTECODE=1 TOKENIZERS_PARALLELISM=false
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]

FROM runtime AS npu
RUN apt-get update && apt-get install -y --no-install-recommends libtbb12 libze1 \
    && rm -rf /var/lib/apt/lists/*
COPY scripts/install-npu.py /tmp/install-npu.py
RUN python /tmp/install-npu.py && rm /tmp/install-npu.py
RUN pip install --no-cache-dir openvino-genai==2026.3.1.0
COPY scripts/npu_transcribe.py /srv/scripts/npu_transcribe.py
