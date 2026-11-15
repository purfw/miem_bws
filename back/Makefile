.PHONY: schema secrets up down logs build test tidy verify clean

secrets:
	./scripts/gen-keys.sh
	./scripts/gen-secrets.sh

up:
	docker compose up -d --build

down:
	docker compose down

logs:
	docker compose logs -f --tail=100

MODULES := shared \
	services/identity-svc \
	services/conference-svc \
	services/application-svc \
	services/engagement-svc

build:
	@for m in $(MODULES); do echo "==> $$m"; (cd $$m && go build ./...) || exit 1; done

# Resolve each module independently of go.work.
tidy:
	@for m in $(MODULES); do echo "==> $$m"; (cd $$m && GOWORK=off go mod tidy) || exit 1; done

verify:
	@for m in $(MODULES); do echo "==> $$m"; (cd $$m && GOWORK=off go vet ./... && GOWORK=off go build ./...) || exit 1; done

test:
	@for m in $(MODULES); do echo "==> $$m"; (cd $$m && GOWORK=off go test -race ./...) || exit 1; done

clean:
	docker compose down -v
	rm -rf secrets/jwt_ed25519.* .env

schema:
	./scripts/export-schema.sh
