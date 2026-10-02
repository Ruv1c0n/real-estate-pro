FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN go mod tidy && CGO_ENABLED=0 go build -o /notifier .

FROM alpine:3.20
COPY --from=build /notifier /notifier
CMD ["/notifier"]