package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
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
	userID                 = "fudgedoodle"
	reducerPastPlaylistID  = "7MHn8B6AcI0SK6qFfvcrHL"
	bufferPlaylistID       = "1rWKf36NvH4q6imCstXvy4"
	sixMonthsAgoPlaylistID = "1JzrBnA8LeB99urw7q54ed"
	redirectURI            = "http://localhost:8080/callback"
	clientID               = "145df35f322e4809b1ddb730f237e113"
	clientSecret           = "428bdf9b128044988c58ba00e7548b9b"
	songStatisticsTable    = "reducer-song-statistics"
	keyDataTable           = "reducer-key-table"
	refreshURI             = "https://accounts.spotify.com/api/token"
)

var (
	playlistsToMonitor = []string{
		bufferPlaylistID,
	}
)

var (
	clientChannel   = make(chan *spotify.Client)
	auth            = spotify.NewAuthenticator(redirectURI, spotify.ScopePlaylistModifyPrivate)
	state           = "abc123"
	sess            = session.New()
	local           = parseArg(1)
	testing         = parseArg(2)
	importOnly      = parseArg(3)
	clientGetAmount = 5
	addToDynamo     = false
)

func parseArg(index int) bool {
	return len(os.Args) > index && os.Args[index] == "true"
}

func main() {
	if local || testing {
		handler(nil)
	} else {
		lambda.Start(handler)
	}
}

func handler(ctx context.Context) (lambdaResponse, error) {

	var err error

	if importOnly {
		sess, err = session.NewSession(&aws.Config{
			Region:      aws.String("us-east-1"),
			Credentials: credentials.NewSharedCredentials("", "personal"),
		})
		userAuth()
		importAllPlaylists(playlists)
		return lambdaResponse{
			Message: "success",
		}, nil
	}

	if local {
		sess, err = session.NewSession(&aws.Config{
			Region:      aws.String("us-east-1"),
			Credentials: credentials.NewSharedCredentials("", "personal"),
		})
		if err != nil {
			return lambdaResponse{
				Message: "fail",
			}, err
		}
		userAuth()
		err := refreshReducer()
		if err != nil {
			fmt.Println(err)
			return lambdaResponse{
				Message: "fail",
			}, err
		}
	} else {

		if testing {
			sess, err = session.NewSession(&aws.Config{
				Region:      aws.String("us-east-1"),
				Credentials: credentials.NewSharedCredentials("", "personal"),
			})

		}

		go retrieveToken()
		err := refreshReducer()
		if err != nil {
			fmt.Println(err)
		}
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
		Item:                   conv,
		ReturnConsumedCapacity: aws.String("TOTAL"),
		TableName:              aws.String(keyDataTable),
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

func refreshReducer() error {

	client := <-clientChannel

	dynamoSvc := dynamodb.New(sess)

	currentTime := time.Now()
	newReducerName := fmt.Sprintf("Reducer %s", currentTime.Format("2006.01.02"))

	// get tracks to add
	tracksToAdd := []spotify.PlaylistTrack{}
	for _, playlistID := range playlistsToMonitor {
		trackNum, err := client.GetPlaylist(spotify.ID(playlistID))
		if err != nil {
			println(err)
		}
		numTracks := trackNum.Tracks.Total
		for i := 0; i < numTracks; i += 100 {
			tracksPage, err := client.GetPlaylistTracksOpt(spotify.ID(playlistID), &spotify.Options{Offset: &i}, "")
			for _, track := range tracksPage.Tracks {
				tracksToAdd = append(tracksToAdd, track)
			}
			if err != nil {
				fmt.Println(err)
				continue
			}
		}
	}

	tracksAddedCount := 0

	// add tracks to dynamo / spotify
	for _, track := range tracksToAdd {
		addedDate, _ := time.Parse(spotify.TimestampLayout, track.AddedAt)
		startOfDay := time.Date(currentTime.Year(), currentTime.Month(), currentTime.Day(), 0, 0, 0, 0, currentTime.Location())
		if addedDate.Before(startOfDay) {
			continue
		}

		reducerPastTracks, err := client.GetPlaylistTracks(reducerPastPlaylistID)
		if err != nil {
			return err
		}

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
			return err
		}

		input := &dynamodb.PutItemInput{
			Item:                   item,
			ReturnConsumedCapacity: aws.String("TOTAL"),
			TableName:              aws.String("reducer-song-statistics"),
		}

		if addToDynamo {
			_, err = dynamoSvc.PutItem(input)
		}
		fmt.Printf("%v", input)

		for _, pt := range reducerPastTracks.Tracks {
			if pt.Track.ID == track.Track.ID {
				continue
			}
		}

		tracksAddedCount++

		if err != nil {
			return err
		}

	}

	var tracksAddedIDs []spotify.ID
	for _, track := range tracksToAdd {
		tracksAddedIDs = append(tracksAddedIDs, track.Track.ID)
	}

	client.RemoveTracksFromPlaylist(bufferPlaylistID, tracksAddedIDs...)
	client.AddTracksToPlaylist(reducerPastPlaylistID, tracksAddedIDs...)

	fmt.Printf("%d tracks added\n", tracksAddedCount)

	return nil

}

func refreshSixMonthsAgo() {
	client := <-clientChannel
	dynamoSvc := dynamodb.New(sess)
	allSongs, err := dynamoSvc.Scan(&dynamodb.ScanInput{
		TableName: aws.String(songStatisticsTable),
	})
	sixMonthsAgo := time.Now().AddDate(0, -6, 0)
	songsToAdd := []spotify.ID{}
	for _, dynamoSong := range allSongs.Items {
		song := songData{}
		err = dynamodbattribute.UnmarshalMap(dynamoSong, song)
		if err != nil {
			println(err)
		}
		if song.Date == sixMonthsAgo.Format("2006.01.02") {
			songsToAdd = append(songsToAdd, spotify.ID(song.ID))
		}
	}
	existingSongs, _ := client.GetPlaylistTracks(sixMonthsAgoPlaylistID)
	songIDsToRemove := []spotify.ID{}
	for _, song := range existingSongs.Tracks {
		songIDsToRemove = append(songIDsToRemove, song.Track.ID)
	}
	client.RemoveTracksFromPlaylist(sixMonthsAgoPlaylistID, songIDsToRemove...)
	fmt.Printf("songs from 6 months ago: %+v", songsToAdd)
	client.AddTracksToPlaylist(sixMonthsAgoPlaylistID, songsToAdd...)
	if err != nil {
		return
	}
}
