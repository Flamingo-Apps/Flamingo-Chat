package main

import (
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/config"
)

func main() {
	port := config.String("GRPC_PORT", "50054")

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	srv := grpc.NewServer()

	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(srv, healthSrv)
	reflection.Register(srv)

	// TODO: register the presence service implementation.

	log.Printf("presence listening on :%s", port)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
