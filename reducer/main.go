package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"

	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"golang.org/x/oauth2"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/zmb3/spotify"
)

type LambdaResponse struct {
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
)

var (
	ch    = make(chan *spotify.Client)
	auth  = spotify.NewAuthenticator(redirectURI, spotify.ScopePlaylistModifyPrivate)
	state = "abc123"
	sess  = session.New()
)

func main() {

	local := len(os.Args) > 1 && os.Args[1] == "true"

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

		// sess, err = session.NewSession(&aws.Config{
		// 	Region:      aws.String("us-east-1"),
		// 	Credentials: credentials.NewSharedCredentials("", "personal"),
		// })

		go retrieveToken()
		refreshReducer()
	}

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
	// use the token to get an authenticated client
	fmt.Println("tok", tok)
	fmt.Println("tok.AccessToken", tok.AccessToken)
	fmt.Println("tok.Expiry", tok.Expiry)
	fmt.Println("tok.RefreshToken", tok.RefreshToken)
	fmt.Println("tok.Type", tok.Type())
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

func retrieveToken() {
	println("retrieve token")
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

	fmt.Println("token ok", &token)

	client := auth.NewClient(token)
	ch <- &client
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

	fmt.Println(client)
	println("get profile")

	user, err := client.GetUsersPublicProfile(spotify.ID(userID))

	if err != nil {
		panic(err)
	}

	println(user.ID)
	println(user.DisplayName)

	reducerPlaylist, err := client.GetPlaylist(user.ID, reducerPlaylistID)

	if err != nil {
		panic(err)
	}

	if reducerPlaylist.Tracks.Total == 0 {
		return
	}

	currentTime := time.Now()
	newReducerName := fmt.Sprintf("Reducer %s", currentTime.Format("2006.01.02"))

	println(newReducerName)

	newReducerPlaylist, err := client.CreatePlaylistForUser(userID, newReducerName, false)

	if err != nil {
		panic(err)
	}

	println(newReducerPlaylist.Name)

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
