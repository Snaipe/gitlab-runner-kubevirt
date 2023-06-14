ARG GITLAB_RUNNER_VERSION=v15.11.1

FROM golang:1.20-alpine AS build

WORKDIR /src

ARG VERSION
ENV VERSION=${VERSION:-v0.0.0}

RUN --mount=target=. \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg \
    go build -trimpath -ldflags='-extldflags=-static -w -s -X main.version='"$VERSION" -o /out/gitlab-runner-kubevirt .

FROM gitlab/gitlab-runner:alpine-${GITLAB_RUNNER_VERSION}

COPY --from=build /out/gitlab-runner-kubevirt /bin/gitlab-runner-kubevirt
