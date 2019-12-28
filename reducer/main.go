package main

import (
	"context"
	"fmt"
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
	snapshotPlaylistID    = "1JzrBnA8LeB99urw7q54ed"
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
	addToDynamo     = true
)

func parseArg(index int) bool {
	return len(os.Args) > index && os.Args[index] == "true"
}

func main() {
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
			fmt.Println(err)
			return lambdaResponse{
				Message: "fail",
			}, err
		}
	} else {

		go retrieveToken()
		err := refreshAllPlaylists()
		if err != nil {
			fmt.Println(err)
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
		song := songData{}
		err = dynamodbattribute.UnmarshalMap(dynamoSong, song)
		if err != nil {
			println(err)
		}
		allSongs = append(allSongs, song)
	}
	return allSongs
}

func refreshAllPlaylists() error {
	client := <-clientChannel
	err := refreshReducer(client)
	if err != nil {
		return err
	}
	allSongs := getAllStoredSongs()
	refreshSnapshot(allSongs, client, -6, snapshotPlaylistID)
	refreshSnapshot(allSongs, client, -12, snapshotPlaylistID)
	return nil
}

func refreshReducer(client *spotify.Client) error {

	dynamoSvc := dynamodb.New(sess)

	currentTime := time.Now()
	newReducerName := fmt.Sprintf("Reducer %s", currentTime.Format("2006.01.02"))

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
				fmt.Println(err)
				continue
			}
		}
	}

	recentlyPlayed, err := client.PlayerRecentlyPlayed()

	if err != nil {
		println(err)
	}

	for _, track := range recentlyPlayed {
		tracksToAdd = append(tracksToAdd, songListenedAt{track.Track, track.PlayedAt.Format(spotify.TimestampLayout)})
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
			Item:      item,
			TableName: aws.String("reducer-song-statistics"),
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

	client.AddTracksToPlaylist(reducerPastPlaylistID, tracksAddedIDs...)
	client.RemoveTracksFromPlaylist(bufferPlaylistID, tracksAddedIDs...)

	fmt.Printf("%d tracks added\n", tracksAddedCount)

	return nil

}

func refreshSnapshot(allStoredSongs []songData, client *spotify.Client, monthOffset int, playlistID spotify.ID) {

	existingSongs, _ := client.GetPlaylistTracks(playlistID)

	songIDsToRemove := []spotify.ID{}

	for _, song := range existingSongs.Tracks {
		songIDsToRemove = append(songIDsToRemove, song.Track.ID)
	}

	timeOffset := time.Now().AddDate(0, monthOffset, 0)

	songsToAdd := []spotify.ID{}

	for _, song := range allStoredSongs {
		if song.Date == timeOffset.Format("2006.01.02") {
			songsToAdd = append(songsToAdd, spotify.ID(song.ID))
		}
	}

	client.RemoveTracksFromPlaylist(snapshotPlaylistID, songIDsToRemove...)
	client.AddTracksToPlaylist(snapshotPlaylistID, songsToAdd...)

}
