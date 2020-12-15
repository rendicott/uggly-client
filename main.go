package main

import (
	"flag"
	"log"
    "context"
	"time"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/examples/data"
	pb "github.com/rendicott/uggly"
)

var (
	tls                = flag.Bool("tls", false, "Connection uses TLS if true, else plain TCP")
	caFile             = flag.String("ca_file", "", "The file containing the CA root cert file")
	serverAddr         = flag.String("server_addr", "localhost:10000", "The server address in the format of host:port")
	serverHostOverride = flag.String("server_host_override", "x.test.youtube.com", "The server name used to verify the hostname returned by the TLS handshake")
)

func main() {
	flag.Parse()
	var opts []grpc.DialOption
	if *tls {
		if *caFile == "" {
			*caFile = data.Path("x509/ca_cert.pem")
		}
		creds, err := credentials.NewClientTLSFromFile(*caFile, *serverHostOverride)
		if err != nil {
			log.Fatalf("Failed to create TLS credentials %v", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}
	opts = append(opts, grpc.WithBlock())
	conn, err := grpc.Dial(*serverAddr, opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	defer conn.Close()
	client := pb.NewScreenerClient(conn)
    log.Println("ScreenerClient created")
    ss := pb.ScreenSet{
        Id: 1,
        Name: "one",
    }
    ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
    stream, err := client.GetScreens(ctx, &ss)
    if err != nil {
        panic(err)
    }
	for {
		screen, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("%v.GetScreens(_) = _, %v", client, err)
		}
		log.Printf("Screen: id: %d, contents('%s'", screen.GetId(),
			screen.GetContents())
	}
}
