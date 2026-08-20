package main

import "log"

func main() {
	// TODO: consume chat messages + moderation events off RabbitMQ and
	// write them to Postgres. No server to run - this is a pure consumer.
	log.Println("persistence-worker starting")
}
