FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY *.go ./
COPY migrations ./migrations
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /unitech-api .

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app && mkdir /data && chown app /data
USER app
COPY --from=build /unitech-api /usr/local/bin/unitech-api
ENV ADDR=:8080 DATA_PATH=/data/unitech.gob
EXPOSE 8080
CMD ["unitech-api"]
