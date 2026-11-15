FROM golang:1.27.3-alpine AS build
WORKDIR /src
RUN apk add --no-cache git
COPY shared/ shared/
COPY services/conference-svc/ services/conference-svc/
WORKDIR /src/services/conference-svc
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/conference ./cmd/conference

FROM alpine:3.21
WORKDIR /app
RUN addgroup -S app && adduser -S app -G app
COPY --from=build /out/conference /app/conference
COPY --from=build /src/services/conference-svc/migrations /app/migrations
USER app:app
EXPOSE 8082
ENTRYPOINT ["/app/conference"]
