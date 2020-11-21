package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"

	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"

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

type songListenedAt struct {
	Track   spotify.SimpleTrack
	AddedAt string
}

const (
	userID                = "fudgedoodle"
	reducerPastPlaylistID = "7MHn8B6AcI0SK6qFfvcrHL"
	bufferPlaylistID      = "1rWKf36NvH4q6imCstXvy4"
	historyPlaylistID     = "3qRoWUcFzg4MPigu2jfesV"
	snapshotPlaylistID    = "1rWKf36NvH4q6imCstXvy4"
	redirectURI           = "http://localhost:8080/callback"
	clientID              = "145df35f322e4809b1ddb730f237e113"
	clientSecret          = "428bdf9b128044988c58ba00e7548b9b"
	songStatisticsTable   = "reducer-song-statistics"
	keyDataTable          = "reducer-key-table"
	refreshURI            = "https://accounts.spotify.com/api/token"
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
	importOnly      = parseArg(2)
	clientGetAmount = 5
	addToDynamo     = false
)

func parseArg(index int) bool {
	return len(os.Args) > index && os.Args[index] == "true"
}

func main() {
	println("lambda started")
	if local {
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
		err := refreshAllPlaylists()
		if err != nil {
			println(err)
			return lambdaResponse{
				Message: "fail",
			}, err
		}
	} else {

		go retrieveToken()
		err := refreshAllPlaylists()
		if err != nil {
			log.Fatal(err)
		}
	}

	return lambdaResponse{
		Message: "success",
	}, nil

}

func getAllStoredSongs() []songData {
	dynamoSvc := dynamodb.New(sess)
	songScan, err := dynamoSvc.Scan(&dynamodb.ScanInput{
		TableName: aws.String(songStatisticsTable),
	})
	allSongs := []songData{}
	for _, dynamoSong := range songScan.Items {
		song := &songData{}
		err = dynamodbattribute.UnmarshalMap(dynamoSong, song)
		if err != nil {
			log.Fatal(err)
		}
		allSongs = append(allSongs, *song)
	}
	return allSongs
}

func refreshAllPlaylists() error {
	client := <-clientChannel
	// take songs from buffer and put them in reducer
	err := refreshReducer(*client)
	if err != nil {
		return err
	}
	err = addRecentlyPlayed(client)
	if err != nil {
		return err
	}

	// refresh the snapshot playlist with both 6 and 12 months ago
	allSongs := getAllStoredSongs()
	removeAllFromPlaylist(allSongs, client, snapshotPlaylistID)
	refreshSnapshot(allSongs, client, -6, snapshotPlaylistID)
	refreshSnapshot(allSongs, client, -12, snapshotPlaylistID)
	return nil
}

func addRecentlyPlayed(client *spotify.Client) error {
	recentlyPlayed, err := client.PlayerRecentlyPlayed()

	// get tracks to add
	tracksToAdd := []songListenedAt{}
	dynamoSvc := dynamodb.New(sess)

	if err == nil {
		for _, track := range recentlyPlayed {
			tracksToAdd = append(tracksToAdd, songListenedAt{track.Track, track.PlayedAt.Format(spotify.TimestampLayout)})
		}
	}

	for _, track := range tracksToAdd {
		addTrackToDynamo(track, *client, dynamoSvc)
	}

	return nil
}

func addTrackToDynamo(track songListenedAt, client spotify.Client, dynamoSvc *dynamodb.DynamoDB) error {

	currentTime := time.Now()
	newReducerName := fmt.Sprintf("Reducer %s", currentTime.Format("2006.01.02"))

	addedDate, _ := time.Parse(spotify.TimestampLayout, track.AddedAt)
	startOfDay := time.Date(currentTime.Year(), currentTime.Month(), currentTime.Day(), 0, 0, 0, 0, currentTime.Location())
	if addedDate.Before(startOfDay) {
		return nil
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
		Item:      item,
		TableName: aws.String("reducer-song-statistics"),
	}

	if addToDynamo {
		fmt.Printf("Add song: %s", track.Track.Name)
		_, err = dynamoSvc.PutItem(input)
	}

	if err != nil {
		return err
	}

	return nil

}

func refreshReducer(client spotify.Client) error {

	// get tracks to add
	tracksToAdd := []songListenedAt{}

	for _, playlistID := range playlistsToMonitor {
		trackNum, err := client.GetPlaylist(spotify.ID(playlistID))
		if err != nil {
			println(err)
		}
		numTracks := trackNum.Tracks.Total
		for i := 0; i < numTracks; i += 100 {
			tracksPage, err := client.GetPlaylistTracksOpt(spotify.ID(playlistID), &spotify.Options{Offset: &i}, "")
			for _, track := range tracksPage.Tracks {
				tracksToAdd = append(tracksToAdd, songListenedAt{track.Track.SimpleTrack, track.AddedAt})
			}
			if err != nil {
				println(err)
				continue
			}
		}
	}

	tracksAddedCount := 0

	reducerPastTracks, err := client.GetPlaylistTracks(reducerPastPlaylistID)
	if err != nil {
		return err
	}

	dynamoSvc := dynamodb.New(sess)
	// add tracks to dynamo / spotify
	for _, track := range tracksToAdd {
		for _, pastTrack := range reducerPastTracks.Tracks {
			if pastTrack.Track.ID == track.Track.ID {
				fmt.Printf("detected duplicate: %s\n", track.Track.Name)
				continue
			}
		}
		tracksAddedCount++

		addTrackToDynamo(track, client, dynamoSvc)
	}

	var tracksAddedIDs []spotify.ID
	for _, track := range tracksToAdd {
		tracksAddedIDs = append(tracksAddedIDs, track.Track.ID)
	}

	for i := 0; i < len(tracksAddedIDs); i += 100 {
		trackEnd := i + 100
		if trackEnd > len(tracksAddedIDs) {
			trackEnd = len(tracksAddedIDs)
		}
		trackBatch := tracksAddedIDs[i:trackEnd]
		_, err = client.AddTracksToPlaylist(reducerPastPlaylistID, trackBatch...)
		_, err = client.RemoveTracksFromPlaylist(bufferPlaylistID, trackBatch...)
	}

	fmt.Printf("%d tracks added\n", tracksAddedCount)

	return nil

}
