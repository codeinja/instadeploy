FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /agent .

# The docker CLI (with the Compose and Buildx plugins) is used for builds
# and Compose deployments; git for Git deployments. Everything else goes
# through the Docker Engine API.
#
# The agent needs root (or the docker group) to use /var/run/docker.sock.
FROM alpine:3.22
RUN apk add --no-cache ca-certificates docker-cli docker-cli-compose docker-cli-buildx git
COPY --from=build /agent /agent
ENTRYPOINT ["/agent"]
