FROM golang:1.24-alpine

WORKDIR /app

# Install build tools
RUN apk add --no-cache git make

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application
COPY . .

# Build the application
RUN go build -o main ./cmd/api

# Expose the application port
EXPOSE 8080

# Command to run the application
CMD ["./main"] 
