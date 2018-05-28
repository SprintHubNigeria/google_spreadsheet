FROM golang:alpine as go-build

WORKDIR /go/src/github.com/SprintHubNigeria/google_spreadsheet
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go install ./...


FROM golang:alpine

WORKDIR /srv
COPY --from=go-build /go/bin/app .
COPY client_secret.json .
COPY token.json .
COPY email-template.html .

CMD ["/srv/app"]
