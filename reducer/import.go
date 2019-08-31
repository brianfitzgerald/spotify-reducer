package main

import (
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/zmb3/spotify"
)

func importPlaylist(playlist *spotify.FullPlaylist) {
	currentTime := time.Now()

	dynamoSvc := dynamodb.New(sess)

	for _, track := range playlist.Tracks.Tracks {
		songDataToSave := &songData{
			ID:        track.Track.ID.String(),
			Title:     track.Track.Name,
			Artist:    track.Track.Artists[0].Name,
			Timestamp: int(currentTime.Unix()),
			Date:      track.AddedAt,
			Reducer:   playlist.Name,
		}
		item, err := dynamodbattribute.MarshalMap(songDataToSave)

		if err != nil {
			panic(err)
		}

		input := &dynamodb.PutItemInput{
			Item:                   item,
			ReturnConsumedCapacity: aws.String("TOTAL"),
			TableName:              aws.String("reducer-song-statistics"),
		}

		_, err = dynamoSvc.PutItem(input)

		if err != nil {
			panic(err)
		}

	}

}

func importAllPlaylists(playlistIDs []string) {
	client := <-ch
	for _, fullURL := range playlistIDs {
		url := strings.Split(fullURL, "/")[6]
		println(url)
		playlist, err := client.GetPlaylist(spotify.ID(url))
		if err != nil {
			panic(err)
		}
		importPlaylist(playlist)
	}
}

var playlists = []string{
	"https://open.spotify.com/user/fudgedoodle/playlist/4V7Dqq3iLEbJrMMcEyn8PM?si=R_JCFmVESi-2itdltxSXHA",
	"https://open.spotify.com/user/fudgedoodle/playlist/0Z1XjAMRgTfSa9RblebfzW?si=3vjYKvTURZSdPANs-VYM7A",
	"https://open.spotify.com/user/fudgedoodle/playlist/4OTaYb4IIOXqk4Di8F03fU?si=oPoKtzhgS3GWzuMc-y_m1A",
	"https://open.spotify.com/user/fudgedoodle/playlist/7D9FfqLabu7X18uHQCMSRW?si=QT0nFY4YRtebgRBFSrQA9A",
	"https://open.spotify.com/user/fudgedoodle/playlist/0eqYBaNmwcbnLHUYYlmXyC?si=sHEyfnXnTWS6ElGWQ4kS8A",
	"https://open.spotify.com/user/fudgedoodle/playlist/4Zy22OSubMcmcwne9Hm65O?si=rEFmge7AT4mqueVcSJJGcw",
	"https://open.spotify.com/user/fudgedoodle/playlist/1oPmfBq4txspxeEYy1xKdv?si=TEQ_qhiASRqEsZBpcilZnA",
	"https://open.spotify.com/user/fudgedoodle/playlist/3IIDlb8UiVKe5remnWX0T8?si=9zUj_v0wSHqG1zohzn1nQQ",
	"https://open.spotify.com/user/fudgedoodle/playlist/6RW8xCIXyKUL3WyB9Or9iN?si=eekDFdopSay5eYY0wWsSRw",
	"https://open.spotify.com/user/fudgedoodle/playlist/6DBdFfP6daViXg7kP8p9Y3?si=Xri6iFrZQv-O_A97128DsQ",
	"https://open.spotify.com/user/fudgedoodle/playlist/7pQlKvM6FIp0RIQH06xsmm?si=HAfLcidcSmu2qdJFdxAgCQ",
	"https://open.spotify.com/user/fudgedoodle/playlist/02ns6AxHG4kQ6xVtxokJPM?si=EacCpZFlTVK0m3pagp2nYA",
	"https://open.spotify.com/user/fudgedoodle/playlist/4OykpsOtHnRjTGxclgU5aE?si=aACPEX7wQbeqbTsBt1oCXA",
	"https://open.spotify.com/user/fudgedoodle/playlist/7knurM1ddAiyRoMLddZHHj?si=gIvmdDt9Q9G6ujGn8nCT3w",
	"https://open.spotify.com/user/fudgedoodle/playlist/6ASUH2DasoQyMkGZf2GO1h?si=Wr3EDArGQUGnK6Y5fFSz4g",
	"https://open.spotify.com/user/fudgedoodle/playlist/1PjHUdjwz47PXsvkAkK6nB?si=ITSZ5Ch0SCyjUE2f2tD72Q",
	"https://open.spotify.com/user/fudgedoodle/playlist/0mdjIzrBsNOEmYd8rbtGtM?si=_73R1F9zRG6Q_I-xTYjgvg",
	"https://open.spotify.com/user/fudgedoodle/playlist/3uN2PhtCsTTgDhXGzL9Mez?si=kHJG-g3gTOKZftMOgjrDBg",
	"https://open.spotify.com/user/fudgedoodle/playlist/2CenM7GMbe1FaYsm6CVryZ?si=vJYb1DYkTNy6aGBgWJ6Rcg",
	"https://open.spotify.com/user/fudgedoodle/playlist/5hOSuwUwIiPKtfV20LtbvE?si=h13EnKWaQsqoh4C8mj-Csw",
	"https://open.spotify.com/user/fudgedoodle/playlist/3NaV9BfLCciZ2RHvxqzh4h?si=Kt4EMbiJQ7GN_LDbRH3_2w",
	"https://open.spotify.com/user/fudgedoodle/playlist/4pvnbDsfu4wz0pXig4BofS?si=R9w3HNPjRqihDgMxwJ-bsQ",
	"https://open.spotify.com/user/fudgedoodle/playlist/3EduuQarOmS0eH70KckpOp?si=askZKcGJRFCOqJJ69eoftA",
	"https://open.spotify.com/user/fudgedoodle/playlist/7xEl0gVHL70Vt6dmOXF8mq?si=yKKQdgYsRk2B8ViE1Qf0bQ",
}
