# Dockerfile

# ---- Build Stage ----
FROM golang:1.21 AS builder

# Set the working directory
WORKDIR /app

# Copy go.mod and go.sum files to download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the static binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -o /controller .

# ---- Final Stage ----
FROM gcr.io/distroless/static:nonroot

# Copy the static binary from the builder stage
COPY --from=builder /controller /controller

# Set the user to nonroot
USER nonroot

# Set the entrypoint
ENTRYPOINT ["/controller"]
