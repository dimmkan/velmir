FROM node:24-bookworm-slim AS frontend-build
WORKDIR /build/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run type-check && npm run build

FROM golang:1.26.0-alpine AS backend-build
WORKDIR /build/backend
ENV CGO_ENABLED=0
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN go test ./... -timeout 60s && \
    go build -trimpath -ldflags="-s -w" -o /out/velmir-server ./cmd/server && \
    go build -trimpath -ldflags="-s -w" -o /out/velmir-migrate ./cmd/migrate && \
    go build -trimpath -ldflags="-s -w" -o /out/velmir-bootstrap ./cmd/bootstrap && \
    go build -trimpath -ldflags="-s -w" -o /out/velmir-users ./cmd/users

FROM alpine:3.22 AS runtime
RUN apk add --no-cache ca-certificates && \
    addgroup -S velmir && adduser -S -G velmir velmir
WORKDIR /app
COPY --from=backend-build /out/ /usr/local/bin/
COPY backend/migrations/ ./migrations/
COPY --from=frontend-build /build/frontend/dist/ ./frontend/dist/
ENV FRONTEND_DIST_PATH=/app/frontend/dist
USER velmir
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/velmir-server"]
