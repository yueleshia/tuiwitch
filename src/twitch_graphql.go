package src

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// See the following:
// https://github.com/streamlink/streamlink/blob/master/src/streamlink/plugins/twitch.py
// https://github.com/futo-org/grayjay-plugin-twitch/blob/master/TwitchScript.js
// https://github.com/SuperSonicHub1/twitch-graphql-api

// in grayjay, see getChannelPager
// I've added game Moments
var VODS_GRAPHQL_QUERY = strings.ReplaceAll(`query FilterableVideoTower_Videos($channelOwnerLogin: String!, $limit: Int, $cursor: Cursor, $broadcastType: BroadcastType, $videoSort: VideoSort, $options: VideoConnectionOptionsInput) {
    user(login: $channelOwnerLogin) {
        id
        videos(first: $limit, after: $cursor, type: $broadcastType, sort: $videoSort, options: $options) {
            edges {
                cursor
                node {
                    __typename
                    id
                    title
                    previewThumbnailURL(width: 320, height: 180)
                    publishedAt
                    lengthSeconds
                    game {
                        name
                    }
                    owner {
                        id
                        displayName
                        login
                        profileImageURL(width: 50)
                    }
                    moments(first: 0, after: null, sort: ASC, types: GAME_CHANGE, momentRequestType: VIDEO_CHAPTER_MARKERS) {
                        edges {
                            node {
                                description
                                positionMilliseconds
                            }
                        }
                        pageInfo {
                            hasNextPage
                        }
                    }
                }
            }
            pageInfo {
                hasNextPage
            }
        }
    }
}`, "\n", "")
func Graph_vods(channel string) Result[[]Video] {
	// url format https://www.twitch.tv/qtcinderella/videos?filter=all&sort=time (query params may or may not be there)
	variables := strings.Join([]string{
		`{`,
		`"broadcastType":null,`,
		`"channelOwnerLogin":"` + channel  + `",`,
		`"cursor":null,`,
		`"limit":` + fmt.Sprintf("%d", PAGE_SIZE) + `,`,
		`"videoSort":"TIME"`,
		`}`,
	}, "")
	query := strings.Join([]string{
		"[{",
		`"operationName": "FilterableVideoTower_Videos",`,
		`"variables":` + variables + `,`,
		`"query":"` + VODS_GRAPHQL_QUERY + `"`,
		"}]",
	}, "")
	Assert(json.Valid([]byte(query)))


	videos := [PAGE_SIZE]Video{}
	var request io.ReadCloser
	{
		x, err := Request(context.TODO(), "POST", map[string]string{
			//"Authorization": void 0,
			"Accept": "*/*",
			"Accept-Language": "en-US",
			"Content-Type": "text/plain; charset=UTF-8",
			"Client-Id": CLIENT_ID,
			//"Device-ID": void 0,
		}, strings.NewReader(query), "https://gql.twitch.tv/gql#origin=twilight", fmt.Sprintf("graph-%s-videos", channel))
		if err != nil {
			return Result[[]Video]{videos[:0], err}
		}
		request = x
	}

	ret, err := func() ([]Video, error) {
		type VideoNode struct {
			Typename       string `json:"__typename"`
			Id             string `json:"id"`
			Title          string `json:"title"`
			Thumbnail_URL  string `json:"previewThumbnailURL"`
			Published_at   string `json:"publishedAt"`
			Length_seconds int    `json:"lengthSeconds"`
			Game struct {
				Name string `json:"name"`
			} `json:"game"`
			Owner struct {
				Id            string `json:"id"`
				Display_name  string `json:"displayName"`
				Login         string `json:"login"`
				Profile_URL   string `json:"profileImageURL"`
			} `json:"owner"`
			Moments struct {
				Edges []struct {
					Node struct {
						Description           string        `json:"description"`
						Position_milliseconds time.Duration `json:"positionMilliseconds"`
					} `json:"node"`
				} `json:"edges"`
				Page_info struct {
					Has_next_page bool `json:"hasNextPage"`
				} `json:"pageInfo"`
			} `json:"moments"`
		}
		type VideoEdge struct {
			Cursor string    `json:"cursor"`
			Node   VideoNode `json:"node"`
		}
		type FilterableVideoTower_Videos struct {
			Data struct {
				User struct {
					Id string `json:"id"`
					Videos struct {
						Edges []VideoEdge `json:"edges"`
						Page_info struct {
							Has_next_page bool `json:"hasNextPage"`
						} `json:"pageInfo"`
					} `json:"videos"`
				} `json:"user"`
			} `json:"data"`
			Extensions struct {
				Duration_milliseconds int    `json:"durationMilliseconds"`
				Operation_name        string `json:"operationName"`
				Request_id            string `json:"requestID"`

			} `json:"extensions"`
		}

		var unmarshalled []FilterableVideoTower_Videos
		dec := json.NewDecoder(request)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&unmarshalled); err != nil {
			return videos[:0], err
		}

		video_edges := unmarshalled[0].Data.User.Videos.Edges
		min_length := PAGE_SIZE
		if len(video_edges) < min_length {
			min_length = len(video_edges)
		}
		idx := 0
		for i := min_length - 1; i >= 0; i -= 1 {
			x := video_edges[i].Node

			var start time.Time
			if x, err := time.Parse(time.RFC3339, x.Published_at); err != nil {
				return videos[:0], err
			} else {
				start = x
			}

			var chapters []Chapter
			if len(x.Moments.Edges) == 0 {
				chapters = []Chapter{Chapter{x.Game.Name, 0}}
			} else {
				chapters = make([]Chapter, len(x.Moments.Edges))
				for j, y := range x.Moments.Edges {
					chapters[j] = Chapter {
						Name:     y.Node.Description,
						Position: y.Node.Position_milliseconds * time.Millisecond,
					}
				}
			}

			videos[idx] = Video {
				Title: x.Title,
				Channel: channel,
				Description: "",
				Thumbnail_URL: []string{x.Thumbnail_URL},
				Start_time: start,
				Duration: time.Duration(x.Length_seconds) * time.Second,
				Is_live: false,
				Url: "https://www.twitch.tv/videos/" + x.Id,
				Chapters: chapters,
			}
			idx += 1
		}

		return videos[:idx], nil
	}()
	if err != nil {
		return Result[[]Video]{videos[:0], err}
	}
	return Result[[]Video]{ret, request.Close()}
}
