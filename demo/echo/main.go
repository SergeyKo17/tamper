package main

import (
	"context"
	"log"
	"net"

	"github.com/SergeyKo17/tamper/demo/echo/echopb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type echoServer struct {
	echopb.UnimplementedEchoServer
}

func (echoServer) Say(_ context.Context, req *echopb.SayRequest) (*echopb.SayResponse, error) {
	return &echopb.SayResponse{Text: req.GetText()}, nil
}

const addr = "0.0.0.0:50051"

func main() {
	// Inside the container the proxy reaches the mock over the compose
	// network, so loopback would not do.
	var lc net.ListenConfig
	//nolint:gosec // G102: demo stack only
	lis, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}

	srv := grpc.NewServer()
	echopb.RegisterEchoServer(srv, echoServer{})
	reflection.Register(srv)

	log.Printf("echo listening on %s", addr)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
