FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /backend .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app \
    && mkdir /data && chown app:app /data
USER app
# Uploaded build contexts and the generated secrets key.
VOLUME /data
ENV DATA_DIR=/data
COPY --from=build /backend /backend
EXPOSE 8080
ENTRYPOINT ["/backend"]
CMD ["serve"]
