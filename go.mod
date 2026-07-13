module uggly-client

go 1.15

replace github.com/rendicott/uggly-client/boxes => ./boxes

replace github.com/rendicott/uggly-client/ugcon => ./ugcon

replace github.com/rendicott/uggly => ../uggly

replace github.com/rendicott/uggo => ../uggo

replace github.com/rendicott/ugform => ../ugform

replace github.com/rendicott/uggsec => ../uggsec

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/gdamore/tcell/v2 v2.4.0
	github.com/inconshreveable/log15 v0.0.0-20201112154412-8562bdadbbac
	github.com/rendicott/ugform v0.0.2
	github.com/rendicott/uggly v0.1.2
	github.com/rendicott/uggly-client/boxes v0.0.0
	github.com/rendicott/uggly-client/ugcon v0.0.0
	github.com/rendicott/uggo v0.0.2
	github.com/rendicott/uggsec v0.0.0-20220417162920-8d8282e3a927
	golang.org/x/term v0.0.0-20210503060354-a79de5458b56 // indirect
	google.golang.org/grpc v1.45.0
	google.golang.org/protobuf v1.28.0 // indirect
	gopkg.in/yaml.v3 v3.0.0-20200313102051-9f266ea9e77c
)
