build: format
	go build

format:
	gofmt -w main.go
