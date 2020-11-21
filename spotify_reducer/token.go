package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"golang.org/x/oauth2"
)

func completeAuth(w http.ResponseWriter, r *http.Request) {
	tok, err := auth.Token(state, r)
	if err != nil {
		http.Error(w, "Couldn't get token", http.StatusForbidden)
		log.Fatal(err)
	}
	if st := r.FormValue("state"); st != state {
		http.NotFound(w, r)
		log.Fatalf("State mismatch: %s != %s\n", st, state)
	}
	client := auth.NewClient(tok)
	fmt.Fprintf(w, "Login Completed!")
	for i := 0; i < clientGetAmount; i++ {
		clientChannel <- &client
	}
	if !importOnly {
		saveToken(tok)
	}
}

func saveToken(token *oauth2.Token) {
	dynamoSvc := dynamodb.New(sess)
	conv, err := dynamodbattribute.MarshalMap(token)

	if err != nil {
		panic(err)
	}

	conv["id"] = &dynamodb.AttributeValue{
		S: aws.String(userID),
	}

	input := &dynamodb.PutItemInput{
		Item:      conv,
		TableName: aws.String(keyDataTable),
	}

	_, err = dynamoSvc.PutItem(input)

	if err != nil {
		panic(err)
	}

}

func refreshToken(refreshToken string) (*oauth2.Token, error) {
	params := url.Values{}
	params.Set("refresh_token", "AQDyKFYmr2giu3Wj6qzJKduGTsFjcd2yeBDUDilonWjiMbyP42Jeuqsf2jwrFkwiSwsaS5nJA6-5006Cbk0nTxXnckhfVNXc1yxecoml6VSxFIVKlUIWkx45NpB3NybMrek")
	params.Set("grant_type", "refresh_token")
	reqBody := bytes.NewBufferString(params.Encode())
	req, err := http.NewRequest("POST", refreshURI, reqBody)

	headerToEncode := fmt.Sprintf("%s:%s", clientID, clientSecret)
	encodedAuthHeader := base64.StdEncoding.EncodeToString([]byte(headerToEncode))

	authHeader := fmt.Sprintf("Basic %s", encodedAuthHeader)
	req.Header.Add("Authorization", authHeader)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")

	if err != nil {
		return nil, err
	}
	client := http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	fmt.Println("token refresh response ", res)
	token := &oauth2.Token{}
	json.Unmarshal(body, token)
	return token, nil
}

func retrieveToken() error {
	dynamoSvc := dynamodb.New(sess)
	tokenGetItem, err := dynamoSvc.GetItem(&dynamodb.GetItemInput{
		TableName: aws.String(keyDataTable),
		Key: map[string]*dynamodb.AttributeValue{
			"id": {
				S: aws.String(userID),
			},
		},
	})

	if err != nil {
		return err
	}

	token := &oauth2.Token{}

	err = dynamodbattribute.UnmarshalMap(tokenGetItem.Item, token)

	if err != nil {
		return err
	}

	if token.Expiry.Before(time.Now()) {
		token, err = refreshToken(token.RefreshToken)
		if err != nil {
			fmt.Println(err)
		}
		client := auth.NewClient(token)
		saveToken(token)
		clientChannel <- &client
	} else {

		client := auth.NewClient(token)
		clientChannel <- &client

	}

	return nil

}

func userAuth() {
	http.HandleFunc("/callback", completeAuth)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Got request for:", r.URL.String())
	})
	go http.ListenAndServe(":8080", nil)

	auth.SetAuthInfo(clientID, clientSecret)

	url := auth.AuthURL(state)
	fmt.Println("Please log in to Spotify by visiting the following page in your browser:", url)

	// wait for auth to complete
	client := <-clientChannel

	// use the client to make calls that require authorization
	user, err := client.CurrentUser()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("You are logged in as:", user.ID)
}
