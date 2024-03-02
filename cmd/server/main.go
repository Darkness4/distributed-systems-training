package main

import (
	"distributed-systems/internal/server"
	"log/slog"
	"net/http"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func main() {
	r := http.NewServeMux()
	path, handler := server.NewLogAPIHandler(&server.Config{})
	r.Handle(path, handler)

	slog.Info("Server started at :8080")
	if err := http.ListenAndServe(":8080", h2c.NewHandler(r, &http2.Server{})); err != nil {
		panic(err)
	}
}
