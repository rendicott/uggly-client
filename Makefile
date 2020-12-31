build: format
	go build

format:
	gofmt -w main.go
	gofmt -w boxes/boxes.go
	gofmt -w boxes/wrap.go
	gofmt -w ugcon/ugcon.go
