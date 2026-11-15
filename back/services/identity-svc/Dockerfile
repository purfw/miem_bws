FROM golang:1.27.3-alpine AS build
WORKDIR /src
RUN apk add --no-cache git
COPY shared/ shared/
COPY services/identity-svc/ services/identity-svc/
WORKDIR /src/services/identity-svc
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/identity ./cmd/identity

FROM alpine:3.21
WORKDIR /app
RUN addgroup -S app && adduser -S app -G app
COPY --from=build /out/identity /app/identity
COPY --from=build /src/services/identity-svc/migrations /app/migrations
USER app:app
EXPOSE 8081
ENTRYPOINT ["/app/identity"]
