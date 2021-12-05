FROM golang:1.17.4 as builder
WORKDIR /build
COPY go.mod .
COPY go.sum .
RUN go mod download

# Build
COPY . .
RUN git rev-parse --short HEAD
RUN GIT_COMMIT=$(git rev-parse --short HEAD) && \
    CGO_ENABLED=0 go build -o app -ldflags "-X main.GitCommit=${GIT_COMMIT}"


FROM docker.na1.hsdp.io/loafoe/nomad:latest
RUN adduser nomad
RUN mkdir -p /plugins
COPY --from=builder /build/app /plugins/nomad-driver-ch
USER nomad
