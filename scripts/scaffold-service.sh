#!/usr/bin/env bash
# Scaffolds a minimal gRPC service skeleton under services/<name>: its own Go
# module plus a main.go that starts a gRPC server with health checking and
# reflection wired up, and nothing else. Run once per new gRPC service, then
# edit that service's main.go directly - this script is not meant to be
# re-run against a service that already has real logic in it.
#
# Usage: scripts/scaffold-service.sh <service-name> <default-grpc-port>
set -euo pipefail

if [ $# -ne 2 ]; then
  echo "usage: $0 <service-name> <default-grpc-port>" >&2
  exit 1
fi

NAME=$1
PORT=$2
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIR="$ROOT/services/$NAME"
MODULE="github.com/Flamingo-Apps/Flamingo-Chat/services/$NAME"

if [ -e "$DIR" ]; then
  echo "refusing to overwrite existing $DIR" >&2
  exit 1
fi

mkdir -p "$DIR"
cd "$DIR"

go mod init "$MODULE" >/dev/null

cat > main.go <<EOF
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
	port := config.String("GRPC_PORT", "$PORT")

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	srv := grpc.NewServer()

	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(srv, healthSrv)
	reflection.Register(srv)

	// TODO: register the ${NAME} service implementation.

	log.Printf("${NAME} listening on :%s", port)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
EOF

echo "scaffolded $DIR"
