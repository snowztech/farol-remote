FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /relay .

# Nothing but the binary. TLS is the host's job, in front of this.
FROM scratch
# The list of authorities the relay trusts, so it can reach Apple for notifications.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /relay /relay
USER 65534
EXPOSE 8080
ENTRYPOINT ["/relay"]
