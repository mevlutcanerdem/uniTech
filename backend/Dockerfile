FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -o /unitech-api .

FROM alpine:3.20
RUN adduser -D -u 10001 app && mkdir /data && chown app /data
USER app
COPY --from=build /unitech-api /usr/local/bin/unitech-api
ENV ADDR=:8080 DATA_PATH=/data/unitech.json
VOLUME /data
EXPOSE 8080
CMD ["unitech-api"]
