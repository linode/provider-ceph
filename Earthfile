VERSION --use-cache-command 0.7
FROM golang:1.19
WORKDIR /workdir

all:
    WAIT
        BUILD +go-lint
        BUILD +go-test
    END
    WAIT
        BUILD +go-sec
    END

go-lint:
    FROM earthly/dind:alpine
    COPY . ./workdir
    WITH DOCKER --pull golangci/golangci-lint:v1.51.0
        RUN docker run -w /workdir -v /workdir:/workdir golangci/golangci-lint:v1.51.0 golangci-lint run --timeout 500s
    END

go-sec:
    FROM earthly/dind:alpine
    COPY . ./workdir
    WITH DOCKER --pull securego/gosec:2.15.0
        RUN docker run -w /workdir -v /workdir:/workdir securego/gosec:2.15.0 -exclude-dir=bin -exclude-dir=drivers -exclude-generated ./...
    END

go-test:
    FROM +deps-go-build
    CACHE $HOME/.cache/go-build
    COPY . ./
    RUN make go.test.unit

    SAVE ARTIFACT --if-exists --force _output/tests AS LOCAL _output/tests

deps-submodules:
    LOCALLY
    RUN make submodules

deps-go:
    BUILD +deps-submodules
    COPY go.mod go.sum ./
    RUN go mod download

deps-go-build:
    FROM +deps-go
    COPY --dir build build
    COPY Makefile ./
    RUN make generate