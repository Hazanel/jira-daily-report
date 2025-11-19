# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy source files
COPY go.mod ./
COPY main.go slash-server.go ./

# Build the application
RUN go build -ldflags="-s -w" -o jira_update main.go slash-server.go

# Runtime stage
FROM alpine:latest

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/jira_update .

# Expose port (OpenShift will map this)
EXPOSE 8080

# Run in server mode
CMD ["./jira_update", "-server"]
