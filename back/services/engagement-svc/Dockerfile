FROM golang:1.27.3-alpine AS build
WORKDIR /src
RUN apk add --no-cache git
COPY shared/ shared/
COPY services/engagement-svc/ services/engagement-svc/
WORKDIR /src/services/engagement-svc
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/engagement ./cmd/engagement

FROM alpine:3.21
WORKDIR /app
RUN apk add --no-cache ca-certificates && addgroup -S app && adduser -S app -G app
COPY --from=build /out/engagement /app/engagement
COPY --from=build /src/services/engagement-svc/migrations /app/migrations
COPY --from=build /src/services/engagement-svc/templates /app/templates
USER app:app
EXPOSE 8084
ENTRYPOINT ["/app/engagement"]
