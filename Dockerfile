# Docker is not the primary way to run ON Suite — a single binary and a data
# directory is — but the option costs almost nothing to maintain.
FROM golang:1.26-alpine AS build

WORKDIR /src
# Copy the module files first so dependency download is cached separately from
# the source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=docker
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/onsuite ./cmd/onsuite

# scratch, not alpine: the binary is static, so there is nothing else to ship.
FROM scratch

# Root certificates, needed only if built-in TLS is used — autocert has to
# verify Let's Encrypt's own certificate.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/onsuite /onsuite

# An unprivileged numeric user; scratch has no /etc/passwd to name one in.
USER 65532:65532

VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["/onsuite"]
CMD ["serve", "--data-dir", "/data", "--addr", ":8080"]
