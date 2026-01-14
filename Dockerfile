# Build Stage
FROM golang:1.21-alpine AS builder

# Set Go Proxy for China users to ensure build success
ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /app

# Copy go mod files
COPY go.mod ./
# If go.sum exists, copy it. If not, go mod download will generate what it needs.
# COPY go.sum ./ 

# Download dependencies
RUN go mod download

COPY . .

# Build statically linked binary
# GOARCH will be automatically detected by the builder platform, 
# or we can rely on HA Supervisor to handle multi-arch build.
RUN CGO_ENABLED=0 GOOS=linux go build -o bridge main.go

# Runtime Stage
FROM alpine:latest

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/bridge .

# Ensure binary is executable
RUN chmod +x bridge

# Expose port
EXPOSE 8080

# Command to run
CMD ["./bridge"]
