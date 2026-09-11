# Builds all three Go binaries (receiver, worker, api) into one small image.
# docker-compose picks which one to run per service via `command:`.

# ---- Build stage ----
FROM golang:1.24-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/receiver ./cmd/receiver \
 && CGO_ENABLED=0 go build -o /out/worker   ./cmd/worker \
 && CGO_ENABLED=0 go build -o /out/api      ./cmd/api

# ---- Run stage ----
# Distroless: tiny, no shell, smaller attack surface.
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/receiver /receiver
COPY --from=build /out/worker /worker
COPY --from=build /out/api /api
EXPOSE 8080 8081
USER nonroot:nonroot
# Default to the receiver; worker/api override this in compose/k8s.
ENTRYPOINT ["/receiver"]
