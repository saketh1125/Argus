FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/codesentinel .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates git python3 nodejs npm \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --create-home --uid 10001 app
COPY --from=build /usr/local/go /usr/local/go
COPY --from=build /out/codesentinel /usr/local/bin/codesentinel
ENV PATH="/usr/local/go/bin:/usr/local/bin:${PATH}" \
    GOPATH=/home/app/go \
    GOCACHE=/tmp/go-cache
USER 10001:10001
WORKDIR /home/app
ENTRYPOINT ["codesentinel"]
