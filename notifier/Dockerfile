FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
# go mod download выполнен выше (слои go.mod/go.sum кэшируются);
# tidy здесь не нужен — go.sum коммичен, зависимости не мутируют при сборке.
RUN CGO_ENABLED=0 go build -o /notifier .

FROM alpine:3.24
COPY --from=build /notifier /notifier
CMD ["/notifier"]