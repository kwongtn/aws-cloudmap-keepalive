# Stage 1: Build the Go binary
FROM golang:1.23 AS builder

# Set the working directory
WORKDIR /app

# Copy and download dependency files
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the Go binary
RUN CGO_ENABLED=0 go build -o main .

# Stage 2: Minimal runtime image
FROM alpine:3.21

WORKDIR /root/
COPY --from=builder /app/main .

CMD ["./main"]
