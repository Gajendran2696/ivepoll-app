# One image that serves the API and the built React app. The two codebases stay in
# /frontend and /backend; this file only assembles their build outputs.

# ---- 1. build the React app ----
FROM node:22-alpine AS web
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# ---- 2. build the Go service ----
FROM golang:1.24-alpine AS api
WORKDIR /src
ENV GOTOOLCHAIN=auto
COPY backend/ ./
# `go mod tidy` resolves dependencies and writes go.sum on first build.
RUN go mod tidy && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/livepoll .

# ---- 3. tiny runtime image ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
WORKDIR /app
COPY --from=api /out/livepoll ./livepoll
COPY --from=web /web/dist ./public
ENV STATIC_DIR=/app/public GIN_MODE=release PORT=8080
USER app
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://127.0.0.1:${PORT}/healthz || exit 1
CMD ["./livepoll"]
