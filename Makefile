build: format
	go build

format:
	gofmt -w main.go
	gofmt -w boxes/boxes.go
