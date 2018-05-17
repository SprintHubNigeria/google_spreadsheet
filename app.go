package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

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
	messageText    = "Your SprintHub co-working space subscription will expire in %s hours. You can contact us to renew your subscription."
	// Data range to be read from the spreadsheet
	readRange = "Sheet1!A3:F"
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
	srv               *sheets.Service
	emailTemplate     *template.Template
	mailClient        *sendgrid.Client
)

func main() {
	if err := setupEnvVars(map[string]*string{
		"SENDGRID_API_KEY": &sendGridAPIKey,
		"SPREADSHEET_ID":   &spreadsheetID,
		"ENV":              &env,
		"CLIENT_SECRET":    &clientSecret,
		"TOKEN":            &sheetsAPIToken,
		"PORT":             &port,
	}); err != nil {
		log.Fatalf("%+v\n", err)
	}
	if env == "dev" {
		enableSandboxMode = true
	}
	if srv = newSheetsService([]byte(clientSecret)); srv == nil {
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
			data, err := newSheetEntry(row)
			if err != nil {
				log.Println(errors.WithMessage(err, "Failed to parse data from spreadsheet."))
			}
			// Determine whether there's between 0 and 24 hours left
			if data.TimeLeft.Hours() < 24 {
				// Send email notification
				if err := sendEmail(data); err != nil {
					log.Println(err)
				}
			}
			wg.Done()
		}(row)
	}
	wg.Wait()
	w.WriteHeader(http.StatusOK)
}

func sendEmail(data sheetEntry) error {
	from := mail.NewEmail(messageSender, fromEmail)
	to := mail.NewEmail(data.FirstName, "jthankgod@ymail.com")
	text := fmt.Sprintf(messageText, strconv.Itoa(int(data.TimeLeft.Hours())))
	msgBytes := bytes.NewBuffer([]byte{})
	err := emailTemplate.Execute(msgBytes, struct {
		FirstName, TimeLeft string
	}{
		FirstName: data.FirstName,
		TimeLeft:  strconv.Itoa(int(data.TimeLeft.Hours())) + " hours",
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
	response, err := mailClient.Send(message)
	if err != nil {
		return errors.WithMessage(err, "Message sending failed.")
	}
	log.Printf("Response: %v\n", response)
	return nil
}

type sheetEntry struct {
	FirstName string
	LastName  string
	Email     string
	EndDate   time.Time
	TimeLeft  time.Duration
}

// Parse the data from the spreadsheet and clean them for use
func newSheetEntry(data []interface{}) (sheetEntry, error) {
	// Parse the time from the response
	firstName, ok := data[0].(string)
	if !ok {
		return sheetEntry{}, errors.New("Unexpected first name value")
	}
	lastName, ok := data[1].(string)
	if !ok {
		return sheetEntry{}, errors.New("Unexpected last name value")
	}
	date, ok := data[5].(string)
	if !ok {
		return sheetEntry{}, errors.New("Unexpected date value")
	}
	email, ok := data[3].(string)
	if !ok {
		return sheetEntry{}, errors.New("Unexpected email value")
	}
	now := time.Now().UTC()
	expiryDate, err := timeFromSheet(date, now)
	if err != nil {
		return sheetEntry{}, errors.WithMessage(err, "Bad time value")
	}
	timeLeft := expiryDate.Sub(now)
	return sheetEntry{
		Email:     email,
		EndDate:   expiryDate,
		FirstName: firstName,
		LastName:  lastName,
		TimeLeft:  timeLeft,
	}, nil
}

func newSheetsService(secret []byte) *sheets.Service {
	// If modifying these scopes, delete your previously saved client_secret.json.
	config, err := google.ConfigFromJSON(secret, sheets.SpreadsheetsReadonlyScope)
	if err != nil {
		log.Printf("Unable to parse client secret file to config: %v", err)
		return nil
	}
	client := getClient(config)

	srv, err := sheets.New(client)
	if err != nil {
		log.Printf("Unable to retrieve Sheets client: %v", err)
		return nil
	}
	return srv
}

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config) *http.Client {
	ctx := context.Background()
	tok, err := tokenFromEnvOrFile("token.json")
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
	client := config.Client(ctx, tok)
	return client
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
	} else if f, err := os.Open(file); err == nil {
		// Try reading the token from file
		defer f.Close()
		err = json.NewDecoder(f).Decode(tok)
	}
	log.Printf("%+v\n", err)
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

// timeFromSheet converts the time gotten from the spreadsheet to a time.Time value
// If there is a problem during the conversion, it returns a time.Time value with the default zero values
// and an error. If the conversion succeeds, it returns the converted time and no error.
func timeFromSheet(date string, now time.Time) (time.Time, error) {
	expires := strings.Split(date, "/")
	y, err := strconv.Atoi("20" + expires[2])
	if err != nil {
		return time.Time{}, err
	}
	mInt, err := strconv.Atoi(expires[1])
	if err != nil {
		return time.Time{}, err
	}
	m := time.Month(mInt)
	d, err := strconv.Atoi(expires[0])
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location()).UTC(), nil
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
