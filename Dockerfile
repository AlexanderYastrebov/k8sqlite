FROM golang:latest AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build -v -trimpath -ldflags '-d -w -s'

FROM scratch
ARG REVISION
LABEL org.opencontainers.image.title="Query Kubernetes objects in SQLite"
LABEL org.opencontainers.image.description="This program connects to Kubernetes API server and fetches configured API resources into in-memory sqlite3 database."
LABEL org.opencontainers.image.authors="Alexander Yastrebov <yastrebov.alex@gmail.com>"
LABEL org.opencontainers.image.url="https://github.com/AlexanderYastrebov/k8sqlite"
LABEL org.opencontainers.image.licenses="BSD-3-Clause"
LABEL org.opencontainers.image.revision="${REVISION}"

COPY --from=builder /app/k8sqlite /k8sqlite

ENTRYPOINT ["/k8sqlite"]
