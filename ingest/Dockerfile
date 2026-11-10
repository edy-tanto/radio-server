FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /ingest .

FROM alpine:3.20
RUN apk add --no-cache ffmpeg
COPY --from=build /ingest /usr/local/bin/ingest
EXPOSE 8080
CMD ["ingest"]
