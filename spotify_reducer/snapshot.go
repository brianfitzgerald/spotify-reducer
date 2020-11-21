package main

import (
	"time"

	"github.com/zmb3/spotify"
)

func removeAllFromPlaylist(allStoredSongs []songData, client *spotify.Client, playlistID spotify.ID) {
	// Remove existing songs
	existingSongs, _ := client.GetPlaylistTracks(playlistID)

	songIDsToRemove := []spotify.ID{}

	for _, song := range existingSongs.Tracks {
		songIDsToRemove = append(songIDsToRemove, song.Track.ID)
	}

	client.RemoveTracksFromPlaylist(snapshotPlaylistID, songIDsToRemove...)
}

func refreshSnapshot(allStoredSongs []songData, client *spotify.Client, monthOffset int, playlistID spotify.ID) {

	timeOffset := time.Now().AddDate(0, monthOffset, 0)

	songsToAdd := []spotify.ID{}

	// Add new songs for date
	for _, song := range allStoredSongs {
		if song.Date == timeOffset.Format("2006.01.02") {
			songsToAdd = append(songsToAdd, spotify.ID(song.ID))
		}
	}

	client.AddTracksToPlaylist(snapshotPlaylistID, songsToAdd...)

}
