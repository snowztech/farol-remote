FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /relay .

# Nothing but the binary. TLS is the host's job, in front of this.
FROM scratch
COPY --from=build /relay /relay
USER 65534
EXPOSE 8080
ENTRYPOINT ["/relay"]
