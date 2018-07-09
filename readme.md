# Spotify Reducer

A system for tracking your music tastes over time.

Here's how it works:

* clone inside `GOPATH`
* `make` then `serverless deploy`
* Create a playlist and enter the playlist ID in the `reducerPlaylistID` value in `main`.
* Add songs to the playlist.
* Each day at midnight, the lambda will copy all the songs inside there to a Dynamo table and reset the playlist for the next day's music listening. It'll also create a new playlist with all the songs for that day, if you choose.