# build
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X github.com/mehrabix/kubectl-ripple/internal/cli.Version=${VERSION} \
      -X github.com/mehrabix/kubectl-ripple/internal/cli.Commit=${COMMIT} \
      -X github.com/mehrabix/kubectl-ripple/internal/cli.BuildDate=${BUILD_DATE}" \
    -o /out/kubectl-ripple ./cmd/kubectl-ripple

# runtime: static, nonroot, no shell
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/kubectl-ripple /kubectl-ripple
USER nonroot:nonroot
ENTRYPOINT ["/kubectl-ripple"]
