FROM golang:1.27.3-alpine AS build
WORKDIR /src
RUN apk add --no-cache git
COPY shared/ shared/
COPY services/application-svc/ services/application-svc/
WORKDIR /src/services/application-svc
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/application ./cmd/application

FROM alpine:3.21
WORKDIR /app
RUN apk add --no-cache ca-certificates font-dejavu && addgroup -S app && adduser -S app -G app
COPY --from=build /out/application /app/application
COPY --from=build /src/services/application-svc/migrations /app/migrations
USER app:app
EXPOSE 8083
ENTRYPOINT ["/app/application"]
