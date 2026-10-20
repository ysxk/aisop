FROM golang:1.26-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/aisop ./cmd/aisop

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 aisop \
    && useradd --uid 10001 --gid 10001 --no-create-home --shell /usr/sbin/nologin aisop \
    && mkdir -p /data/media \
    && chown 10001:10001 /data/media
COPY --from=builder /out/aisop /usr/local/bin/aisop
ENV ADDR=:8080 MEDIA_DIR=/data/media
WORKDIR /data
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/aisop"]
CMD ["serve"]
