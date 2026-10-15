.PHONY: test logs

test:
	go vet ./...
	go test -race ./...

logs:
	railway logs --service relay
