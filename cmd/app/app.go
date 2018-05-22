package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/SprintHubNigeria/google_spreadsheet/pkg/model/sheetdata"
	"github.com/gobuffalo/envy"

	"github.com/sendgrid/sendgrid-go/helpers/mail"

	"github.com/pkg/errors"

	"github.com/sendgrid/sendgrid-go"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/sheets/v4"
)

const (
	// Email parameters
	messageSender  = "SprintHub"
	fromEmail      = "noreply@sprinthub.com.ng"
	messageSubject = "Co-working Space Subscription Expiry"
	messageText    = "Your SprintHub co-working space subscription will expire in %s days. You can contact us to renew your subscription."
	// Data range to be read from the spreadsheet
	readRange = "Hub List!A3:F"
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
	srv               *sheets.Service
	emailTemplate     *template.Template
	mailClient        *sendgrid.Client
	tokenFile         string = "token.json"
)

func main() {
	if err := setupEnvVars(map[string]*string{
		"SENDGRID_API_KEY": &sendGridAPIKey,
		"SPREADSHEET_ID":   &spreadsheetID,
		"ENV":              &env,
		"CLIENT_SECRET":    &clientSecret,
		"PORT":             &port,
		"CRON_HEADER":      &cronHeader,
	}); err != nil {
		log.Fatalf("%+v\n", err)
	}
	sheetsAPIToken = envy.Get("TOKEN", "")
	if env == "dev" {
		enableSandboxMode = true
	}
	srv = newSheetsService([]byte(clientSecret))
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

func shouldSendEmail(daysToExpiry int) bool {
	if daysToExpiry == 1 || daysToExpiry == 3 || daysToExpiry == 7 {
		return true
	}
	return false
}

func newSheetsService(secret []byte) *sheets.Service {
	// If modifying these scopes, delete your previously saved client_secret.json.
	config, err := google.ConfigFromJSON(secret, sheets.SpreadsheetsReadonlyScope)
	if err != nil {
		log.Printf("Unable to parse client secret file to config: %v", err)
		return nil
	}
	client := getClient(config)
	if client == nil {
		return nil
	}
	srv, err := sheets.New(client)
	if err != nil {
		log.Printf("Unable to retrieve Sheets client: %v", err)
		return nil
	}
	return srv
}

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config) *http.Client {
	tok, err := tokenFromEnvOrFile(tokenFile)
	if err != nil {
		tok, err = getTokenFromWeb(config)
		if err != nil {
			log.Printf("Unable to retrieve token from web: %+v\n", err)
			return nil
		}
		if err = saveToken("token.json", tok); err != nil {
			log.Printf("%+v\n", err)
		}
	}
	return config.Client(context.Background(), tok)
}

// Request a token from the web, then return the retrieved token.
func getTokenFromWeb(config *oauth2.Config) (*oauth2.Token, error) {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		return nil, errors.WithMessage(err, "Scanning for token failed")
	}

	tok, err := config.Exchange(oauth2.NoContext, authCode)
	if err != nil {
		return nil, errors.WithMessage(err, "Token exchange failed")
	}
	return tok, nil
}

// Retrieves a token from a local file. It first checks the "TOKEN" environment variable for the token.
func tokenFromEnvOrFile(file string) (*oauth2.Token, error) {
	var (
		tok = &oauth2.Token{}
		err error
	)
	// Try reading from the environment variable
	if sheetsAPIToken != "" {
		err = json.NewDecoder(bytes.NewReader([]byte(sheetsAPIToken))).Decode(tok)
	} else {
		f, err := ioutil.ReadFile(file)
		if err != nil {
			return nil, err
		}
		buf := bytes.NewReader(f)
		err = json.NewDecoder(buf).Decode(tok)
	}
	return tok, err
}

// Saves a token to a file path.
func saveToken(tokenFilePath string, token *oauth2.Token) error {
	f, err := os.Create(tokenFilePath)
	if err != nil {
		return errors.WithMessage(err, "Unable to create file "+tokenFilePath)
	}
	if err = json.NewEncoder(f).Encode(token); err != nil {
		return err
	}
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
