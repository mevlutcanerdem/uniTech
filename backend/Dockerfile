FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
# PostgreSQL sürücüsü sadece sunucu derlemesinde eklenir; yerel derleme bağımlılıksız kalır.
RUN go get github.com/jackc/pgx/v5@v5.7.2 && CGO_ENABLED=0 go build -tags postgres -o /unitech-api .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app && mkdir /data && chown app /data
USER app
COPY --from=build /unitech-api /usr/local/bin/unitech-api
ENV ADDR=:8080 DATA_PATH=/data/unitech.json
VOLUME /data
EXPOSE 8080
CMD ["unitech-api"]
