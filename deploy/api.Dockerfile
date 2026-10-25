FROM golang:1.23-alpine AS build

WORKDIR /src

ENV CGO_ENABLED=0 \
    GOFLAGS=-buildvcs=false

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations

RUN go build -trimpath -ldflags "-s -w" -o /out/api ./cmd/api \
 && go build -trimpath -ldflags "-s -w" -o /out/migrate ./cmd/migrate

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /out/api /app/api
COPY --from=build /out/migrate /app/migrate

USER nonroot:nonroot

EXPOSE 8080

CMD ["/app/api"]
