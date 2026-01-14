# Build Stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod ./
# COPY go.sum ./ # Not needed yet as we haven't run go mod tidy

# Download dependencies
# Since we can't run go mod tidy in the environment without network/go, 
# we rely on go get running during build or pre-vendor.
# However, standard practice is to copy source and build.
RUN go mod download

COPY . .

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -o bridge main.go

# Runtime Stage
FROM alpine:latest

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/bridge .

# Expose port
EXPOSE 8080

# Command to run
CMD ["./bridge"]
