FROM golang:1.23

WORKDIR /usr/src/app

# Copying go.mod results lets us redownload dependencies only when go.mod
# changes.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

RUN go run github.com/playwright-community/playwright-go/cmd/playwright@latest \
  install --with-deps

COPY . .
RUN go build -v -o /usr/local/bin/app

ENTRYPOINT ["app"]
