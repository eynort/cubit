# El bot en un contenedor: un binario estático sobre alpine (con certificados
# para Microsoft, los webhooks y las IAs por https).
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY third_party ./third_party
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /minecraft-bot .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /minecraft-bot /usr/local/bin/minecraft-bot
# bot.json, datos/ y logs/ van aquí (un volumen: sobreviven al contenedor)
WORKDIR /data
EXPOSE 8080
ENTRYPOINT ["minecraft-bot"]
