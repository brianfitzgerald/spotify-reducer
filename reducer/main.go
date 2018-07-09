package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"

	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"golang.org/x/oauth2"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/zmb3/spotify"
)

type lambdaResponse struct {
	Message string `json:"message"`
}

type songData struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Date      string `json:"date"`
	Timestamp int    `json:"timestamp"`
	Reducer   string `json:"reducer"`
}

const (
	userID              = "fudgedoodle"
	reducerPlaylistID   = "7ICCpbSVM2lzKAiLjyKMrZ"
	redirectURI         = "http://localhost:8080/callback"
	clientID            = "145df35f322e4809b1ddb730f237e113"
	clientSecret        = "428bdf9b128044988c58ba00e7548b9b"
	songStatisticsTable = "reducer-song-statistics"
	keyDataTable        = "reducer-key-table"
	refreshURI          = "https://accounts.spotify.com/api/token"
)

var (
	ch      = make(chan *spotify.Client)
	auth    = spotify.NewAuthenticator(redirectURI, spotify.ScopePlaylistModifyPrivate)
	state   = "abc123"
	sess    = session.New()
	local   = len(os.Args) > 1 && os.Args[1] == "true"
	testing = len(os.Args) > 2 && os.Args[2] == "true"
)

func main() {
	if local || testing {
		handler(nil)
	} else {
		lambda.Start(handler)
	}
}

func handler(ctx context.Context) (lambdaResponse, error) {

	println(local)
	var err error

	if local {
		sess, err = session.NewSession(&aws.Config{
			Region:      aws.String("us-east-1"),
			Credentials: credentials.NewSharedCredentials("", "personal"),
		})
		if err != nil {
			panic(err)
		}
		userAuth()
		refreshReducer()
	} else {

		if testing {
			sess, err = session.NewSession(&aws.Config{
				Region:      aws.String("us-east-1"),
				Credentials: credentials.NewSharedCredentials("", "personal"),
			})

		}

		go retrieveToken()
		refreshReducer()
	}

	return lambdaResponse{
		Message: "success",
	}, nil

}

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
	ch <- &client
	saveToken(tok)
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
		Item: conv,
		ReturnConsumedCapacity: aws.String("TOTAL"),
		TableName:              aws.String(keyDataTable),
	}

	_, err = dynamoSvc.PutItem(input)

	if err != nil {
		panic(err)
	}

}

func refreshToken(refreshToken string) *oauth2.Token {
	println("token is obsolete, refreshing")
	println("refresh token ", refreshToken)
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {refreshToken},
		"redirect_uri": {redirectURI},
	}
	req, err := http.NewRequest("POST", refreshURI, strings.NewReader(form.Encode()))
	authHeader := fmt.Sprintf("Basic %s:%s", clientID, clientSecret)
	println(authHeader)
	req.Header.Add("Authorization", base64.StdEncoding.EncodeToString([]byte(authHeader)))
	if err != nil {
		panic(err)
	}
	client := http.Client{}
	res, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}
	println(res.StatusCode)
	fmt.Println("token refresh response ", res)
	token := &oauth2.Token{}
	json.Unmarshal(body, token)
	return token
}

func retrieveToken() {
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
		panic(err)
	}

	token := &oauth2.Token{}

	fmt.Println(token)

	err = dynamodbattribute.UnmarshalMap(tokenGetItem.Item, token)

	if err != nil {
		panic(err)
	}

	println("token.Expiry.Before ", token.Expiry.Before(time.Now()))

	if token.Expiry.Before(time.Now()) {
		token = refreshToken(token.RefreshToken)
		println("refreshed token:")
		fmt.Println(token)
		client := auth.NewClient(token)
		ch <- &client
	} else {

		fmt.Println("token ok", &token)

		println("token expires in ", token.Expiry.Format("Mon Jan 2 15:04:05 MST 2006"))

		client := auth.NewClient(token)
		ch <- &client

	}

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
	client := <-ch

	// use the client to make calls that require authorization
	user, err := client.CurrentUser()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("You are logged in as:", user.ID)
}

func refreshReducer() {

	println("refreshing")

	client := <-ch

	user, err := client.GetUsersPublicProfile(spotify.ID(userID))

	if err != nil {
		panic(err)
	}

	println("refreshing for ", user.ID)

	reducerPlaylist, err := client.GetPlaylist(user.ID, reducerPlaylistID)

	if err != nil {
		panic(err)
	}

	if reducerPlaylist.Tracks.Total == 0 {
		return
	}

	currentTime := time.Now()
	newReducerName := fmt.Sprintf("Reducer %s", currentTime.Format("2006.01.02"))

	newReducerPlaylist, err := client.CreatePlaylistForUser(userID, newReducerName, false)

	if err != nil {
		panic(err)
	}

	dynamoSvc := dynamodb.New(sess)

	for _, track := range reducerPlaylist.Tracks.Tracks {
		client.AddTracksToPlaylist(userID, newReducerPlaylist.ID, track.Track.ID)
		client.RemoveTracksFromPlaylist(userID, reducerPlaylist.ID, track.Track.ID)
		songDataToSave := &songData{
			ID:        track.Track.ID.String(),
			Title:     track.Track.Name,
			Artist:    track.Track.Artists[0].Name,
			Timestamp: int(currentTime.Unix()),
			Date:      currentTime.Format("2006.01.02"),
			Reducer:   newReducerName,
		}
		item, err := dynamodbattribute.MarshalMap(songDataToSave)

		if err != nil {
			panic(err)
		}

		input := &dynamodb.PutItemInput{
			Item: item,
			ReturnConsumedCapacity: aws.String("TOTAL"),
			TableName:              aws.String("reducer-song-statistics"),
		}

		_, err = dynamoSvc.PutItem(input)

		if err != nil {
			panic(err)
		}
	}

}
