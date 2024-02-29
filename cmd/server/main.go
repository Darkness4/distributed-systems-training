package main

import (
	"distributed-systems/internal/server"
	"log/slog"
	"net/http"
)

func main() {
	r := server.New(server.NewLog()).HandleLog("/")

	slog.Info("Server started at :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		panic(err)
	}
}
