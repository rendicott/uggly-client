module uggly-client

go 1.15

replace github.com/rendicott/uggly-client/boxes => ./boxes

replace github.com/rendicott/uggly => ../uggly

require (
	github.com/gdamore/tcell/v2 v2.1.0
	github.com/go-stack/stack v1.8.0 // indirect
	github.com/golang/protobuf v1.4.3 // indirect
	github.com/inconshreveable/log15 v0.0.0-20201112154412-8562bdadbbac
	github.com/mattn/go-colorable v0.1.8 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/rendicott/uggly v0.0.0
	github.com/rendicott/uggly-client/boxes v0.0.0
	google.golang.org/grpc v1.34.0
)
