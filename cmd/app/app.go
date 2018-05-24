package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/SprintHubNigeria/google_spreadsheet/pkg/sheetservice"

	"github.com/SprintHubNigeria/google_spreadsheet/pkg/sheetdata"
	"github.com/gobuffalo/envy"

	"github.com/sendgrid/sendgrid-go/helpers/mail"

	"github.com/pkg/errors"

	"github.com/sendgrid/sendgrid-go"

	"google.golang.org/api/sheets/v4"
)

const (
	// Email parameters
	messageSender  = "SprintHub"
	fromEmail      = "noreply@sprinthub.com.ng"
	messageSubject = "Co-working Space Subscription Expiry"
	messageText    = "Your SprintHub co-working space subscription will expire in %s days. You can contact us to renew your subscription."
	// ErrFmtMissingEnvVar will be raised when required environment variables are missing
	ErrFmtMissingEnvVar = "Missing environment variable %s"
)

var (
	enableSandboxMode bool
	sendGridAPIKey    string
	clientSecret      string
	sheetsAPIToken    string
	spreadsheetID     string
	env               string
	port              string
	cronHeader        string
	// Data range to be read from the spreadsheet
	readRange     string
	srv           *sheets.Service
	emailTemplate *template.Template
	mailClient    *sendgrid.Client
)

func main() {
	if err := setupEnvVars(map[string]*string{
		"SENDGRID_API_KEY": &sendGridAPIKey,
		"SPREADSHEET_ID":   &spreadsheetID,
		"READ_RANGE":       &readRange,
		"ENV":              &env,
		"CLIENT_SECRET":    &clientSecret,
		"PORT":             &port,
		"CRON_HEADER":      &cronHeader,
	}); err != nil {
		log.Fatalf("%+v\n", err)
	}
	if env == "dev" {
		enableSandboxMode = true
	}
	srv = sheetsservice.NewSheetsService([]byte(clientSecret))
	if srv == nil {
		log.Fatalln("Sheets service configuration failed")
	}
	// Email template for the message
	emailTemplate = template.Must(template.ParseFiles("email-template.html"))
	mailClient = sendgrid.NewSendClient(sendGridAPIKey)
	// Create ServeMux and register HTTP handler
	server := http.NewServeMux()
	server.HandleFunc("/", cronPingHandler)
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
	}
	log.Printf("Listening on %s:%s", hostname, port)
	log.Fatalf("Server crashed with error: %+v\n", http.ListenAndServe(fmt.Sprintf("%s:%s", hostname, port), server))
}

func cronPingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-SPRINTHUB-CRON") != cronHeader {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	resp, err := srv.Spreadsheets.Values.Get(spreadsheetID, readRange).Do()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(resp.Values) == 0 {
		http.Error(w, errors.New("Missing sheets data").Error(), http.StatusInternalServerError)
		return
	}
	wg := sync.WaitGroup{}
	for _, row := range resp.Values {
		wg.Add(1)
		go func(row []interface{}) {
			data, err := sheetdata.NewSheetEntry(row)
			if err != nil {
				log.Println(errors.WithMessage(err, "Failed to parse data from spreadsheet."))
			}
			// Determine whether there's between 0 and 24 hours left
			if shouldSendEmail(data.DaysLeft()) {
				// Send email notification
				if err := sendEmail(data); err != nil {
					log.Printf("%+v\n%+v\n", err, data)
					wg.Done()
					return
				}
				log.Printf("Sent email to %s at %s\n", data.FullName(), data.Email)
			} else {
				log.Printf("Not sending email to %s. Days left: %d\n", data.Email, data.DaysLeft())
			}
			wg.Done()
		}(row)
	}
	wg.Wait()
	w.WriteHeader(http.StatusOK)
}

// shouldSendEmail determines whether a hub user should be emailed
func shouldSendEmail(daysToExpiry int) bool {
	if daysToExpiry == 1 || daysToExpiry == 3 || daysToExpiry == 7 {
		return true
	}
	return false
}

func sendEmail(data sheetdata.SheetEntry) error {
	from := mail.NewEmail(messageSender, fromEmail)
	to := mail.NewEmail(data.FirstName, data.Email)
	text := fmt.Sprintf(messageText, strconv.Itoa(data.DaysLeft()))
	msgBytes := bytes.NewBuffer([]byte{})
	daysLeft := strconv.Itoa(data.DaysLeft()) + " day"
	if data.DaysLeft() > 1 {
		daysLeft = daysLeft + "s"
	}
	err := emailTemplate.Execute(msgBytes, struct {
		FirstName, TimeLeft string
	}{
		FirstName: data.FirstName,
		TimeLeft:  daysLeft,
	})
	if err != nil {
		return errors.WithMessage(err, "Cannot execute HTML template.")
	}
	message := mail.NewSingleEmail(from, messageSubject, to, text, msgBytes.String())
	message.SetMailSettings(&mail.MailSettings{
		SandboxMode: &mail.Setting{
			Enable: &enableSandboxMode,
		},
	})
	_, err = mailClient.Send(message)
	if err != nil {
		return errors.WithMessage(err, "Message sending failed.")
	}
	//log.Printf("Response: %v\n", response)
	return nil
}

func setupEnvVars(vars map[string]*string) error {
	for envVar, dest := range vars {
		val, err := envy.MustGet(envVar)
		if err != nil {
			return errors.Errorf(ErrFmtMissingEnvVar, envVar)
		}
		*dest = val
	}
	return nil
}
