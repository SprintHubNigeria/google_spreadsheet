FROM golang:alpine as go-build

WORKDIR /go/src/github.com/SprintHubNigeria/google_spreadsheet
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go install ./...


FROM alpine:latest

RUN apk update && apk add ca-certificates
RUN rm -rf /var/cache/apk/*

WORKDIR /srv
COPY --from=go-build /go/bin/app .
COPY client_secret.json .
COPY token.json .
COPY email-template.html .
COPY .env .
EXPOSE 8080 80

CMD ["/srv/app"]
