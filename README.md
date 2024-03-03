# Training: Distributed Services with Go

According to the book by Traevis Jeffery, this repository implements the code examples from the book with slight modifications.

Implemented:

- Chapter 1: Commit Log Prototype
- Chapter 2: Structure Data with Protocol Buffers
- Chapter 3: Building a Log Package (i.e, using generated Protobuf to implement logs)
- Chapter 4: Serve Requests with ~gRPC~ ConnectRPC
- Chapter 5: Secure your Services (i.e, use mutual TLS)
- Chapter 6: Observe your systems (i.e, use OpenTelemetry for metrics and traces, slog for structured logging)

Modifications:

- Instead of using a 3rd party library for mmap, I used the `syscall` standard library.
- Some error handling is done differently.
- gRPC is replaced with ConnectRPC, an alternative to gRPC with gRPC-compatible APIs.
- Buf as a standard to generate Protobuf and gRPC code.

This training is a work in progress and will be updated as I progress through the book.

I also take a step back to avoid bad practices.

For now, the book seems quite good.
