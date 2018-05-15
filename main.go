package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sendgrid/sendgrid-go/helpers/mail"

	"github.com/pkg/errors"

	"github.com/sendgrid/sendgrid-go"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/sheets/v4"
)

const readRange = "Sheet1!A3:F"

var (
	enableSandboxMode bool
	sendGridAPIKey    string
	spreadsheetID     string
	// ErrMissingEnvVar will be raised when required environment variables are missing
	ErrMissingEnvVar = errors.New("Missing environment variable")
	srv              *sheets.Service
	emailTemplate    *template.Template
	mailClient       *sendgrid.Client
)

func init() {
	srv = newSheetsService("client_secret.json")
	if srv == nil {
		log.Fatalln("Sheets service configuration failed")
	}
	if err := setupEnvVars(map[string]*string{
		"SENDGRID_API_KEY": &sendGridAPIKey,
		"SPREADSHEET_ID":   &spreadsheetID,
	}); err != nil {
		log.Fatalln(err)
	}
	// Email template for the message
	emailTemplate = template.Must(template.ParseFiles("email-template.html"))
	mailClient = sendgrid.NewSendClient(sendGridAPIKey)
}

func main() {

}

type emailData struct {
	FirstName string
	TimeLeft  string
}

type sheetEntry struct {
	firstName string
	lastName  string
	email     string
	endDate   time.Time
}

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
	return sheetEntry{
		email:     email,
		endDate:   expiryDate,
		firstName: firstName,
		lastName:  lastName,
	}, nil
}

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config) *http.Client {
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(oauth2.NoContext, authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	defer f.Close()
	if err != nil {
		return nil, err
	}
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	defer f.Close()
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	json.NewEncoder(f).Encode(token)
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
		*dest = os.Getenv(envVar)
		if *dest == "" {
			return ErrMissingEnvVar
		}
	}
	return nil
}

func newSheetsService(secretsFile string) *sheets.Service {
	clientSecret, err := ioutil.ReadFile("client_secret.json")
	if err != nil {
		log.Printf("Unable to read client secret file: %v", err)
		return nil
	}

	// If modifying these scopes, delete your previously saved client_secret.json.
	config, err := google.ConfigFromJSON(clientSecret, sheets.SpreadsheetsReadonlyScope)
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

func handleMailPing(w http.ResponseWriter, r *http.Request) {
	resp, err := srv.Spreadsheets.Values.Get(spreadsheetID, readRange).Do()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	if len(resp.Values) == 0 {
		http.Error(w, errors.New("Missing sheets data").Error(), http.StatusInternalServerError)
	} else {
		for _, row := range resp.Values {
			// Parse the data from the spreadsheet and clean them for use
			data, err := newSheetEntry(row)
			if err != nil {
				log.Println(errors.WithMessage(err, "Failed to parse data from spreadsheet."))
				continue
			}
			// Determine whether there's between 0 and 24 hours left
			now := time.Now().UTC()
			if timeLeft := data.endDate.Sub(now); timeLeft.Hours() < 24 {
				// Send email notification
				from := mail.NewEmail("SprintHub", "noreply@sprinthub.com.ng")
				to := mail.NewEmail(data.firstName, "jthankgod@ymail.com")
				subject := "Co-working Space Subscription Expiry"
				messageText := "Your SprintHub co-working space subscription will expire in" + strconv.Itoa(int(timeLeft.Hours())) + "hours."
				msgBytes := bytes.NewBuffer([]byte{})
				err := emailTemplate.Execute(msgBytes, emailData{
					FirstName: data.firstName,
					TimeLeft:  strconv.Itoa(int(timeLeft.Hours())) + "hours",
				})
				if err != nil {
					log.Println(errors.WithMessage(err, "Cannot execute HTML template."))
					continue
				}
				message := mail.NewSingleEmail(from, subject, to, messageText, msgBytes.String())
				// message.SetMailSettings(&mail.MailSettings{
				// 	SandboxMode: &mail.Setting{
				// 		Enable: &enableSandboxMode,
				// 	},
				// })
				response, err := mailClient.Send(message)
				if err != nil {
					log.Println(errors.WithMessage(err, "Message sending failed."))
				}
				log.Println(response)
			}
		}
	}
}
