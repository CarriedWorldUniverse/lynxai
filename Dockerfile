# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/lynxai ./cmd/lynxai

FROM chromedp/headless-shell:latest
# headless-shell ships a Chromium-equivalent binary; chromedp finds it on PATH.
# It has no system CA bundle though — Chromium carries its own, but the Go
# binary (bridle → LLM provider over TLS) needs one. Take the build stage's.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/lynxai /usr/local/bin/lynxai
ENV LYNXAI_DATA_DIR=/data
VOLUME /data
EXPOSE 7878
ENTRYPOINT ["/usr/local/bin/lynxai"]
CMD ["serve", "--addr", "0.0.0.0:7878", "--data-dir", "/data"]
