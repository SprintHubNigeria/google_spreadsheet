package sheetsservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/pkg/errors"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	sheets "google.golang.org/api/sheets/v4"
)

const tokenFile = "token.json"

// NewSheetsService creates a new sheets service with the given client secret
func NewSheetsService(secret []byte) *sheets.Service {
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
	tok, err := tokenFromEnvOrFile(os.Getenv("TOKEN"))
	if err != nil {
		tok, err = getTokenFromWeb(config)
		if err != nil {
			log.Printf("Unable to retrieve token from web: %+v\n", err)
			return nil
		}
		if err = saveToken(tok); err != nil {
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
func tokenFromEnvOrFile(sheetsAPIToken string) (*oauth2.Token, error) {
	var (
		tok = &oauth2.Token{}
		err error
	)
	// Try reading from the environment variable
	if sheetsAPIToken != "" {
		err = json.NewDecoder(bytes.NewReader([]byte(sheetsAPIToken))).Decode(tok)
	} else {
		f, err := ioutil.ReadFile(tokenFile)
		if err != nil {
			return nil, err
		}
		buf := bytes.NewReader(f)
		err = json.NewDecoder(buf).Decode(tok)
	}
	return tok, err
}

// Saves a token to token.json
func saveToken(token *oauth2.Token) error {
	f, err := os.Create(tokenFile)
	if err != nil {
		return errors.WithMessage(err, "Unable to create file "+tokenFile)
	}
	if err = json.NewEncoder(f).Encode(token); err != nil {
		return err
	}
	return nil
}
