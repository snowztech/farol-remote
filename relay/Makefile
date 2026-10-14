.PHONY: test deploy logs

test:
	go vet ./...
	go test -race ./...

# Ships what main has, never what is on disk: the tree must be clean and match origin/main, and the tests must pass.
# Needs the Railway CLI, logged in and linked to the farol-relay project from this folder.
deploy: test
	@git fetch -q origin main
	@test "$$(git rev-parse --abbrev-ref HEAD)" = main || { echo "Deploy from main."; exit 1; }
	@test -z "$$(git status --porcelain -- .)" || { echo "relay/ has changes that are not committed."; exit 1; }
	@test "$$(git rev-parse HEAD)" = "$$(git rev-parse origin/main)" || { echo "main is not the same as origin/main. Pull or push first."; exit 1; }
	railway up --service relay --detach
	@echo "Deploying $$(git rev-parse --short HEAD). Run make logs, and wait for: farol relay on :8080, notifications on"

logs:
	railway logs --service relay
